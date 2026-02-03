package middleware

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/willunylabs/wand/logger"
)

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	prev := RequestIDGenerator
	RequestIDGenerator = func() string { return "abc123" }
	defer func() { RequestIDGenerator = prev }()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	var gotFromHandler string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFromHandler = r.Header.Get(HeaderRequestID)
		w.WriteHeader(http.StatusOK)
	}))

	h.ServeHTTP(rec, req)

	if gotFromHandler != "abc123" {
		t.Fatalf("expected handler to see generated id, got %q", gotFromHandler)
	}
	if got := rec.Header().Get(HeaderRequestID); got != "abc123" {
		t.Fatalf("expected response header to be set, got %q", got)
	}
}

func TestRequestID_PreservesExisting(t *testing.T) {
	prev := RequestIDGenerator
	RequestIDGenerator = func() string { return "new-id" }
	defer func() { RequestIDGenerator = prev }()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequestID, "existing")
	rec := httptest.NewRecorder()
	var gotFromHandler string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFromHandler = r.Header.Get(HeaderRequestID)
		w.WriteHeader(http.StatusOK)
	}))

	h.ServeHTTP(rec, req)

	if gotFromHandler != "existing" {
		t.Fatalf("expected handler to see existing id, got %q", gotFromHandler)
	}
	if got := rec.Header().Get(HeaderRequestID); got != "existing" {
		t.Fatalf("expected response header to preserve existing id, got %q", got)
	}
}

func TestRequestID_NoGeneratorNoHeader(t *testing.T) {
	prev := RequestIDGenerator
	RequestIDGenerator = nil
	defer func() { RequestIDGenerator = prev }()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	h.ServeHTTP(rec, req)

	if got := rec.Header().Get(HeaderRequestID); got != "" {
		t.Fatalf("expected no request id header, got %q", got)
	}
}

func TestRecovery_Recovers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h := Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

type markerHandler struct {
	called bool
}

func (m *markerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.called = true
}

func TestTimeout_ReturnsNextWhenDisabled(t *testing.T) {
	next := &markerHandler{}
	if got := Timeout(0, next); got != next {
		t.Fatalf("expected same handler when disabled")
	}
}

func TestTimeout_Triggers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h := Timeout(10*time.Millisecond, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestBodySizeLimit_Enforced(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("1234567890"))
	rec := httptest.NewRecorder()
	var readErr error
	h := BodySizeLimit(5, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))

	h.ServeHTTP(rec, req)

	var mbe *http.MaxBytesError
	if !errors.As(readErr, &mbe) {
		t.Fatalf("expected MaxBytesError, got %v", readErr)
	}
	if mbe.Limit != 5 {
		t.Fatalf("expected limit 5, got %d", mbe.Limit)
	}
}

func TestAccessLog_WritesEvent(t *testing.T) {
	rb, err := logger.NewRingBuffer(8)
	if err != nil {
		t.Fatalf("ring buffer: %v", err)
	}

	events := make(chan logger.LogEvent, 1)
	done := make(chan struct{})
	go func() {
		rb.Consume(func(batch []logger.LogEvent) {
			for _, e := range batch {
				select {
				case events <- e:
				default:
				}
			}
		})
		close(done)
	}()

	h := AccessLog(rb, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	rb.Close()

	select {
	case e := <-events:
		if e.Method != http.MethodGet {
			t.Fatalf("expected method GET, got %q", e.Method)
		}
		if e.Path != "/health" {
			t.Fatalf("expected path /health, got %q", e.Path)
		}
		if e.Status != http.StatusNoContent {
			t.Fatalf("expected status 204, got %d", e.Status)
		}
		if e.Bytes != 0 {
			t.Fatalf("expected 0 bytes, got %d", e.Bytes)
		}
		if e.RemoteAddr != "127.0.0.1" {
			t.Fatalf("expected remote addr 127.0.0.1, got %q", e.RemoteAddr)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for log event")
	}

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for consumer to finish")
	}
}

func TestAccessLog_PanicStillLogs(t *testing.T) {
	rb, err := logger.NewRingBuffer(8)
	if err != nil {
		t.Fatalf("ring buffer: %v", err)
	}

	events := make(chan logger.LogEvent, 1)
	done := make(chan struct{})
	go func() {
		rb.Consume(func(batch []logger.LogEvent) {
			for _, e := range batch {
				select {
				case events <- e:
				default:
				}
			}
		})
		close(done)
	}()

	h := AccessLog(rb, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	func() {
		defer func() {
			_ = recover()
		}()
		h.ServeHTTP(rec, req)
	}()
	rb.Close()

	select {
	case e := <-events:
		if e.Path != "/panic" {
			t.Fatalf("expected path /panic, got %q", e.Path)
		}
		if e.Status != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d", e.Status)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for log event")
	}

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for consumer to finish")
	}
}

