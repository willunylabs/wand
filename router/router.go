package router

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"path"
	"sort"
	"strings"
	"sync"
)

const MaxPathLength = 4096 // Maximum path length (DoS protection).

// --------------------------------------------------------------------------------
// [Design Philosophy]
//
// Wand uses a "Zero Allocation" strategy for its hot paths (routing).
// This is achieved by:
// 1. sync.Pool: Reusing complex objects (Params, pathSegments).
// 2. Trie: Using a prefix tree for O(k) lookups where k is path length.
// 3. Flattening: Avoiding interface{} where possible (e.g., paramRW).
//
// The result is a router that generates 0 bytes of garbage for standard requests,
// significantly reducing GC pressure in high-throughput microservices.
// --------------------------------------------------------------------------------

// HandleFunc defines the handler function type.
type HandleFunc func(http.ResponseWriter, *http.Request)

// Middleware wraps an http.Handler and returns a new http.Handler.
// Chains are composed once at registration time to avoid per-request allocations.
type Middleware func(http.Handler) http.Handler

// ParamGetter exposes route parameter lookup for handlers.
type ParamGetter interface {
	Param(key string) (string, bool)
}

// Param is a helper to read route params from the ResponseWriter.
func Param(w http.ResponseWriter, key string) (string, bool) {
	for w != nil {
		if pg, ok := w.(ParamGetter); ok {
			return pg.Param(key)
		}
		if uw, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
			w = uw.Unwrap()
			continue
		}
		break
	}
	return "", false
}

type routeTable struct {
	roots       map[string]*node
	static      map[string]map[string]HandleFunc
	staticAllow map[string]string
	hasParams   map[string]bool
	anyParams   bool
	hasTrailing bool
}

// Router holds the routing tree.
// [Concurrency]: registration is serialized with an RWMutex; concurrent ServeHTTP is safe.
// Run-time registration is supported but will block reads while the tree is updated.
type Router struct {
	mu    sync.RWMutex
	table routeTable
	hosts map[string]*routeTable // host -> routing table
	// [Memory Optimization]
	// We use sync.Pool to recycle objects. This dramatically reduces heap allocations.
	// - paramPool: Recycles *Params objects (the map-like storage for :id, :user).
	// - partsPool: Recycles *pathSegments (the slice of path parts used during search).
	// - rwPool:    Recycles *paramRW (the http.ResponseWriter wrapper for capturing params).
	paramPool sync.Pool // pool for Params (Zero Alloc Params)
	partsPool sync.Pool // pool for pathSegments (Zero Alloc Split & Indices)
	rwPool    sync.Pool // pool for paramRW wrappers (Zero Alloc Wrapper)

	middlewares       []Middleware
	routesCount       int
	ignoreCaseSet     bool
	ignoreCaseEnabled bool
	IgnoreCase        bool
	StrictSlash       bool
	UseRawPath        bool
	NotFound          HandleFunc
	MethodNotAllowed  HandleFunc
	PanicHandler      func(http.ResponseWriter, *http.Request, any)
}

