package main

import (
	"flag"
	"fmt"
	"io"
	"math/rand"
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

	perWorker := 0
	if *rps > 0 {
		perWorker = *rps / workers
		if perWorker <= 0 {
			perWorker = 1
		}
	}

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(seed int64) {
			defer wg.Done()
			rnd := rand.New(rand.NewSource(seed))
			var ticker *time.Ticker
			if perWorker > 0 {
				interval := time.Second / time.Duration(perWorker)
				if interval <= 0 {
					interval = time.Nanosecond
				}
				ticker = time.NewTicker(interval)
				defer ticker.Stop()
			}
			for time.Now().Before(end) {
				if ticker != nil {
					<-ticker.C
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
		}(time.Now().UnixNano() + int64(i))
	}

	wg.Wait()

	total := atomic.LoadUint64(&okCount) + atomic.LoadUint64(&errCount)
	elapsed := duration.Seconds()
	qps := float64(total) / elapsed
	fmt.Printf("duration=%s concurrency=%d rps_target=%d total=%d ok=%d err=%d qps=%.1f\n",
		duration.String(), workers, *rps, total, okCount, errCount, qps)
}
