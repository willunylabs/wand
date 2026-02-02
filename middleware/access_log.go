package middleware

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/WillunyLabs-LLC/wand/logger"
)

// AccessLog writes structured access events into the ring buffer.
func AccessLog(rb *logger.RingBuffer, next http.Handler) http.Handler {
	if rb == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := statusWriterPool.Get().(*statusWriter)
		sw.ResponseWriter = w
		sw.status = 0
		sw.bytes = 0
		next.ServeHTTP(sw, r)

		status := sw.status
		bytes := sw.bytes
		statusWriterPool.Put(sw)
		if status == 0 {
			status = http.StatusOK
		}

		remote := r.RemoteAddr
		if host, _, err := net.SplitHostPort(remote); err == nil {
			remote = host
		}

		end := time.Now()
		event := logger.LogEvent{
			Timestamp:     end.UnixNano(),
			Method:        r.Method,
			Path:          r.URL.Path,
			Status:        uint16(status),
			Bytes:         bytes,
			DurationNanos: end.Sub(start).Nanoseconds(),
			RemoteAddr:    remote,
		}
		_ = rb.TryWrite(event)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

var statusWriterPool = sync.Pool{
	New: func() interface{} {
		return &statusWriter{}
	},
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (w *statusWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (w *statusWriter) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		if w.status == 0 {
			w.status = http.StatusOK
		}
		n, err := rf.ReadFrom(r)
		w.bytes += n
		return n, err
	}
	return io.Copy(w.ResponseWriter, r)
}