// pathSegments holds path segments and original indices.
// [Optimization]: supports O(1) wildcard slicing without allocations.
type pathSegments struct {
	path    string   // original path (for wildcard slicing)
	match   string   // normalized path used for matching (e.g., lowercased)
	parts   []string // split segments
	indices []int    // start index of each segment in the normalized path
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
		table: routeTable{
			roots:       make(map[string]*node),
			static:      make(map[string]map[string]HandleFunc),
			staticAllow: make(map[string]string),
			hasParams:   make(map[string]bool),
		},
		hosts: make(map[string]*routeTable),
		// Best-practice default: normalize trailing slashes with redirects.
		StrictSlash: true,
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

// getParts splits the path with zero allocations and records indices.
// Returns *pathSegments so ServeHTTP can return it to the pool.
func (r *Router) getParts(path string) (*pathSegments, bool) {
	return r.getPartsWithRaw(path, path)
}

// getPartsWithRaw is like getParts but keeps raw for param extraction.
func (r *Router) getPartsWithRaw(path, raw string) (*pathSegments, bool) {
	segs := r.partsPool.Get().(*pathSegments)

	segs.path = raw   // store original path
	segs.match = path // store normalized path used for indices
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

func redirectToPath(w http.ResponseWriter, req *http.Request, path string) {
	u := *req.URL
	u.Path = path
	u.RawPath = ""
	code := http.StatusMovedPermanently
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		code = http.StatusPermanentRedirect
	}
	http.Redirect(w, req, u.String(), code)
}

func redirectToRawPath(w http.ResponseWriter, req *http.Request, raw string) {
	u := *req.URL
	u.RawPath = raw
	if decoded, err := neturl.PathUnescape(raw); err == nil {
		u.Path = decoded
	} else {
		u.Path = raw
		u.RawPath = ""
	}
	code := http.StatusMovedPermanently
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		code = http.StatusPermanentRedirect
	}
	http.Redirect(w, req, u.String(), code)
}

func lowerASCII(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			b := make([]byte, len(s))
			for j := 0; j < len(s); j++ {
				cc := s[j]
				if cc >= 'A' && cc <= 'Z' {
					cc += 'a' - 'A'
				}
				b[j] = cc
			}
			return string(b)
		}
	}
	return s
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	return lowerASCII(host)
}

func newRouteTable() *routeTable {
	return &routeTable{
		roots:       make(map[string]*node),
		static:      make(map[string]map[string]HandleFunc),
		staticAllow: make(map[string]string),
		hasParams:   make(map[string]bool),
	}
}

func (r *Router) tableForHostLocked(host string) *routeTable {
	if host == "" {
		return &r.table
	}
	if r.hosts == nil {
		r.hosts = make(map[string]*routeTable)
	}
	if t, ok := r.hosts[host]; ok {
		return t
	}
	t := newRouteTable()
	r.hosts[host] = t
	return t
}

func (r *Router) ignoreCaseActive() bool {
	r.mu.RLock()
	if r.ignoreCaseSet {
		v := r.ignoreCaseEnabled
		r.mu.RUnlock()
		return v
	}
	v := r.IgnoreCase
	r.mu.RUnlock()
	return v
}

func (r *Router) lockIgnoreCase() bool {
	r.mu.Lock()
	if !r.ignoreCaseSet {
		r.ignoreCaseEnabled = r.IgnoreCase
		r.ignoreCaseSet = true
	}
	v := r.ignoreCaseEnabled
	r.mu.Unlock()
	return v
}

// resetParamRW resets paramRW for reuse.
func resetParamRW(prw *paramRW) {
	prw.ResponseWriter = nil
	prw.params = nil
}

// Handle registers a route.
func (r *Router) Handle(method, pattern string, handler HandleFunc) error {
	return r.handle("", method, pattern, handler, nil)
}

