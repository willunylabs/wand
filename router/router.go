package router

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
)

const MaxPathLength = 4096 // Maximum path length (DoS protection).

const (
	methodBitGet uint8 = 1 << iota
	methodBitHead
	methodBitPost
	methodBitPut
	methodBitPatch
	methodBitDelete
	methodBitOptions
)

var allowHeaderTable [1 << 7]string

// HandleFunc defines the handler function type.
type HandleFunc func(http.ResponseWriter, *http.Request)

// ParamGetter exposes route parameter lookup for handlers.
type ParamGetter interface {
	Param(key string) (string, bool)
}

// Param is a helper to read route params from the ResponseWriter.
func Param(w http.ResponseWriter, key string) (string, bool) {
	if pg, ok := w.(ParamGetter); ok {
		return pg.Param(key)
	}
	return "", false
}

// Router holds the routing tree.
// [Concurrency]: registration is serialized with an RWMutex; concurrent ServeHTTP is safe.
// Run-time registration is supported but will block reads while the tree is updated.
type Router struct {
	mu        sync.RWMutex
	roots     map[string]*node                 // method -> root node (GET, POST...)
	static    map[string]map[string]HandleFunc // method -> (path -> handler)
	hasParams map[string]bool                  // method -> has any dynamic routes
	paramPool sync.Pool                        // pool for Params (Zero Alloc Params)
	partsPool sync.Pool                        // pool for pathSegments (Zero Alloc Split & Indices)
	rwPool    sync.Pool                        // pool for paramRW wrappers (Zero Alloc Wrapper)
}

// pathSegments holds path segments and original indices.
// [Optimization]: supports O(1) wildcard slicing without allocations.
type pathSegments struct {
	path    string   // original path (for wildcard slicing)
	parts   []string // split segments
	indices []int    // start index of each segment in the original path
}

// paramRW wraps http.ResponseWriter.
// [Performance]: value receiver to reduce escape.
type paramRW struct {
	http.ResponseWriter
	params *Params
}

func (w paramRW) Param(key string) (string, bool) {
	if w.params == nil {
		return "", false
	}
	return w.params.Get(key)
}

func (w paramRW) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Optional interfaces pass-through for compatibility with net/http features.
func (w paramRW) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w paramRW) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (w paramRW) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (w paramRW) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(w.ResponseWriter, r)
}

// NewRouter creates a new router instance.
func NewRouter() *Router {
	return &Router{
		roots:     make(map[string]*node),
		static:    make(map[string]map[string]HandleFunc),
		hasParams: make(map[string]bool),
		paramPool: sync.Pool{
			New: func() interface{} {
				return &Params{
					Keys:   make([]string, 0, 6), // preallocated capacity
					Values: make([]string, 0, 6),
				}
			},
		},
		partsPool: sync.Pool{
			New: func() interface{} {
				// [Optimization]: Pool struct to hold both parts and indices
				return &pathSegments{
					parts:   make([]string, 0, 20),
					indices: make([]int, 0, 21), // +1 for sentinel
				}
			},
		},
		rwPool: sync.Pool{
			New: func() interface{} {
				return &paramRW{}
			},
		},
	}
}

func init() {
	for mask := 0; mask < len(allowHeaderTable); mask++ {
		allowHeaderTable[mask] = buildAllowHeaderMask(uint8(mask))
	}
}

// getParts splits the path with zero allocations and records indices.
// Returns *pathSegments so ServeHTTP can return it to the pool.
func (r *Router) getParts(path string) (*pathSegments, bool) {
	segs := r.partsPool.Get().(*pathSegments)

	segs.path = path // store original path
	// Reset slices (keep capacity)
	segs.parts = segs.parts[:0]
	segs.indices = segs.indices[:0]

	start := 0
	n := len(path)
	for i := 0; i < n; i++ {
		// safety check
		if path[i] == '\x00' || path[i] == '\r' || path[i] == '\n' {
			r.partsPool.Put(segs) // return to pool
			return nil, false
		}

		if path[i] == '/' {
			if start < i {
				segs.parts = append(segs.parts, path[start:i])
				segs.indices = append(segs.indices, start) // record start index
			}
			start = i + 1
		}
	}
	if start < n {
		segs.parts = append(segs.parts, path[start:])
		segs.indices = append(segs.indices, start) // record start index
	}

	// [Sentinel]: ensure indices has len(parts)+1 so wildcard can capture empty remainder safely.
	// indices[len(parts)] == len(path)
	segs.indices = append(segs.indices, n)

	return segs, true
}