func TestCORS_AllowsOrigin(t *testing.T) {
	opts := DefaultCORSOptions()
	opts.AllowedOrigins = []string{"https://example.com"}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cors", nil)
	req.Header.Set("Origin", "https://example.com")

	h := CORS(opts, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("expected allow origin, got %q", got)
	}
	if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Origin") {
		t.Fatalf("expected Vary to include Origin, got %q", vary)
	}
}

func TestCORS_Preflight(t *testing.T) {
	opts := DefaultCORSOptions()
	opts.AllowedOrigins = []string{"https://example.com"}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/cors", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "X-Token, X-Request-ID")

	h := CORS(opts, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPost) {
		t.Fatalf("expected allow methods to include POST, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "X-Token, X-Request-ID" {
		t.Fatalf("expected allow headers to echo request, got %q", got)
	}
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	opts := DefaultCORSOptions()
	opts.AllowedOrigins = []string{"https://example.com"}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/cors", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)

	h := CORS(opts, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no allow origin header, got %q", got)
	}
}

func TestCORS_WildcardWithCredentialsDenied(t *testing.T) {
	opts := DefaultCORSOptions()
	opts.AllowedOrigins = []string{"*"}
	opts.AllowCredentials = true

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/cors", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)

	h := CORS(opts, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no allow origin header, got %q", got)
	}
}

func TestRecovery_WithOptions(t *testing.T) {
	calledLogger := false
	calledHandler := false
	logStack := true
	h := RecoveryWith(RecoveryOptions{
		Logger: func(_ *http.Request, _ any, stack []byte) {
			calledLogger = true
			if len(stack) == 0 {
				t.Fatal("expected stack trace")
			}
		},
		Handler: func(w http.ResponseWriter, _ *http.Request, _ any) {
			calledHandler = true
			w.WriteHeader(http.StatusTeapot)
		},
		LogStack: &logStack,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected 418, got %d", rec.Code)
	}
	if !calledLogger || !calledHandler {
		t.Fatalf("expected logger and handler to be called")
	}
}

func TestRecovery_NoStack(t *testing.T) {
	logStack := false
	calledLogger := false
	h := RecoveryWith(RecoveryOptions{
		LogStack: &logStack,
		Logger: func(_ *http.Request, _ any, stack []byte) {
			calledLogger = true
			if len(stack) != 0 {
				t.Fatal("expected no stack trace")
			}
		},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	h.ServeHTTP(rec, req)
	if !calledLogger {
		t.Fatalf("expected logger to be called")
	}
}

func TestStatic_ServesFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	h := Static("/static", root)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "ok" {
		t.Fatalf("expected body ok, got %q", body)
	}
}

func TestStatic_PrefixBoundary(t *testing.T) {
	root := t.TempDir()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/staticx/app.js", nil)
	h := Static("/static", root)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected next handler, got %d", rec.Code)
	}
}

func TestStatic_NoDirectoryListing(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static/dir/", nil)
	h := Static("/static", root)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestLogger_Default(t *testing.T) {
	var buf strings.Builder
	h := LoggerWith(LoggerOptions{
		Writer: &buf,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/log", nil)
	h.ServeHTTP(rec, req)

	out := buf.String()
	if !strings.Contains(out, "GET /log") {
		t.Fatalf("expected method/path in log, got %q", out)
	}
	if !strings.Contains(out, " 201 ") {
		t.Fatalf("expected status in log, got %q", out)
	}
}

func TestLogger_Panic(t *testing.T) {
	var buf strings.Builder
	h := LoggerWith(LoggerOptions{
		Writer: &buf,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	func() {
		defer func() {
			_ = recover()
		}()
		h.ServeHTTP(rec, req)
	}()

	out := buf.String()
	if !strings.Contains(out, " 500 ") {
		t.Fatalf("expected 500 in log, got %q", out)
	}
}

func TestLogger_JSON(t *testing.T) {
	var buf strings.Builder
	h := LoggerWith(LoggerOptions{
		Writer: &buf,
		JSON:   true,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	h.ServeHTTP(rec, req)

	line := strings.TrimSpace(buf.String())
	if line == "" || !strings.HasPrefix(line, "{") {
		t.Fatalf("expected json log line, got %q", line)
	}
	if !strings.Contains(line, "\"status\":201") {
		t.Fatalf("expected status in json log, got %q", line)
	}
}

func TestLogger_SanitizesControlChars(t *testing.T) {
	var buf strings.Builder
	h := LoggerWith(LoggerOptions{
		Writer: &buf,
		Formatter: func(entry LogEntry) string {
			return entry.Path + " " + entry.RequestID
		},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.URL.Path = "/line1\nline2\rline3"
	req.Header.Set(HeaderRequestID, "rid-1\nrid-2")
	h.ServeHTTP(rec, req)

	out := buf.String()
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("expected single log line, got %q", out)
	}
	if strings.Contains(out, "\r") {
		t.Fatalf("expected CR to be sanitized, got %q", out)
	}
}

func TestTrustedProxyHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2, 3.3.3.3")
	req.Header.Set("X-Forwarded-Proto", "https, http")
	req.Header.Set("X-Forwarded-Host", "example.com, proxy")

	ips := XForwardedFor(req)
	if len(ips) != 3 || ips[0] != "1.1.1.1" || ips[2] != "3.3.3.3" {
		t.Fatalf("unexpected X-Forwarded-For: %v", ips)
	}
	if got := XForwardedProto(req); got != "https" {
		t.Fatalf("expected proto https, got %q", got)
	}
	if got := XForwardedHost(req); got != "example.com" {
		t.Fatalf("expected host example.com, got %q", got)
	}
}

func TestClientIP_TrustChain(t *testing.T) {
	trust, err := NewCIDRTrustFunc([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("trust func: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	req.Header.Set("X-Forwarded-For", "1.1.1.1, 10.0.0.1")

	if got := ClientIP(req, trust); got != "1.1.1.1" {
		t.Fatalf("expected client 1.1.1.1, got %q", got)
	}
}

func TestClientIP_UntrustedPeer(t *testing.T) {
	trust, err := NewCIDRTrustFunc([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("trust func: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.Header.Set("X-Forwarded-For", "1.1.1.1")

	if got := ClientIP(req, trust); got != "8.8.8.8" {
		t.Fatalf("expected remote 8.8.8.8, got %q", got)
	}
}

func TestClientIP_AllTrusted(t *testing.T) {
	trust, err := NewCIDRTrustFunc([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("trust func: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:1234"
	req.Header.Set("X-Forwarded-For", "10.0.0.9, 10.0.0.8")

	if got := ClientIP(req, trust); got != "10.0.0.9" {
		t.Fatalf("expected leftmost XFF 10.0.0.9, got %q", got)
	}
}
