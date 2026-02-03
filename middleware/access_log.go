package middleware

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/willunylabs/wand/logger"
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
		var recovered any
		defer func() {
			if rec := recover(); rec != nil {
				recovered = rec
			}

			status := sw.status
			bytes := sw.bytes
			sw.ResponseWriter = nil
			sw.status = 0
			sw.bytes = 0
			statusWriterPool.Put(sw)
			if status == 0 {
				if recovered != nil {
					status = http.StatusInternalServerError
				} else {
					status = http.StatusOK
				}
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
					Status:        statusToUint16(status),
					Bytes:         bytes,
					DurationNanos: end.Sub(start).Nanoseconds(),
					RemoteAddr:    remote,
				}
			_ = rb.TryWrite(event)

			if recovered != nil {
				panic(recovered)
			}
		}()

		next.ServeHTTP(sw, r)
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
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := io.Copy(w.ResponseWriter, r)
	w.bytes += n
	return n, err
}

func statusToUint16(status int) uint16 {
	if status <= 0 {
		return 0
	}
	if status > 0xffff {
		return 0xffff
	}
	return uint16(status) // #nosec G115 -- bounds checked above
}
