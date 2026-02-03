package main

import (
	"crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/willunylabs/wand/router"
)

func main() {
	duration := flag.Duration("duration", 1*time.Minute, "total test duration")
	concurrency := flag.Int("concurrency", 64, "number of worker goroutines")
	rps := flag.Int("rps", 1000, "approximate total requests per second (0 for unlimited)")
	flag.Parse()

	r := router.NewRouter()
	_ = r.GET("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	_ = r.GET("/users/:id", func(w http.ResponseWriter, req *http.Request) {
		id, _ := router.Param(w, "id")
		_, _ = w.Write([]byte(id))
	})
	_ = r.GET("/static/*filepath", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	transport := &http.Transport{
		MaxIdleConns:        1024,
		MaxIdleConnsPerHost: 1024,
		MaxConnsPerHost:     0,
	}
	client := &http.Client{Transport: transport}

	paths := []string{
		"/health",
		"/users/123",
		"/static/css/app.css",
	}

	end := time.Now().Add(*duration)
	var okCount uint64
	var errCount uint64

	var wg sync.WaitGroup
	workers := *concurrency
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	var rateCh <-chan struct{}
	if *rps > 0 {
		interval := time.Second / time.Duration(*rps)
		if interval <= 0 {
			interval = time.Nanosecond
		}
		tokens := make(chan struct{}, workers)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		go func() {
			for range ticker.C {
				select {
				case tokens <- struct{}{}:
				default:
				}
			}
		}()
		rateCh = tokens
	}

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(seed int64) {
			defer wg.Done()
			rnd := newFastRand(seed)
			for time.Now().Before(end) {
				if rateCh != nil {
					<-rateCh
				}
				path := paths[rnd.Intn(len(paths))]
				resp, err := client.Get(srv.URL + path)
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
		}(int64(randSeed()) + int64(i))
	}

	wg.Wait()

	total := atomic.LoadUint64(&okCount) + atomic.LoadUint64(&errCount)
	elapsed := duration.Seconds()
	qps := float64(total) / elapsed
	fmt.Printf("duration=%s concurrency=%d rps_target=%d total=%d ok=%d err=%d qps=%.1f\n",
		duration.String(), workers, *rps, total, okCount, errCount, qps)
}

type fastRand struct {
	state uint64
}

func newFastRand(seed int64) *fastRand {
	s := uint64(seed)
	if s == 0 {
		s = randSeed()
	}
	return &fastRand{state: s}
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