func (r *Router) handle(host, method, pattern string, handler HandleFunc, groupMws []Middleware) error {
	if handler == nil {
		return fmt.Errorf("nil handler for route: %s", pattern)
	}
	if !isValidMethod(method) {
		return fmt.Errorf("unsupported method: %s", method)
	}
	cleaned := cleanPath(pattern)
	if cleaned != pattern {
		return fmt.Errorf("non-canonical pattern: %s (clean: %s)", pattern, cleaned)
	}
	if len(cleaned) > MaxPathLength {
		return fmt.Errorf("pattern too long: %s", pattern)
	}

	segs, ok := r.getParts(cleaned)
	if !ok {
		return fmt.Errorf("invalid pattern: %s", cleaned)
	}
	if len(segs.parts) > MaxDepth {
		r.partsPool.Put(segs)
		return fmt.Errorf("route too deep, possible DoS attack: %s", cleaned)
	}

	if err := validateParamNames(segs.parts, cleaned); err != nil {
		r.partsPool.Put(segs)
		return err
	}

	ignoreCase := r.lockIgnoreCase()
	matchPattern := cleaned
	matchParts := segs.parts
	if ignoreCase {
		trailingSlash := len(cleaned) > 1 && cleaned[len(cleaned)-1] == '/'
		matchParts = make([]string, len(segs.parts))
		for i, part := range segs.parts {
			if len(part) > 0 && (part[0] == ':' || part[0] == '*') {
				matchParts[i] = part
			} else {
				matchParts[i] = lowerASCII(part)
			}
		}
		if len(matchParts) == 0 {
			matchPattern = "/"
		} else {
			matchPattern = "/" + strings.Join(matchParts, "/")
			if trailingSlash {
				matchPattern += "/"
			}
		}
	}

	hasParams := false
	for _, part := range matchParts {
		if len(part) > 0 && (part[0] == ':' || part[0] == '*') {
			hasParams = true
			break
		}
	}

	if len(groupMws) > 0 {
		composed, err := applyMiddlewares(handler, groupMws)
		if err != nil {
			r.partsPool.Put(segs)
			return err
		}
		handler = composed
	}

	r.mu.RLock()
	routerMws := make([]Middleware, len(r.middlewares))
	copy(routerMws, r.middlewares)
	r.mu.RUnlock()

	if len(routerMws) > 0 {
		composed, err := applyMiddlewares(handler, routerMws)
		if err != nil {
			r.partsPool.Put(segs)
			return err
		}
		handler = composed
	}

	// insert only needs parts
	host = normalizeHost(host)

	r.mu.Lock()
	table := r.tableForHostLocked(host)
	root, ok := table.roots[method]
	if !ok {
		root = &node{}
		table.roots[method] = root
	}
	err := root.insert(matchPattern, matchParts, 0, handler, hasParams)
	if err == nil {
		r.routesCount++
		if len(matchPattern) > 1 && matchPattern[len(matchPattern)-1] == '/' {
			table.hasTrailing = true
		}
		if hasParams {
			table.hasParams[method] = true
			table.anyParams = true
		} else {
			m := table.static[method]
			if m == nil {
				m = make(map[string]HandleFunc, 8)
				table.static[method] = m
			}
			m[matchPattern] = handler
			if allow, ok := buildStaticAllowHeader(table.static, matchPattern); ok {
				table.staticAllow[matchPattern] = allow
			}
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
	if r.PanicHandler != nil {
		defer func() {
			if rec := recover(); rec != nil {
				r.PanicHandler(w, req, rec)
			}
		}()
	}

	ctx, ok := prepareRouteContext(w, req, r.UseRawPath, r.ignoreCaseActive())
	if !ok {
		return // Already responded (redirect or error)
	}

	host := normalizeHost(req.Host)

	var hostTable *routeTable
	hasHost := false
	r.mu.RLock()
	if host != "" {
		if t, ok := r.hosts[host]; ok {
			hostTable = t
			hasHost = true
		}
	}
	if hostTable == nil {
		hostTable = &r.table
	}
	defaultTable := &r.table
	r.mu.RUnlock()

	// Try host-specific table first
	if hasHost {
		if r.serveInTable(w, req, ctx.method, ctx.matchPath, ctx.paramPath, hostTable) {
			return
		}
		if r.tryAlternateSlashInTable(w, req, ctx, hostTable) {
			return
		}
		if r.handleMethodNotAllowedInTable(w, req, ctx, hostTable) {
			return
		}
	}

	// Try default table
	if r.serveInTable(w, req, ctx.method, ctx.matchPath, ctx.paramPath, defaultTable) {
		return
	}
	if r.tryAlternateSlashInTable(w, req, ctx, defaultTable) {
		return
	}
	if r.handleMethodNotAllowedInTable(w, req, ctx, defaultTable) {
		return
	}

	if r.NotFound != nil {
		r.NotFound(w, req)
		return
	}
	http.NotFound(w, req)
}

func (r *Router) tryAlternateSlashInTable(w http.ResponseWriter, req *http.Request, ctx routeContext, table *routeTable) bool {
	// Fast skip for the common "no trailing slash route exists" case.
	if len(ctx.matchPath) > 1 && ctx.matchPath[len(ctx.matchPath)-1] != '/' && !table.hasTrailing {
		return false
	}
	altMatch, ok := alternatePath(ctx.matchPath)
	if !ok || altMatch == ctx.matchPath {
		return false
	}
	if r.StrictSlash {
		if _, ok := r.allowedMethodsInTable(altMatch, table); ok {
			altRedirect, ok := alternatePath(ctx.paramPath)
			if ok && altRedirect != "" {
				ctx.redirectFn(w, req, altRedirect)
				return true
			}
		}
		return false
	}
	altParam, _ := alternatePath(ctx.paramPath)
	return r.serveInTable(w, req, ctx.method, altMatch, altParam, table)
}

func (r *Router) handleMethodNotAllowedInTable(w http.ResponseWriter, req *http.Request, ctx routeContext, table *routeTable) bool {
	if allow, ok := r.allowedMethodsInTable(ctx.matchPath, table); ok {
		return respondMethodNotAllowed(w, req, allow, r.MethodNotAllowed)
	}
	if !r.StrictSlash {
		if len(ctx.matchPath) > 1 && ctx.matchPath[len(ctx.matchPath)-1] != '/' && !table.hasTrailing {
			return false
		}
		if altMatch, ok := alternatePath(ctx.matchPath); ok {
			if allow, ok := r.allowedMethodsInTable(altMatch, table); ok {
				return respondMethodNotAllowed(w, req, allow, r.MethodNotAllowed)
			}
		}
	}
	return false
}

func (r *Router) serveInTable(w http.ResponseWriter, req *http.Request, method, matchPath, rawPath string, table *routeTable) bool {
	if method == http.MethodHead {
		if r.serveMethodInTable(w, req, http.MethodHead, matchPath, rawPath, table) {
			return true
		}
		return r.serveMethodInTable(w, req, http.MethodGet, matchPath, rawPath, table)
	}
	return r.serveMethodInTable(w, req, method, matchPath, rawPath, table)
}

func (r *Router) serveMethodInTable(w http.ResponseWriter, req *http.Request, method, matchPath, rawPath string, table *routeTable) bool {
	r.mu.RLock()
	if m, ok := table.static[method]; ok {
		if handler, ok := m[matchPath]; ok {
			r.mu.RUnlock()
			handler(w, req)
			return true
		}
		if !table.hasParams[method] {
			r.mu.RUnlock()
			return false
		}
	} else if !table.hasParams[method] {
		r.mu.RUnlock()
		return false
	}

	segs, ok := r.getPartsWithRaw(matchPath, rawPath)
	if !ok {
		r.mu.RUnlock()
		return false
	}
	if len(segs.parts) > MaxDepth {
		r.partsPool.Put(segs)
		r.mu.RUnlock()
		return false
	}

	root, ok := table.roots[method]
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

func (r *Router) allowedMethodsInTable(matchPath string, table *routeTable) (string, bool) {
	r.mu.RLock()
	if !table.anyParams {
		if allow, ok := table.staticAllow[matchPath]; ok {
			r.mu.RUnlock()
			return allow, true
		}
		r.mu.RUnlock()
		return "", false
	}

	var bits uint8
	var custom []string
	for method, m := range table.static {
		if m == nil {
			continue
		}
		if _, ok := m[matchPath]; ok {
			bits, custom = addAllowedMethod(method, bits, custom)
		}
	}

	var segs *pathSegments
	var segsOK bool
	for method, has := range table.hasParams {
		if !has {
			continue
		}
		root := table.roots[method]
		if root == nil {
			continue
		}
		if segs == nil {
			segs, segsOK = r.getParts(matchPath)
			if !segsOK {
				if segs != nil {
					r.partsPool.Put(segs)
				}
				r.mu.RUnlock()
				return "", false
			}
			if len(segs.parts) > MaxDepth {
				r.partsPool.Put(segs)
				r.mu.RUnlock()
				return "", false
			}
		}
		if root.search(segs, 0, nil) != nil {
			bits, custom = addAllowedMethod(method, bits, custom)
		}
	}

	if segs != nil {
		r.partsPool.Put(segs)
	}
	r.mu.RUnlock()

	if bits == 0 && len(custom) == 0 {
		return "", false
	}
	if bits&allowMethodGet != 0 {
		bits |= allowMethodHead
	}
	bits |= allowMethodOptions

	return buildAllowHeader(bits, custom), true
}

const (
	allowMethodGet uint8 = 1 << iota
	allowMethodHead
	allowMethodPost
	allowMethodPut
	allowMethodPatch
	allowMethodDelete
	allowMethodOptions
)

type standardMethod struct {
	method string
	bit    uint8
}

var methodOrder = []standardMethod{
	{method: http.MethodGet, bit: allowMethodGet},
	{method: http.MethodHead, bit: allowMethodHead},
	{method: http.MethodPost, bit: allowMethodPost},
	{method: http.MethodPut, bit: allowMethodPut},
	{method: http.MethodPatch, bit: allowMethodPatch},
	{method: http.MethodDelete, bit: allowMethodDelete},
	{method: http.MethodOptions, bit: allowMethodOptions},
}

var standardMethodBits = map[string]uint8{
	http.MethodGet:     allowMethodGet,
	http.MethodHead:    allowMethodHead,
	http.MethodPost:    allowMethodPost,
	http.MethodPut:     allowMethodPut,
	http.MethodPatch:   allowMethodPatch,
	http.MethodDelete:  allowMethodDelete,
	http.MethodOptions: allowMethodOptions,
}

func addAllowedMethod(method string, bits uint8, custom []string) (uint8, []string) {
	if bit, ok := standardMethodBits[method]; ok {
		return bits | bit, custom
	}
	return bits, appendCustomMethod(custom, method)
}

func buildStaticAllowHeader(static map[string]map[string]HandleFunc, path string) (string, bool) {
	var bits uint8
	var custom []string
	for method, routes := range static {
		if routes == nil {
			continue
		}
		if _, ok := routes[path]; ok {
			bits, custom = addAllowedMethod(method, bits, custom)
		}
	}
	if bits == 0 && len(custom) == 0 {
		return "", false
	}
	if bits&allowMethodGet != 0 {
		bits |= allowMethodHead
	}
	bits |= allowMethodOptions
	return buildAllowHeader(bits, custom), true
}

func appendCustomMethod(custom []string, method string) []string {
	for i := range custom {
		if custom[i] == method {
			return custom
		}
	}
	return append(custom, method)
}

func buildAllowHeader(bits uint8, custom []string) string {
	if bits == 0 && len(custom) == 0 {
		return ""
	}
	var b strings.Builder
	first := true

	for _, method := range methodOrder {
		if bits&method.bit != 0 {
			if !first {
				b.WriteString(", ")
			}
			b.WriteString(method.method)
			first = false
		}
	}

	if len(custom) > 1 {
		sort.Strings(custom)
	}
	for _, method := range custom {
		if !first {
			b.WriteString(", ")
		}
		b.WriteString(method)
		first = false
	}

	return b.String()
}

func isValidMethod(method string) bool {
	if method == "" {
		return false
	}
	for i := 0; i < len(method); i++ {
		c := method[i]
		if c <= ' ' || c == 0x7f {
			return false
		}
	}
	return true
}
