package main

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/willunylabs/wand/router"
)

type soakConfig struct {
	duration    time.Duration
	concurrency int
	rps         int
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseSoakConfig(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "soak failed: %v\n", err)
		return 2
	}
	if err := runSoak(cfg, stdout); err != nil {
		_, _ = fmt.Fprintf(stderr, "soak failed: %v\n", err)
		return 1
	}
	return 0
}

func parseSoakConfig(args []string) (soakConfig, error) {
	cfg := soakConfig{
		duration:    1 * time.Minute,
		concurrency: 64,
		rps:         1000,
	}
	fs := flag.NewFlagSet("soak", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.DurationVar(&cfg.duration, "duration", cfg.duration, "total test duration")
	fs.IntVar(&cfg.concurrency, "concurrency", cfg.concurrency, "number of worker goroutines")
	fs.IntVar(&cfg.rps, "rps", cfg.rps, "approximate total requests per second (0 for unlimited)")
	if err := fs.Parse(args); err != nil {
		return soakConfig{}, err
	}
	return cfg, nil
}

func runSoak(cfg soakConfig, out io.Writer) error {
	return runSoakWithClient(cfg, out, nil, "")
}

func runSoakWithClient(cfg soakConfig, out io.Writer, client *http.Client, baseURL string) error {
	if out == nil {
		return errors.New("nil output writer")
	}
	if cfg.duration <= 0 {
		return errors.New("duration must be > 0")
	}

	r := router.NewRouter()
	if err := r.GET("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}); err != nil {
		return err
	}
	if err := r.GET("/users/:id", func(w http.ResponseWriter, req *http.Request) {
		id, _ := router.Param(w, "id")
		_, _ = w.Write([]byte(id))
	}); err != nil {
		return err
	}
	if err := r.GET("/static/*filepath", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}); err != nil {
		return err
	}

	if client == nil {
		var cleanup func()
		var err error
		client, baseURL, cleanup, err = newNetworkSoakClient(r)
		if err != nil {
			client, baseURL, cleanup = newInMemorySoakClient(r)
		}
		defer cleanup()
	} else if baseURL == "" {
		return errors.New("baseURL must be set when custom client is provided")
	}

	paths := []string{"/health", "/users/123", "/static/css/app.css"}

	end := time.Now().Add(cfg.duration)
	var okCount uint64
	var errCount uint64

	var wg sync.WaitGroup
	workers := cfg.concurrency
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	var rateCh <-chan struct{}
	if cfg.rps > 0 {
		interval := time.Second / time.Duration(cfg.rps)
		if interval <= 0 {
			interval = time.Nanosecond
		}
		tokens := make(chan struct{}, workers)
		ticker := time.NewTicker(interval)
		done := make(chan struct{})
		defer ticker.Stop()
		defer close(done)
		go func() {
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					select {
					case tokens <- struct{}{}:
					default:
					}
				}
			}
		}()
		rateCh = tokens
	}

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(seed uint64) {
			defer wg.Done()
			rnd := newFastRand(seed)
			for time.Now().Before(end) {
				if rateCh != nil {
					<-rateCh
				}
				path := paths[rnd.Intn(len(paths))]
				resp, err := client.Get(baseURL + path)
				if err != nil {
					atomic.AddUint64(&errCount, 1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 500 {
					atomic.AddUint64(&okCount, 1)
				} else {
					atomic.AddUint64(&errCount, 1)
				}
			}
		}(randSeed() ^ (uint64(i) + 1)) // #nosec G115
	}

	wg.Wait()

	total := atomic.LoadUint64(&okCount) + atomic.LoadUint64(&errCount)
	elapsed := cfg.duration.Seconds()
	qps := float64(total) / elapsed
	_, err := fmt.Fprintf(out, "duration=%s concurrency=%d rps_target=%d total=%d ok=%d err=%d qps=%.1f\n",
		cfg.duration.String(), workers, cfg.rps, total, okCount, errCount, qps)
	return err
}

func newNetworkSoakClient(handler http.Handler) (*http.Client, string, func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", nil, err
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = ln
	srv.Start()

	transport := &http.Transport{
		MaxIdleConns:        1024,
		MaxIdleConnsPerHost: 1024,
		MaxConnsPerHost:     0,
	}
	client := &http.Client{Transport: transport}
	cleanup := func() {
		client.CloseIdleConnections()
		srv.Close()
	}
	return client, srv.URL, cleanup, nil
}

func newInMemorySoakClient(handler http.Handler) (*http.Client, string, func()) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			return rec.Result(), nil
		}),
	}
	cleanup := func() {
		client.CloseIdleConnections()
	}
	return client, "http://in-memory.local", cleanup
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type fastRand struct {
	state uint64
}

func newFastRand(seed uint64) *fastRand {
	if seed == 0 {
		seed = randSeed()
	}
	return &fastRand{state: seed}
}

func (r *fastRand) next() uint64 {
	// xorshift64
	x := r.state
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	r.state = x
	return x
}

func (r *fastRand) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.next() % uint64(n)) // #nosec G115 -- bounds checked above
}

func randSeed() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return binary.LittleEndian.Uint64(b[:])
	}
	return uint64(time.Now().UnixNano()) // #nosec G115 -- fallback, non-security usage
}
