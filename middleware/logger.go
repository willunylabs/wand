package middleware

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// LogEntry represents a single access log entry.
type LogEntry struct {
	Time       time.Time
	Method     string
	Path       string
	Proto      string
	Status     int
	Bytes      int64
	Duration   time.Duration
	RemoteAddr string
	RequestID  string
}

// JSONLogEntry is the wire format for JSON logger.
type JSONLogEntry struct {
	Time       string `json:"time"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Proto      string `json:"proto"`
	Status     int    `json:"status"`
	Bytes      int64  `json:"bytes"`
	DurationMS int64  `json:"duration_ms"`
	RemoteAddr string `json:"remote_addr,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
}

// LoggerOptions configures Logger behavior.
type LoggerOptions struct {
	// Writer is where log lines are written. Defaults to os.Stdout.
	Writer io.Writer
	// Formatter builds a log line from a LogEntry. Defaults to DefaultLogFormatter.
	// Use JSONFormatter for JSON lines (JSON ignores TimeFormat; JSON=true respects it).
	Formatter func(LogEntry) string
	// JSON forces JSON output and ignores Formatter.
	JSON bool
	// TimeFormat is used by the default formatter. Defaults to time.RFC3339Nano.
	TimeFormat string
}

// Logger writes a single line per request using the default formatter.
func Logger(next http.Handler) http.Handler {
	return LoggerWith(LoggerOptions{})(next)
}

// LoggerWith returns a middleware with custom formatting and output.
func LoggerWith(opts LoggerOptions) func(http.Handler) http.Handler {
	writer := opts.Writer
	if writer == nil {
		writer = os.Stdout
	}
	timeFormat := opts.TimeFormat
	if timeFormat == "" {
		timeFormat = time.RFC3339Nano
	}
	formatter := opts.Formatter
	if formatter == nil {
		formatter = func(entry LogEntry) string {
			return DefaultLogFormatter(entry, timeFormat)
		}
	}
	useJSON := opts.JSON

	var mu sync.Mutex

	return func(next http.Handler) http.Handler {
		if next == nil {
			return nil
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
				entry := LogEntry{
					Time:       end,
					Method:     r.Method,
					Path:       r.URL.Path,
					Proto:      r.Proto,
					Status:     status,
					Bytes:      bytes,
					Duration:   end.Sub(start),
					RemoteAddr: remote,
					RequestID:  r.Header.Get(HeaderRequestID),
				}

				if useJSON {
					mu.Lock()
					_ = writeJSONLine(writer, entry, timeFormat)
					mu.Unlock()
				} else {
					safeEntry := sanitizeLogEntry(entry)
					line := formatter(safeEntry)
					if !strings.HasSuffix(line, "\n") {
						line += "\n"
					}
					mu.Lock()
					_, _ = io.WriteString(writer, line)
					mu.Unlock()
				}

				if recovered != nil {
					panic(recovered)
				}
			}()

			next.ServeHTTP(sw, r)
		})
	}
}

// DefaultLogFormatter renders a minimal, stable access log line.
func DefaultLogFormatter(e LogEntry, timeFormat string) string {
	ts := e.Time.Format(timeFormat)
	remote := sanitizeLogField(e.RemoteAddr)
	if remote == "" {
		remote = "-"
	}
	method := sanitizeLogField(e.Method)
	path := sanitizeLogField(e.Path)
	proto := sanitizeLogField(e.Proto)
	requestID := sanitizeLogField(e.RequestID)

	builder := strings.Builder{}
	builder.Grow(64)
	builder.WriteString(ts)
	builder.WriteString(" ")
	builder.WriteString(remote)
	builder.WriteString(" \"")
	builder.WriteString(method)
	builder.WriteString(" ")
	builder.WriteString(path)
	builder.WriteString(" ")
	builder.WriteString(proto)
	builder.WriteString("\" ")
	builder.WriteString(intToString(e.Status))
	builder.WriteString(" ")
	builder.WriteString(int64ToString(e.Bytes))
	builder.WriteString(" ")
	builder.WriteString(e.Duration.String())
	if requestID != "" {
		builder.WriteString(" rid=")
		builder.WriteString(requestID)
	}
	return builder.String()
}

// JSONFormatter renders a JSON log line using encoding/json.
// It uses RFC3339Nano for time formatting.
func JSONFormatter(e LogEntry) string {
	entry := JSONLogEntry{
		Time:       e.Time.Format(time.RFC3339Nano),
		Method:     e.Method,
		Path:       e.Path,
		Proto:      e.Proto,
		Status:     e.Status,
		Bytes:      e.Bytes,
		DurationMS: e.Duration.Milliseconds(),
		RemoteAddr: e.RemoteAddr,
		RequestID:  e.RequestID,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return ""
	}
	return string(b)
}

func writeJSONLine(w io.Writer, e LogEntry, timeFormat string) error {
	entry := JSONLogEntry{
		Time:       e.Time.Format(timeFormat),
		Method:     e.Method,
		Path:       e.Path,
		Proto:      e.Proto,
		Status:     e.Status,
		Bytes:      e.Bytes,
		DurationMS: e.Duration.Milliseconds(),
		RemoteAddr: e.RemoteAddr,
		RequestID:  e.RequestID,
	}
	enc := json.NewEncoder(w)
	return enc.Encode(entry)
}

func intToString(v int) string {
	return int64ToString(int64(v))
}

func int64ToString(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func sanitizeLogField(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\r' || s[i] == '\n' {
			buf := make([]byte, 0, len(s))
			for j := 0; j < len(s); j++ {
				c := s[j]
				if c == '\r' || c == '\n' {
					buf = append(buf, ' ')
					continue
				}
				buf = append(buf, c)
			}
			return string(buf)
		}
	}
	return s
}

func sanitizeLogEntry(e LogEntry) LogEntry {
	e.Method = sanitizeLogField(e.Method)
	e.Path = sanitizeLogField(e.Path)
	e.Proto = sanitizeLogField(e.Proto)
	e.RemoteAddr = sanitizeLogField(e.RemoteAddr)
	e.RequestID = sanitizeLogField(e.RequestID)
	return e
}
