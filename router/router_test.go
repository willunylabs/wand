package router

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/willunylabs/wand/logger"
	"github.com/willunylabs/wand/middleware"
)

func mustGET(tb testing.TB, r *Router, pattern string, handler HandleFunc) {
	tb.Helper()
	if err := r.GET(pattern, handler); err != nil {
		tb.Fatalf("register %s failed: %v", pattern, err)
	}
}

func mustFreeze(tb testing.TB, r *Router) *FrozenRouter {
	tb.Helper()
	fr, err := r.Freeze()
	if err != nil {
		tb.Fatalf("freeze failed: %v", err)
	}
	return fr
}

// verify Param extraction
func TestRouter_Param(t *testing.T) {
	r := NewRouter()

	// Register route with params
	mustGET(t, r, "/user/:id/files/*filepath", func(w http.ResponseWriter, req *http.Request) {
		// Assert 0-Alloc Params
		prw, ok := w.(interface{ Param(string) (string, bool) })
		if !ok {
			t.Fatal("Response Writer is not wrapped with Params")
		}

		id, ok := prw.Param("id")
		if !ok || id != "42" {
			t.Errorf("Expected id=42, got %s", id)
		}

		fp, ok := prw.Param("filepath")
		if !ok || fp != "photo.jpg" {
			t.Errorf("Expected filepath=photo.jpg, got %s", fp)
		}

		fmt.Fprintf(w, "user %s file %s", id, fp)
	})

	// Create Request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/user/42/files/photo.jpg", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}

func TestRouter_Wildcard_Capture(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/static/*filepath", func(w http.ResponseWriter, req *http.Request) {
		prw := w.(interface{ Param(string) (string, bool) })
		fp, _ := prw.Param("filepath")
		if fp != "js/app.js" {
			t.Errorf("Capture failed. Expected 'js/app.js', got '%s'", fp)
		}
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/static/js/app.js", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Route not matched")
	}
}

func TestRouter_Priority(t *testing.T) {
	// Case 1: Static > Param
	r1 := NewRouter()
	mustGET(t, r1, "/files/new", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("STATIC"))
	})
	mustGET(t, r1, "/files/:filename", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("PARAM"))
	})

	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/files/new", nil)
	r1.ServeHTTP(w1, req1)
	if w1.Body.String() != "STATIC" {
		t.Errorf("Static > Param priority failed. Expected STATIC, got %s", w1.Body.String())
	}

	// Case 2: Static > Wildcard
	r2 := NewRouter()
	mustGET(t, r2, "/static/config", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("STATIC"))
	})
	mustGET(t, r2, "/static/*filepath", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("WILD"))
	})

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/static/config", nil)
	r2.ServeHTTP(w2, req2)
	if w2.Body.String() != "STATIC" {
		t.Errorf("Static > Wildcard priority failed. Expected STATIC, got %s", w2.Body.String())
	}
}

func TestRouter_StaticPriority(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/users/me", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("STATIC"))
	})
	mustGET(t, r, "/users/:id", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("PARAM"))
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/users/me", nil)
	r.ServeHTTP(w, req)

	if w.Body.String() != "STATIC" {
		t.Errorf("Static Priority failed. Expected STATIC, got %s", w.Body.String())
	}
}

func TestRouter_Head_Fallback(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/head", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-From", "get")
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodHead, "/head", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	if got := w.Header().Get("X-From"); got != "get" {
		t.Fatalf("expected X-From=get got %q", got)
	}
}

func TestRouter_Head_Explicit(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/head", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-From", "get")
	})
	if err := r.HEAD("/head", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-From", "head")
	}); err != nil {
		t.Fatalf("register head failed: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodHead, "/head", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	if got := w.Header().Get("X-From"); got != "head" {
		t.Fatalf("expected X-From=head got %q", got)
	}
}

func TestRouter_MethodNotAllowed(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/onlyget", func(w http.ResponseWriter, req *http.Request) {})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/onlyget", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 got %d", w.Code)
	}
	if allow := w.Header().Get("Allow"); allow != "GET, HEAD, OPTIONS" {
		t.Fatalf("unexpected Allow header: %q", allow)
	}
}

func TestRouter_Options(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/options", func(w http.ResponseWriter, req *http.Request) {})
	if err := r.POST("/options", func(w http.ResponseWriter, req *http.Request) {}); err != nil {
		t.Fatalf("register post failed: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodOptions, "/options", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	if allow := w.Header().Get("Allow"); allow != "GET, HEAD, POST, OPTIONS" {
		t.Fatalf("unexpected Allow header: %q", allow)
	}
}

func TestRouter_MethodHelpers(t *testing.T) {
	r := NewRouter()
	if err := r.PUT("/put", func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusNoContent) }); err != nil {
		t.Fatalf("register put failed: %v", err)
	}
	if err := r.PATCH("/patch", func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusNoContent) }); err != nil {
		t.Fatalf("register patch failed: %v", err)
	}
	if err := r.DELETE("/delete", func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusNoContent) }); err != nil {
		t.Fatalf("register delete failed: %v", err)
	}
	if err := r.OPTIONS("/opts", func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusNoContent) }); err != nil {
		t.Fatalf("register options failed: %v", err)
	}

	cases := []struct {
		method string
		path   string
	}{
		{method: http.MethodPut, path: "/put"},
		{method: http.MethodPatch, path: "/patch"},
		{method: http.MethodDelete, path: "/delete"},
		{method: http.MethodOptions, path: "/opts"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s %s expected 204 got %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestGroup_MethodHelpers(t *testing.T) {
	r := NewRouter()
	api := r.Group("/api")
	api.Use(func(next http.Handler) http.Handler { return next })

	if err := api.HEAD("/head", func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusNoContent) }); err != nil {
		t.Fatalf("register group head failed: %v", err)
	}
	if err := api.PUT("/put", func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusNoContent) }); err != nil {
		t.Fatalf("register group put failed: %v", err)
	}
	if err := api.PATCH("/patch", func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusNoContent) }); err != nil {
		t.Fatalf("register group patch failed: %v", err)
	}
	if err := api.DELETE("/delete", func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusNoContent) }); err != nil {
		t.Fatalf("register group delete failed: %v", err)
	}
	if err := api.OPTIONS("/opts", func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusNoContent) }); err != nil {
		t.Fatalf("register group options failed: %v", err)
	}

	cases := []struct {
		method string
		path   string
	}{
		{method: http.MethodHead, path: "/api/head"},
		{method: http.MethodPut, path: "/api/put"},
		{method: http.MethodPatch, path: "/api/patch"},
		{method: http.MethodDelete, path: "/api/delete"},
		{method: http.MethodOptions, path: "/api/opts"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s %s expected 204 got %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestRedirectToRawPath(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/static", nil)

	redirectToRawPath(rec, req, "/assets%2Fapp.js")

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301 got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/assets%2Fapp.js" {
		t.Fatalf("expected encoded redirect path, got %q", got)
	}
}

type flusherRW struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (w *flusherRW) Flush() {
	w.flushed = true
}

func TestRouter_Wrapper_PreservesFlusher(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/users/:id", func(w http.ResponseWriter, req *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("expected http.Flusher on ResponseWriter")
		}
		f.Flush()
	})

	w := &flusherRW{ResponseRecorder: httptest.NewRecorder()}
	req, _ := http.NewRequest("GET", "/users/1", nil)
	r.ServeHTTP(w, req)

	if !w.flushed {
		t.Fatalf("expected Flush to be forwarded to underlying ResponseWriter")
	}
}

type passthroughRW struct {
	header     http.Header
	hijacked   bool
	pushedPath string
	readBytes  int64
}

func (w *passthroughRW) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *passthroughRW) Write(b []byte) (int, error) { return len(b), nil }
func (w *passthroughRW) WriteHeader(int)             {}

func (w *passthroughRW) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacked = true
	return nil, nil, nil
}

func (w *passthroughRW) Push(target string, _ *http.PushOptions) error {
	w.pushedPath = target
	return nil
}

func (w *passthroughRW) ReadFrom(r io.Reader) (int64, error) {
	n, err := io.Copy(io.Discard, r)
	w.readBytes += n
	return n, err
}

func TestParamRW_PreservesInterfaces(t *testing.T) {
	base := &passthroughRW{}
	w := paramRW{ResponseWriter: base}

	if got := w.Unwrap(); got != base {
		t.Fatalf("expected unwrap to return original writer")
	}
	if err := w.Push("/assets/app.js", nil); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	if base.pushedPath != "/assets/app.js" {
		t.Fatalf("expected pushed path to be forwarded, got %q", base.pushedPath)
	}
	if _, _, err := w.Hijack(); err != nil {
		t.Fatalf("hijack failed: %v", err)
	}
	if !base.hijacked {
		t.Fatalf("expected hijack to be forwarded")
	}
	n, err := w.ReadFrom(strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("readfrom failed: %v", err)
	}
	if n != 5 || base.readBytes != 5 {
		t.Fatalf("expected 5 bytes to be forwarded, got n=%d readBytes=%d", n, base.readBytes)
	}
}

func TestRouter_Conflict(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/users/:id", func(w http.ResponseWriter, req *http.Request) {})

	// Case 1: Param name conflict
	if err := r.GET("/users/:name", func(w http.ResponseWriter, req *http.Request) {}); err == nil {
		t.Errorf("Expected error on param name conflict, but got none")
	}

	// Case 2: Param vs Wildcard conflict
	if err := r.GET("/users/*any", func(w http.ResponseWriter, req *http.Request) {}); err == nil {
		t.Errorf("Expected error on param vs wildcard conflict, but got none")
	}
}

func TestRouter_StaticNotWrapped(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/static/:id", func(w http.ResponseWriter, req *http.Request) {})
	mustGET(t, r, "/static/path", func(w http.ResponseWriter, req *http.Request) {
		if _, ok := w.(interface{ Param(string) (string, bool) }); ok {
			t.Fatalf("static route should not be wrapped with params")
		}
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/static/path", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

func TestRouter_Static_TrailingSlashEquivalent(t *testing.T) {
	cases := []struct {
		pattern string
		request string
	}{
		{"/static/path", "/static/path/"},
		{"/static/path/", "/static/path"},
	}

	for _, tc := range cases {
		r := NewRouter()
		r.StrictSlash = false
		mustGET(t, r, tc.pattern, func(w http.ResponseWriter, req *http.Request) {
			w.Write([]byte("ok"))
		})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.request, nil)
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("pattern %q request %q: expected 200 got %d", tc.pattern, tc.request, rec.Code)
		}
		if rec.Body.String() != "ok" {
			t.Fatalf("pattern %q request %q: expected body ok got %q", tc.pattern, tc.request, rec.Body.String())
		}

		fr := mustFreeze(t, r)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, tc.request, nil)
		fr.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("frozen pattern %q request %q: expected 200 got %d", tc.pattern, tc.request, rec.Code)
		}
		if rec.Body.String() != "ok" {
			t.Fatalf("frozen pattern %q request %q: expected body ok got %q", tc.pattern, tc.request, rec.Body.String())
		}
	}
}

func TestRouter_StrictSlash_Redirect(t *testing.T) {
	r := NewRouter()
	r.StrictSlash = true
	mustGET(t, r, "/a/", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("ok"))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/a", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/a/" {
		t.Fatalf("expected Location /a/, got %q", loc)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/a", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("expected 308, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/a/" {
		t.Fatalf("expected Location /a/, got %q", loc)
	}

	fr := mustFreeze(t, r)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/a", nil)
	fr.ServeHTTP(rec, req)
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("frozen expected 301, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/a/" {
		t.Fatalf("frozen expected Location /a/, got %q", loc)
	}
}

func TestRouter_DuplicateParamName(t *testing.T) {
	r := NewRouter()
	if err := r.GET("/users/:id/orders/:id", func(w http.ResponseWriter, req *http.Request) {}); err == nil {
		t.Fatalf("Expected error on duplicate param name, but got none")
	}
}

func TestRouter_InvalidMethod(t *testing.T) {
	r := NewRouter()
	if err := r.Handle("BAD METHOD", "/foo", func(w http.ResponseWriter, req *http.Request) {}); err == nil {
		t.Fatalf("expected error for invalid method")
	}
}

func TestRouter_DuplicateRoute(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/dup", func(w http.ResponseWriter, req *http.Request) {})
	if err := r.GET("/dup", func(w http.ResponseWriter, req *http.Request) {}); err == nil {
		t.Fatalf("Expected error on duplicate route, but got none")
	}
}

func TestRouter_MaxDepth_Register(t *testing.T) {
	r := NewRouter()
	okParts := make([]string, MaxDepth)
	for i := range okParts {
		okParts[i] = "a"
	}
	okPattern := "/" + strings.Join(okParts, "/")
	if err := r.GET(okPattern, func(w http.ResponseWriter, req *http.Request) {}); err != nil {
		t.Fatalf("expected max depth route to register, got %v", err)
	}

	tooDeepParts := append(okParts, "b")
	tooDeep := "/" + strings.Join(tooDeepParts, "/")
	if err := r.GET(tooDeep, func(w http.ResponseWriter, req *http.Request) {}); err == nil {
		t.Fatalf("expected error for too-deep route, got none")
	}
}

func TestRouter_NonCanonicalPattern(t *testing.T) {
	r := NewRouter()
	tests := []string{
		"users",
		"/a//b",
		"/a/./b",
		"/a/../b",
	}
	for _, pattern := range tests {
		if err := r.GET(pattern, func(w http.ResponseWriter, req *http.Request) {}); err == nil {
			t.Fatalf("expected error for non-canonical pattern %q, got none", pattern)
		}
	}
}

func TestRouter_NilHandler(t *testing.T) {
	r := NewRouter()
	if err := r.GET("/nil", nil); err == nil {
		t.Fatalf("expected error for nil handler, got none")
	}
}

func TestRouter_PathClean_Redirect(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/a/b", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/a//b", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301 redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/a/b" {
		t.Fatalf("expected Location /a/b, got %q", loc)
	}
}

func TestRouter_PathClean_Redirect_Post(t *testing.T) {
	r := NewRouter()
	if err := r.POST("/a/b", func(w http.ResponseWriter, req *http.Request) {}); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/a//b", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusPermanentRedirect {
		t.Fatalf("expected 308 redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/a/b" {
		t.Fatalf("expected Location /a/b, got %q", loc)
	}
}

func TestFrozenRouter_Basic(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/a/b", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("ab"))
	})
	mustGET(t, r, "/a/b/c", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("abc"))
	})
	mustGET(t, r, "/users/:id", func(w http.ResponseWriter, req *http.Request) {
		id, _ := Param(w, "id")
		w.Write([]byte(id))
	})
	mustGET(t, r, "/static/*filepath", func(w http.ResponseWriter, req *http.Request) {
		fp, _ := Param(w, "filepath")
		w.Write([]byte(fp))
	})
	fr := mustFreeze(t, r)

	tests := []struct {
		path     string
		code     int
		expected string
	}{
		{"/a/b", 200, "ab"},
		{"/a/b/c", 200, "abc"},
		{"/users/42", 200, "42"},
		{"/static/js/app.js", 200, "js/app.js"},
		{"/notfound", 404, ""},
	}

	for _, tc := range tests {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", tc.path, nil)
		fr.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("path %s: expected status %d got %d", tc.path, tc.code, w.Code)
		}
		if tc.code == 200 && w.Body.String() != tc.expected {
			t.Fatalf("path %s: expected body %q got %q", tc.path, tc.expected, w.Body.String())
		}
	}
}

func TestFrozenRouter_PathClean_Redirect(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/a/b", func(w http.ResponseWriter, req *http.Request) {})
	fr := mustFreeze(t, r)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/a//b", nil)
	fr.ServeHTTP(w, req)
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301 redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/a/b" {
		t.Fatalf("expected Location /a/b, got %q", loc)
	}
}

func TestFrozenRouter_MethodNotAllowed(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/onlyget", func(w http.ResponseWriter, req *http.Request) {})
	fr := mustFreeze(t, r)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/onlyget", nil)
	fr.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 got %d", w.Code)
	}
	if allow := w.Header().Get("Allow"); allow != "GET, HEAD, OPTIONS" {
		t.Fatalf("unexpected Allow header: %q", allow)
	}
}

func TestRouter_InvalidPathChars(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/ok", func(w http.ResponseWriter, req *http.Request) {})

	tests := []string{
		"/\x00",
		"/\r",
		"/\n",
	}
	for _, path := range tests {
		w := httptest.NewRecorder()
		req := &http.Request{Method: "GET", URL: &url.URL{Path: path}}
		r.ServeHTTP(w, req)
		if w.Code != 404 {
			t.Fatalf("expected 404 for %q, got %d", path, w.Code)
		}
	}
}

func TestRouter_PathTooLong(t *testing.T) {
	r := NewRouter()
	longPath := "/" + strings.Repeat("a", MaxPathLength)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", longPath, nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusRequestURITooLong {
		t.Fatalf("expected 414 got %d", w.Code)
	}
}

func TestRouter_ConcurrentServeHTTP(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/users/:id", func(w http.ResponseWriter, req *http.Request) {
		id, _ := Param(w, "id")
		w.Write([]byte(id))
	})
	mustGET(t, r, "/static/*filepath", func(w http.ResponseWriter, req *http.Request) {
		fp, _ := Param(w, "filepath")
		w.Write([]byte(fp))
	})
	mustGET(t, r, "/files/new", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("new"))
	})

	expected := map[string]struct {
		status int
		body   string
	}{
		"/users/1":            {status: 200, body: "1"},
		"/static/css/app.css": {status: 200, body: "css/app.css"},
		"/files/new":          {status: 200, body: "new"},
		"/notfound":           {status: 404, body: ""},
	}

	errCh := make(chan error, 128)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		for path, exp := range expected {
			wg.Add(1)
			go func(p string, e struct {
				status int
				body   string
			}) {
				defer wg.Done()
				w := httptest.NewRecorder()
				req, _ := http.NewRequest("GET", p, nil)
				r.ServeHTTP(w, req)
				if w.Code != e.status {
					errCh <- fmt.Errorf("path %s: expected status %d got %d", p, e.status, w.Code)
					return
				}
				if e.status == 200 && w.Body.String() != e.body {
					errCh <- fmt.Errorf("path %s: expected body %q got %q", p, e.body, w.Body.String())
					return
				}
			}(path, exp)
		}
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}
}

func TestRouter_Wildcard_EdgeCases(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/static/*filepath", func(w http.ResponseWriter, req *http.Request) {
		fp, _ := Param(w, "filepath")
		w.Write([]byte(fp))
	})

	tests := []struct {
		path     string
		expected string
	}{
		{"/static/css/style.css", "css/style.css"},
		{"/static/img/logo.png", "img/logo.png"},
	}

	for _, tc := range tests {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", tc.path, nil)
		r.ServeHTTP(w, req)

		if w.Body.String() != tc.expected {
			t.Errorf("Path %s: expected '%s', got '%s'", tc.path, tc.expected, w.Body.String())
		}
	}

	// Test Empty Match (Should match empty string now)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/static/", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("Expected 200 for empty wildcard match, got %d", w.Code)
	}
	if w.Body.String() != "" {
		t.Errorf("Expected empty body for empty wildcard match, got '%s'", w.Body.String())
	}

	// Test Empty Match (no trailing slash, should also match empty string)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/static", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("Expected 200 for empty wildcard match (no trailing slash), got %d", w.Code)
	}
	if w.Body.String() != "" {
		t.Errorf("Expected empty body for empty wildcard match, got '%s'", w.Body.String())
	}
}

func TestRouter_Wildcard_NoLeadingSlash(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/static/*filepath", func(w http.ResponseWriter, req *http.Request) {
		fp, _ := Param(w, "filepath")
		if strings.HasPrefix(fp, "/") {
			t.Fatalf("filepath should not start with '/', got %q", fp)
		}
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/static/js/app.js", nil)
	r.ServeHTTP(w, req)
}

func TestRouter_DoubleSlash_Clean(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/static/*filepath", func(w http.ResponseWriter, req *http.Request) {
		fp, _ := Param(w, "filepath")
		if fp != "js/app.js" {
			t.Fatalf("expected %q got %q", "js/app.js", fp)
		}
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/static//js/app.js", nil) // Explicit double slash
	r.ServeHTTP(w, req)
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301 got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/static/js/app.js" {
		t.Fatalf("expected Location /static/js/app.js got %q", loc)
	}
}

func TestRouter_Group_NestedMiddlewares(t *testing.T) {
	r := NewRouter()
	var calls []string
	mw := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls = append(calls, name+"-in")
				next.ServeHTTP(w, req)
				calls = append(calls, name+"-out")
			})
		}
	}

	if err := r.Use(mw("root")); err != nil {
		t.Fatalf("use failed: %v", err)
	}
	api := r.Group("/api", mw("api"))
	v1 := api.Group("/v1", mw("v1"))

	if err := v1.GET("/users", func(w http.ResponseWriter, req *http.Request) {
		calls = append(calls, "handler")
	}); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	r.ServeHTTP(rec, req)

	want := []string{"root-in", "api-in", "v1-in", "handler", "v1-out", "api-out", "root-out"}
	if len(calls) != len(want) {
		t.Fatalf("unexpected call order: %v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("unexpected call order: %v", calls)
		}
	}
}

func TestRouter_Middleware_Precomposed(t *testing.T) {
	r := NewRouter()
	wraps := 0
	if err := r.Use(func(next http.Handler) http.Handler {
		wraps++
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req)
		})
	}); err != nil {
		t.Fatalf("use failed: %v", err)
	}

	mustGET(t, r, "/ping", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(rec, req)
	r.ServeHTTP(rec, req)
	r.ServeHTTP(rec, req)

	if wraps != 1 {
		t.Fatalf("expected middleware to wrap once, got %d", wraps)
	}
}

func TestRouter_Use_AfterRegister(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/ping", func(w http.ResponseWriter, req *http.Request) {})
	if err := r.Use(func(next http.Handler) http.Handler { return next }); err == nil {
		t.Fatal("expected error when adding middleware after routes")
	}
}

func TestRouter_WrapHandle(t *testing.T) {
	r := NewRouter()
	var called []string
	if err := r.Use(WrapHandle(func(next HandleFunc) HandleFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			called = append(called, "mw")
			next(w, req)
		}
	})); err != nil {
		t.Fatalf("use failed: %v", err)
	}
	mustGET(t, r, "/ping", func(w http.ResponseWriter, req *http.Request) {
		called = append(called, "handler")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(rec, req)

	if len(called) != 2 || called[0] != "mw" || called[1] != "handler" {
		t.Fatalf("unexpected call order: %v", called)
	}
}

func TestRouter_CustomNotFound(t *testing.T) {
	r := NewRouter()
	r.NotFound = func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected 418, got %d", rec.Code)
	}
}

func TestRouter_CustomMethodNotAllowed(t *testing.T) {
	r := NewRouter()
	mustPOST := func(tb testing.TB, r *Router, pattern string, handler HandleFunc) {
		tb.Helper()
		if err := r.POST(pattern, handler); err != nil {
			tb.Fatalf("register %s failed: %v", pattern, err)
		}
	}
	mustPOST(t, r, "/login", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	called := 0
	r.MethodNotAllowed = func(w http.ResponseWriter, req *http.Request) {
		called++
		w.WriteHeader(http.StatusTeapot)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected 418, got %d", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "POST, OPTIONS" {
		t.Fatalf("unexpected Allow header: %q", allow)
	}
	if called != 1 {
		t.Fatalf("expected MethodNotAllowed to be called once, got %d", called)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodOptions, "/login", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for OPTIONS, got %d", rec.Code)
	}
	if called != 1 {
		t.Fatalf("expected MethodNotAllowed not to be called for OPTIONS")
	}
}

func TestRouter_HostMatching(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/ping", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("default"))
	})

	api := r.Host("api.example.com")
	if err := api.GET("/ping", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("api"))
	}); err != nil {
		t.Fatalf("host register failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Host = "api.example.com"
	r.ServeHTTP(rec, req)
	if rec.Body.String() != "api" {
		t.Fatalf("expected api host route, got %q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Host = "other.example.com"
	r.ServeHTTP(rec, req)
	if rec.Body.String() != "default" {
		t.Fatalf("expected default route, got %q", rec.Body.String())
	}
}

func TestRouter_HostWithPort(t *testing.T) {
	r := NewRouter()
	api := r.Host("api.example.com")
	if err := api.GET("/ping", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}); err != nil {
		t.Fatalf("host register failed: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Host = "api.example.com:8080"
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: " api.example.com ", want: "api.example.com"},
		{in: "API.EXAMPLE.COM", want: "api.example.com"},
		{in: "api.example.com:8080", want: "api.example.com"},
		{in: "api.example.com:abc", want: "api.example.com:abc"},
		{in: "api.example.com:", want: "api.example.com:"},
		{in: ":8080", want: ""},
		{in: "[2001:db8::1]", want: "2001:db8::1"},
		{in: "[2001:db8::1]:8443", want: "2001:db8::1"},
		{in: "[2001:db8::1]:abc", want: "[2001:db8::1]:abc"},
	}
	for _, tc := range cases {
		if got := normalizeHost(tc.in); got != tc.want {
			t.Fatalf("normalizeHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRouter_HostFallbackToDefault(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/ping", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("default"))
	})
	api := r.Host("api.example.com")
	if err := api.GET("/only-api", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("api"))
	}); err != nil {
		t.Fatalf("host register failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Host = "api.example.com"
	r.ServeHTTP(rec, req)
	if rec.Body.String() != "default" {
		t.Fatalf("expected default fallback, got %q", rec.Body.String())
	}
}

func TestRouter_HostMethodNotAllowedOverridesDefault(t *testing.T) {
	r := NewRouter()
	mustGET(t, r, "/login", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("default"))
	})
	api := r.Host("api.example.com")
	if err := api.POST("/login", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}); err != nil {
		t.Fatalf("host register failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Host = "api.example.com"
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "OPTIONS, POST" && allow != "POST, OPTIONS" {
		t.Fatalf("unexpected Allow header: %q", allow)
	}
}

func TestRouter_CustomMethod(t *testing.T) {
	r := NewRouter()
	if err := r.Handle("PURGE", "/cache", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}); err != nil {
		t.Fatalf("register custom method failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PURGE", "/cache", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/cache", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "OPTIONS, PURGE" && allow != "PURGE, OPTIONS" {
		t.Fatalf("unexpected Allow header: %q", allow)
	}
}

func TestRouter_IgnoreCase(t *testing.T) {
	r := NewRouter()
	r.IgnoreCase = true
	mustGET(t, r, "/Users/:ID", func(w http.ResponseWriter, req *http.Request) {
		id, ok := Param(w, "ID")
		if !ok || id != "AbC" {
			t.Fatalf("expected id AbC, got %q", id)
		}
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/AbC", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRouter_PanicHandler(t *testing.T) {
	r := NewRouter()
	r.PanicHandler = func(w http.ResponseWriter, req *http.Request, _ any) {
		w.WriteHeader(http.StatusTeapot)
	}
	mustGET(t, r, "/boom", func(w http.ResponseWriter, req *http.Request) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected 418, got %d", rec.Code)
	}
}

func TestFrozenRouter_IgnoreCaseAndHost(t *testing.T) {
	r := NewRouter()
	r.IgnoreCase = true
	api := r.Host("api.example.com")
	if err := api.GET("/Users/:id", func(w http.ResponseWriter, req *http.Request) {
		id, _ := Param(w, "id")
		w.Write([]byte(id))
	}); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	fr := mustFreeze(t, r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/AbC", nil)
	req.Host = "api.example.com:443"
	fr.ServeHTTP(rec, req)
	if rec.Body.String() != "AbC" {
		t.Fatalf("expected AbC, got %q", rec.Body.String())
	}
}

func TestFrozenRouter_IgnoreCase_StaticSpan(t *testing.T) {
	r := NewRouter()
	r.IgnoreCase = true
	mustGET(t, r, "/Users/List", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("ok"))
	})
	fr := mustFreeze(t, r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/USERS/LIST", nil)
	fr.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("expected body ok, got %q", rec.Body.String())
	}
}

func TestRouter_Frozen_Parity(t *testing.T) {
	r := NewRouter()
	r.IgnoreCase = true
	r.StrictSlash = true

	mustGET(t, r, "/users/:id", func(w http.ResponseWriter, req *http.Request) {
		id, _ := Param(w, "id")
		w.Write([]byte("user:" + id))
	})
	mustGET(t, r, "/static/*filepath", func(w http.ResponseWriter, req *http.Request) {
		fp, _ := Param(w, "filepath")
		w.Write([]byte("static:" + fp))
	})
	if err := r.POST("/users", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}); err != nil {
		t.Fatalf("register post failed: %v", err)
	}

	fr := mustFreeze(t, r)

	type testCase struct {
		name   string
		method string
		path   string
		host   string
	}

	cases := []testCase{
		{name: "static", method: http.MethodGet, path: "/static/css/app.css"},
		{name: "wildcard-empty", method: http.MethodGet, path: "/static"},
		{name: "param", method: http.MethodGet, path: "/users/42"},
		{name: "post", method: http.MethodPost, path: "/users"},
		{name: "notfound", method: http.MethodGet, path: "/missing"},
		{name: "method-not-allowed", method: http.MethodPut, path: "/users"},
		{name: "strict-slash-redirect", method: http.MethodGet, path: "/users/42/"},
		{name: "ignore-case", method: http.MethodGet, path: "/UsErS/AbC"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec1 := httptest.NewRecorder()
			req1 := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.host != "" {
				req1.Host = tc.host
			}
			r.ServeHTTP(rec1, req1)

			rec2 := httptest.NewRecorder()
			req2 := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.host != "" {
				req2.Host = tc.host
			}
			fr.ServeHTTP(rec2, req2)

			if rec1.Code != rec2.Code {
				t.Fatalf("status mismatch: router=%d frozen=%d", rec1.Code, rec2.Code)
			}
			if rec1.Body.String() != rec2.Body.String() {
				t.Fatalf("body mismatch: router=%q frozen=%q", rec1.Body.String(), rec2.Body.String())
			}

			allow1 := rec1.Header().Get("Allow")
			allow2 := rec2.Header().Get("Allow")
			if allow1 != allow2 {
				t.Fatalf("allow header mismatch: router=%q frozen=%q", allow1, allow2)
			}

			loc1 := rec1.Header().Get("Location")
			loc2 := rec2.Header().Get("Location")
			if loc1 != loc2 {
				t.Fatalf("location mismatch: router=%q frozen=%q", loc1, loc2)
			}
		})
	}
}

func TestRegisterPprof(t *testing.T) {
	r := NewRouter()
	if err := RegisterPprof(r, "/debug/pprof"); err == nil {
		t.Fatalf("expected RegisterPprof to require Allow policy")
	}
}

func TestRegisterPprofWith_Deny(t *testing.T) {
	r := NewRouter()
	if err := RegisterPprofWith(r, PprofOptions{
		Prefix: "/debug/pprof",
		Allow:  func(*http.Request) bool { return false },
	}); err != nil {
		t.Fatalf("register pprof failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRegisterPprofWith_Allow(t *testing.T) {
	r := NewRouter()
	if err := RegisterPprofWith(r, PprofOptions{
		Prefix: "/debug/pprof",
		Allow:  func(*http.Request) bool { return true },
	}); err != nil {
		t.Fatalf("register pprof failed: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusNotFound {
		t.Fatalf("expected pprof route to be accessible, got %d", rec.Code)
	}
}

func TestRouter_UseRawPath_EncodedParam(t *testing.T) {
	r := NewRouter()
	r.UseRawPath = true
	mustGET(t, r, "/files/:name", func(w http.ResponseWriter, req *http.Request) {
		name, _ := Param(w, "name")
		w.Write([]byte(name))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/a%2Fb", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "a%2Fb" {
		t.Fatalf("expected encoded param, got %q", rec.Body.String())
	}

	fr := mustFreeze(t, r)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/files/a%2Fb", nil)
	fr.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("frozen expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "a%2Fb" {
		t.Fatalf("frozen expected encoded param, got %q", rec.Body.String())
	}
}

func TestRouter_UseRawPath_InvalidFallback(t *testing.T) {
	r := NewRouter()
	r.UseRawPath = true
	mustGET(t, r, "/files/:name", func(w http.ResponseWriter, req *http.Request) {
		name, _ := Param(w, "name")
		w.Write([]byte(name))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/ok", nil)
	req.URL.RawPath = "/files/%2"
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("expected decoded param on fallback, got %q", rec.Body.String())
	}
}

func TestRouter_UseRawPath_SkipCleanRedirect(t *testing.T) {
	r := NewRouter()
	r.UseRawPath = true
	mustGET(t, r, "/a/b", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("ok"))
	})

	// When UseRawPath is true and RawPath equals EscapedPath, no cleaning/redirect happens.
	// Request the exact registered path to verify no redirect occurs.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/a/b", nil)
	req.URL.RawPath = "/a/b"
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Location") != "" {
		t.Fatalf("expected no redirect, got Location %q", rec.Header().Get("Location"))
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("expected ok, got %q", rec.Body.String())
	}
}

func TestRouter_Param_WithMiddlewareWrapper(t *testing.T) {
	r := NewRouter()
	rb, err := logger.NewRingBuffer(8)
	if err != nil {
		t.Fatalf("ring buffer: %v", err)
	}
	if err := r.Use(func(next http.Handler) http.Handler {
		return middleware.AccessLog(rb, next)
	}); err != nil {
		t.Fatalf("use failed: %v", err)
	}

	mustGET(t, r, "/users/:id", func(w http.ResponseWriter, req *http.Request) {
		id, ok := Param(w, "id")
		if !ok || id != "42" {
			t.Fatalf("expected id=42, got %q", id)
		}
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// nopRW for Zero-Alloc Benchmark
type nopRW struct {
	header http.Header
}

func (w *nopRW) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *nopRW) Write(b []byte) (int, error) { return len(b), nil }
func (w *nopRW) WriteHeader(statusCode int)  {}

// Benchmark
func BenchmarkRouter_Dynamic(b *testing.B) {
	r := NewRouter()
	mustGET(b, r, "/user/:name/age/:age", func(w http.ResponseWriter, req *http.Request) {})
	mustGET(b, r, "/static/path/to/resource", func(w http.ResponseWriter, req *http.Request) {})

	req, _ := http.NewRequest("GET", "/user/will/age/30", nil)
	w := &nopRW{} // Reuse outside loop

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_Static(b *testing.B) {
	r := NewRouter()
	mustGET(b, r, "/static/path/to/resource", func(w http.ResponseWriter, req *http.Request) {})

	req, _ := http.NewRequest("GET", "/static/path/to/resource", nil)
	w := &nopRW{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_Wildcard(b *testing.B) {
	r := NewRouter()
	mustGET(b, r, "/static/*filepath", func(w http.ResponseWriter, req *http.Request) {})

	req, _ := http.NewRequest("GET", "/static/css/styles.css", nil)
	w := &nopRW{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkFrozen_Dynamic(b *testing.B) {
	r := NewRouter()
	mustGET(b, r, "/user/:name/age/:age", func(w http.ResponseWriter, req *http.Request) {})
	mustGET(b, r, "/static/path/to/resource", func(w http.ResponseWriter, req *http.Request) {})
	fr := mustFreeze(b, r)

	req, _ := http.NewRequest("GET", "/user/will/age/30", nil)
	w := &nopRW{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		fr.ServeHTTP(w, req)
	}
}

func BenchmarkFrozen_Static(b *testing.B) {
	r := NewRouter()
	mustGET(b, r, "/static/path/to/resource", func(w http.ResponseWriter, req *http.Request) {})
	fr := mustFreeze(b, r)

	req, _ := http.NewRequest("GET", "/static/path/to/resource", nil)
	w := &nopRW{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		fr.ServeHTTP(w, req)
	}
}

func BenchmarkFrozen_Wildcard(b *testing.B) {
	r := NewRouter()
	mustGET(b, r, "/static/*filepath", func(w http.ResponseWriter, req *http.Request) {})
	fr := mustFreeze(b, r)

	req, _ := http.NewRequest("GET", "/static/css/styles.css", nil)
	w := &nopRW{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		fr.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_MethodNotAllowed(b *testing.B) {
	r := NewRouter()
	mustGET(b, r, "/resource", func(w http.ResponseWriter, req *http.Request) {})
	if err := r.POST("/resource", func(w http.ResponseWriter, req *http.Request) {}); err != nil {
		b.Fatalf("register post failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/resource", nil)
	w := &nopRW{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkFrozen_MethodNotAllowed(b *testing.B) {
	r := NewRouter()
	mustGET(b, r, "/resource", func(w http.ResponseWriter, req *http.Request) {})
	if err := r.POST("/resource", func(w http.ResponseWriter, req *http.Request) {}); err != nil {
		b.Fatalf("register post failed: %v", err)
	}
	fr := mustFreeze(b, r)

	req := httptest.NewRequest(http.MethodPatch, "/resource", nil)
	w := &nopRW{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		fr.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_Options(b *testing.B) {
	r := NewRouter()
	mustGET(b, r, "/resource", func(w http.ResponseWriter, req *http.Request) {})
	if err := r.POST("/resource", func(w http.ResponseWriter, req *http.Request) {}); err != nil {
		b.Fatalf("register post failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodOptions, "/resource", nil)
	w := &nopRW{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r.ServeHTTP(w, req)
	}
}

func BenchmarkFrozen_Options(b *testing.B) {
	r := NewRouter()
	mustGET(b, r, "/resource", func(w http.ResponseWriter, req *http.Request) {})
	if err := r.POST("/resource", func(w http.ResponseWriter, req *http.Request) {}); err != nil {
		b.Fatalf("register post failed: %v", err)
	}
	fr := mustFreeze(b, r)

	req := httptest.NewRequest(http.MethodOptions, "/resource", nil)
	w := &nopRW{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		fr.ServeHTTP(w, req)
	}
}