func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	cp := path.Clean(p)
	if len(cp) > 1 && p[len(p)-1] == '/' && cp[len(cp)-1] != '/' {
		cp += "/"
	}
	return cp
}

// resetParamRW resets paramRW for reuse.
func resetParamRW(prw *paramRW) {
	prw.ResponseWriter = nil
	prw.params = nil
}

// Handle registers a route.
func (r *Router) Handle(method, pattern string, handler HandleFunc) error {
	if handler == nil {
		return fmt.Errorf("nil handler for route: %s", pattern)
	}
	if !isStandardMethod(method) {
		return fmt.Errorf("unsupported method: %s", method)
	}
	cleaned := cleanPath(pattern)
	if cleaned != pattern {
		return fmt.Errorf("non-canonical pattern: %s (clean: %s)", pattern, cleaned)
	}
	pattern = cleaned
	if len(pattern) > MaxPathLength {
		return fmt.Errorf("pattern too long: %s", pattern)
	}

	segs, ok := r.getParts(cleaned)
	if !ok {
		return fmt.Errorf("invalid pattern: %s", pattern)
	}
	if len(segs.parts) > MaxDepth {
		r.partsPool.Put(segs)
		return fmt.Errorf("route too deep, possible DoS attack: %s", pattern)
	}

	if err := validateParamNames(segs.parts, pattern); err != nil {
		r.partsPool.Put(segs)
		return err
	}

	hasParams := false
	for _, part := range segs.parts {
		if len(part) > 0 && (part[0] == ':' || part[0] == '*') {
			hasParams = true
			break
		}
	}

	// insert only needs parts
	r.mu.Lock()
	root, ok := r.roots[method]
	if !ok {
		root = &node{}
		r.roots[method] = root
	}
	err := root.insert(pattern, segs.parts, 0, handler, hasParams)
	if err == nil {
		if hasParams {
			r.hasParams[method] = true
		} else {
			m := r.static[method]
			if m == nil {
				m = make(map[string]HandleFunc, 8)
				r.static[method] = m
			}
			m[pattern] = handler
		}
	}
	r.mu.Unlock()
	if err != nil {
		r.partsPool.Put(segs)
		return err
	}

	// Return segs to the pool after registration to keep pool semantics correct.
	r.partsPool.Put(segs)
	return nil
}

func validateParamNames(parts []string, pattern string) error {
	var seen map[string]struct{}
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		if part[0] == '*' && i != len(parts)-1 {
			return fmt.Errorf("wildcard * must be at the end of path: %s", pattern)
		}
		if part[0] == ':' || part[0] == '*' {
			name := part[1:]
			if name == "" {
				return fmt.Errorf("parameter must have a name (e.g., :id) in path: %s", pattern)
			}
			if seen == nil {
				seen = make(map[string]struct{}, 4)
			}
			if _, ok := seen[name]; ok {
				return fmt.Errorf("conflict: duplicate param name '%s' in path '%s' at index %d", name, pattern, i)
			}
			seen[name] = struct{}{}
		}
	}
	return nil
}

func (r *Router) GET(pattern string, handler HandleFunc) error {
	return r.Handle(http.MethodGet, pattern, handler)
}

func (r *Router) HEAD(pattern string, handler HandleFunc) error {
	return r.Handle(http.MethodHead, pattern, handler)
}

func (r *Router) POST(pattern string, handler HandleFunc) error {
	return r.Handle(http.MethodPost, pattern, handler)
}

func (r *Router) PUT(pattern string, handler HandleFunc) error {
	return r.Handle(http.MethodPut, pattern, handler)
}

func (r *Router) PATCH(pattern string, handler HandleFunc) error {
	return r.Handle(http.MethodPatch, pattern, handler)
}

func (r *Router) DELETE(pattern string, handler HandleFunc) error {
	return r.Handle(http.MethodDelete, pattern, handler)
}

func (r *Router) OPTIONS(pattern string, handler HandleFunc) error {
	return r.Handle(http.MethodOptions, pattern, handler)
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if len(req.URL.Path) > MaxPathLength {
		w.WriteHeader(http.StatusRequestURITooLong)
		return
	}
	cleaned := cleanPath(req.URL.Path)
	if len(cleaned) > MaxPathLength {
		w.WriteHeader(http.StatusRequestURITooLong)
		return
	}
	if cleaned != req.URL.Path {
		url := *req.URL
		url.Path = cleaned
		url.RawPath = ""
		code := http.StatusMovedPermanently
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			code = http.StatusPermanentRedirect
		}
		http.Redirect(w, req, url.String(), code)
		return
	}

	method := req.Method
	if method == http.MethodHead {
		if r.serveMethod(w, req, http.MethodHead, cleaned) {
			return
		}
		method = http.MethodGet
	}

	if r.serveMethod(w, req, method, cleaned) {
		return
	}

	if allow, ok := r.allowedMethods(cleaned); ok {
		w.Header().Set("Allow", allow)
		if req.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	http.NotFound(w, req)
}

