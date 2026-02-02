package middleware

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WillunyLabs-LLC/wand/logger"
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
