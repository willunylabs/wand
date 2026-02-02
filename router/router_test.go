package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
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

func TestRouter_DuplicateParamName(t *testing.T) {
	r := NewRouter()
	if err := r.GET("/users/:id/orders/:id", func(w http.ResponseWriter, req *http.Request) {}); err == nil {
		t.Fatalf("Expected error on duplicate param name, but got none")
	}
}

func TestRouter_UnsupportedMethod(t *testing.T) {
	r := NewRouter()
	if err := r.Handle("FOO", "/foo", func(w http.ResponseWriter, req *http.Request) {}); err == nil {
		t.Fatalf("expected error for unsupported method")
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