func (r *Router) serveMethod(w http.ResponseWriter, req *http.Request, method, cleaned string) bool {
	r.mu.RLock()
	if m, ok := r.static[method]; ok {
		if handler, ok := m[cleaned]; ok {
			r.mu.RUnlock()
			handler(w, req)
			return true
		}
		if !r.hasParams[method] {
			r.mu.RUnlock()
			return false
		}
	} else if !r.hasParams[method] {
		r.mu.RUnlock()
		return false
	}

	segs, ok := r.getParts(cleaned)
	if !ok {
		r.mu.RUnlock()
		return false
	}
	if len(segs.parts) > MaxDepth {
		r.partsPool.Put(segs)
		r.mu.RUnlock()
		return false
	}

	root, ok := r.roots[method]
	if !ok {
		r.partsPool.Put(segs)
		r.mu.RUnlock()
		return false
	}

	node := root.search(segs, 0, nil)
	if node != nil && node.handler != nil {
		handler := node.handler
		hasParams := node.hasParams
		if !hasParams {
			r.mu.RUnlock()
			handler(w, req)
			r.partsPool.Put(segs)
			return true
		}

		params := r.paramPool.Get().(*Params)
		params.Reset()
		_ = root.search(segs, 0, params)
		r.mu.RUnlock()

		prw := r.rwPool.Get().(*paramRW)
		prw.ResponseWriter = w
		prw.params = params

		handler(prw, req)

		resetParamRW(prw)
		r.rwPool.Put(prw)
		r.paramPool.Put(params)
		r.partsPool.Put(segs)
		return true
	}

	r.partsPool.Put(segs)
	r.mu.RUnlock()
	return false
}

func (r *Router) allowedMethods(cleaned string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var mask uint8

	for method, m := range r.static {
		if m == nil {
			continue
		}
		if _, ok := m[cleaned]; ok {
			if bit, ok := methodBit(method); ok {
				mask |= bit
			}
		}
	}

	var segs *pathSegments
	var segsOK bool
	for method, has := range r.hasParams {
		if !has {
			continue
		}
		root := r.roots[method]
		if root == nil {
			continue
		}
		if segs == nil {
			segs, segsOK = r.getParts(cleaned)
			if !segsOK {
				if segs != nil {
					r.partsPool.Put(segs)
				}
				return "", false
			}
			if len(segs.parts) > MaxDepth {
				r.partsPool.Put(segs)
				return "", false
			}
		}
		if root.search(segs, 0, nil) != nil {
			if bit, ok := methodBit(method); ok {
				mask |= bit
			}
		}
	}

	if segs != nil {
		r.partsPool.Put(segs)
	}

	if mask == 0 {
		return "", false
	}

	if mask&methodBitGet != 0 {
		mask |= methodBitHead
	}
	mask |= methodBitOptions

	return allowHeaderForMask(mask), true
}

var methodOrder = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodOptions,
}

func allowHeaderForMask(mask uint8) string {
	return allowHeaderTable[mask]
}

func buildAllowHeaderMask(mask uint8) string {
	if mask == 0 {
		return ""
	}

	var b strings.Builder
	first := true
	for _, method := range methodOrder {
		if bit, ok := methodBit(method); ok && (mask&bit) != 0 {
			if !first {
				b.WriteString(", ")
			}
			b.WriteString(method)
			first = false
		}
	}
	return b.String()
}

func isStandardMethod(method string) bool {
	_, ok := methodBit(method)
	return ok
}

func methodBit(method string) (uint8, bool) {
	switch method {
	case http.MethodGet:
		return methodBitGet, true
	case http.MethodHead:
		return methodBitHead, true
	case http.MethodPost:
		return methodBitPost, true
	case http.MethodPut:
		return methodBitPut, true
	case http.MethodPatch:
		return methodBitPatch, true
	case http.MethodDelete:
		return methodBitDelete, true
	case http.MethodOptions:
		return methodBitOptions, true
	default:
		return 0, false
	}
}
