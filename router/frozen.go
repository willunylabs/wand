package router

import (
	"net/http"
	"strings"
	"sync"
)

type FrozenRouter struct {
	table     frozenTable
	hosts     map[string]*frozenTable
	paramPool sync.Pool
	partsPool sync.Pool
	rwPool    sync.Pool

	NotFound         HandleFunc
	MethodNotAllowed HandleFunc
	PanicHandler     func(http.ResponseWriter, *http.Request, any)
	IgnoreCase       bool
	StrictSlash      bool
	UseRawPath       bool
}

type frozenTable struct {
	roots       map[string]*frozenNode
	static      map[string]map[string]HandleFunc
	staticAllow map[string]string
	hasParams   map[string]bool
	anyParams   bool
}

type frozenNode struct {
	staticSpan     string
	spanSegs       int
	part           string
	staticChildren *frozenStaticChildren
	paramChild     *frozenNode
	wildChild      *frozenNode
	pattern        string
	handler        HandleFunc
	hasParams      bool
}

const frozenStaticThreshold = 4

type frozenStaticChild struct {
	part string
	node *frozenNode
}

type frozenStaticChildren struct {
	small []frozenStaticChild
	m     map[string]*frozenNode
}

func (s *frozenStaticChildren) get(part string) *frozenNode {
	if s == nil {
		return nil
	}
	if s.m != nil {
		return s.m[part]
	}
	for i := range s.small {
		if s.small[i].part == part {
			return s.small[i].node
		}
	}
	return nil
}

func (s *frozenStaticChildren) set(part string, child *frozenNode) {
	if s.m != nil {
		s.m[part] = child
		return
	}
	for i := range s.small {
		if s.small[i].part == part {
			s.small[i].node = child
			return
		}
	}
	if len(s.small) < frozenStaticThreshold {
		s.small = append(s.small, frozenStaticChild{part: part, node: child})
		return
	}
	m := make(map[string]*frozenNode, len(s.small)+1)
	for _, e := range s.small {
		m[e.part] = e.node
	}
	m[part] = child
	s.small = nil
	s.m = m
}

func NewFrozenRouter() *FrozenRouter {
	fr := &FrozenRouter{
		table: newFrozenTable(),
		hosts: make(map[string]*frozenTable),
		// Best-practice default: normalize trailing slashes with redirects.
		StrictSlash: true,
	}
	fr.paramPool = sync.Pool{
		New: func() interface{} {
			return &Params{Keys: make([]string, 0, 6), Values: make([]string, 0, 6)}
		},
	}
	fr.partsPool = sync.Pool{
		New: func() interface{} {
			return &pathSegments{parts: make([]string, 0, 20), indices: make([]int, 0, 21)}
		},
	}
	fr.rwPool = sync.Pool{
		New: func() interface{} { return &paramRW{} },
	}
	return fr
}

func newFrozenTable() frozenTable {
	return frozenTable{
		roots:       make(map[string]*frozenNode),
		static:      make(map[string]map[string]HandleFunc),
		staticAllow: make(map[string]string),
		hasParams:   make(map[string]bool),
	}
}

func (r *Router) Freeze() (*FrozenRouter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	fr := NewFrozenRouter()
	if ft := freezeTable(&r.table); ft != nil {
		fr.table = *ft
	}
	for host, table := range r.hosts {
		fr.hosts[host] = freezeTable(table)
	}
	if r.ignoreCaseSet {
		fr.IgnoreCase = r.ignoreCaseEnabled
	} else {
		fr.IgnoreCase = r.IgnoreCase
	}
	fr.StrictSlash = r.StrictSlash
	fr.UseRawPath = r.UseRawPath
	fr.NotFound = r.NotFound
	fr.MethodNotAllowed = r.MethodNotAllowed
	fr.PanicHandler = r.PanicHandler

	return fr, nil
}

func freezeTable(src *routeTable) *frozenTable {
	if src == nil {
		return nil
	}
	ft := newFrozenTable()
	ft.static = cloneStatic(src.static)
	ft.staticAllow = cloneStaticAllow(src.staticAllow)
	ft.hasParams = cloneHasParams(src.hasParams)
	ft.anyParams = src.anyParams
	for method, root := range src.roots {
		ft.roots[method] = freezeRoot(root)
	}
	return &ft
}

func cloneStatic(src map[string]map[string]HandleFunc) map[string]map[string]HandleFunc {
	if src == nil {
		return nil
	}
	dst := make(map[string]map[string]HandleFunc, len(src))
	for method, m := range src {
		if m == nil {
			continue
		}
		inner := make(map[string]HandleFunc, len(m))
		for k, v := range m {
			inner[k] = v
		}
		dst[method] = inner
	}
	return dst
}

func cloneHasParams(src map[string]bool) map[string]bool {
	if src == nil {
		return nil
	}
	dst := make(map[string]bool, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneStaticAllow(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func freezeRoot(root *node) *frozenNode {
	if root == nil {
		return nil
	}
	fn := &frozenNode{
		pattern:   root.pattern,
		handler:   root.handler,
		hasParams: root.hasParams,
	}
	if root.staticChildren != nil {
		root.staticChildren.rangeFn(func(_ string, child *node) bool {
			parts, end := compressStaticChain(child)
			fchild := buildFrozenStatic(parts, end)
			if fn.staticChildren == nil {
				fn.staticChildren = &frozenStaticChildren{}
			}
			fn.staticChildren.set(parts[0], fchild)
			return true
		})
	}
	if root.paramChild != nil {
		fn.paramChild = buildFrozenNode(root.paramChild)
	}
	if root.wildChild != nil {
		fn.wildChild = buildFrozenNode(root.wildChild)
	}
	return fn
}

func compressStaticChain(start *node) ([]string, *node) {
	parts := make([]string, 0, 4)
	cur := start
	for cur != nil {
		parts = append(parts, cur.part)
		if cur.pattern != "" || cur.handler != nil || cur.paramChild != nil || cur.wildChild != nil {
			return parts, cur
		}
		if cur.staticChildren == nil || cur.staticChildren.len() != 1 {
			return parts, cur
		}
		_, next, ok := cur.staticChildren.only()
		if !ok {
			return parts, cur
		}
		cur = next
	}
	return parts, cur
}

func buildFrozenStatic(parts []string, end *node) *frozenNode {
	fn := &frozenNode{
		staticSpan: strings.Join(parts, "/"),
		spanSegs:   len(parts),
		pattern:    end.pattern,
		handler:    end.handler,
		hasParams:  end.hasParams,
	}
	if end.staticChildren != nil {
		end.staticChildren.rangeFn(func(_ string, child *node) bool {
			childParts, childEnd := compressStaticChain(child)
			fchild := buildFrozenStatic(childParts, childEnd)
			if fn.staticChildren == nil {
				fn.staticChildren = &frozenStaticChildren{}
			}
			fn.staticChildren.set(childParts[0], fchild)
			return true
		})
	}
	if end.paramChild != nil {
		fn.paramChild = buildFrozenNode(end.paramChild)
	}
	if end.wildChild != nil {
		fn.wildChild = buildFrozenNode(end.wildChild)
	}
	return fn
}

func buildFrozenNode(n *node) *frozenNode {
	if n == nil {
		return nil
	}
	fn := &frozenNode{
		part:      n.part,
		pattern:   n.pattern,
		handler:   n.handler,
		hasParams: n.hasParams,
	}
	if n.staticChildren != nil {
		n.staticChildren.rangeFn(func(_ string, child *node) bool {
			parts, end := compressStaticChain(child)
			fchild := buildFrozenStatic(parts, end)
			if fn.staticChildren == nil {
				fn.staticChildren = &frozenStaticChildren{}
			}
			fn.staticChildren.set(parts[0], fchild)
			return true
		})
	}
	if n.paramChild != nil {
		fn.paramChild = buildFrozenNode(n.paramChild)
	}
	if n.wildChild != nil {
		fn.wildChild = buildFrozenNode(n.wildChild)
	}
	return fn
}

func (r *FrozenRouter) getParts(path string) (*pathSegments, bool) {
	return r.getPartsWithRaw(path, path)
}

func (r *FrozenRouter) getPartsWithRaw(path, raw string) (*pathSegments, bool) {
	segs := r.partsPool.Get().(*pathSegments)

	segs.path = raw
	segs.match = path
	segs.parts = segs.parts[:0]
	segs.indices = segs.indices[:0]

	start := 0
	n := len(path)
	for i := 0; i < n; i++ {
		if path[i] == '\x00' || path[i] == '\r' || path[i] == '\n' {
			r.partsPool.Put(segs)
			return nil, false
		}
		if path[i] == '/' {
			if start < i {
				segs.parts = append(segs.parts, path[start:i])
				segs.indices = append(segs.indices, start)
			}
			start = i + 1
		}
	}
	if start < n {
		segs.parts = append(segs.parts, path[start:])
		segs.indices = append(segs.indices, start)
	}
	segs.indices = append(segs.indices, n)
	return segs, true
}

func (r *FrozenRouter) tableForHost(host string) *frozenTable {
	if host == "" {
		return &r.table
	}
	if t, ok := r.hosts[host]; ok && t != nil {
		return t
	}
	return &r.table
}

func (r *FrozenRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r.PanicHandler != nil {
		defer func() {
			if rec := recover(); rec != nil {
				r.PanicHandler(w, req, rec)
			}
		}()
	}

	ctx, ok := prepareRouteContext(w, req, r.UseRawPath, r.IgnoreCase)
	if !ok {
		return // Already responded (redirect or error)
	}

	host := normalizeHost(req.Host)
	hostTable := r.tableForHost(host)
	hasHost := host != "" && hostTable != &r.table
	defaultTable := &r.table

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

func (r *FrozenRouter) tryAlternateSlashInTable(w http.ResponseWriter, req *http.Request, ctx routeContext, table *frozenTable) bool {
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

func (r *FrozenRouter) handleMethodNotAllowedInTable(w http.ResponseWriter, req *http.Request, ctx routeContext, table *frozenTable) bool {
	if allow, ok := r.allowedMethodsInTable(ctx.matchPath, table); ok {
		return respondMethodNotAllowed(w, req, allow, r.MethodNotAllowed)
	}
	if !r.StrictSlash {
		if altMatch, ok := alternatePath(ctx.matchPath); ok {
			if allow, ok := r.allowedMethodsInTable(altMatch, table); ok {
				return respondMethodNotAllowed(w, req, allow, r.MethodNotAllowed)
			}
		}
	}
	return false
}

func (r *FrozenRouter) serveInTable(w http.ResponseWriter, req *http.Request, method, matchPath, rawPath string, table *frozenTable) bool {
	if method == http.MethodHead {
		if r.serveMethodInTable(w, req, http.MethodHead, matchPath, rawPath, table) {
			return true
		}
		return r.serveMethodInTable(w, req, http.MethodGet, matchPath, rawPath, table)
	}
	return r.serveMethodInTable(w, req, method, matchPath, rawPath, table)
}

func (r *FrozenRouter) serveMethodInTable(w http.ResponseWriter, req *http.Request, method, matchPath, rawPath string, table *frozenTable) bool {
	if m, ok := table.static[method]; ok {
		if handler, ok := m[matchPath]; ok {
			handler(w, req)
			return true
		}
		if !table.hasParams[method] {
			return false
		}
	} else if !table.hasParams[method] {
		return false
	}

	segs, ok := r.getPartsWithRaw(matchPath, rawPath)
	if !ok {
		return false
	}
	if len(segs.parts) > MaxDepth {
		r.partsPool.Put(segs)
		return false
	}
	cleanupParts := func() { r.partsPool.Put(segs) }

	root, ok := table.roots[method]
	if !ok {
		cleanupParts()
		return false
	}

	node := root.search(segs, 0, nil)
	if node != nil && node.handler != nil {
		handler := node.handler
		hasParams := node.hasParams

		if !hasParams {
			handler(w, req)
			cleanupParts()
			return true
		}

		params := r.paramPool.Get().(*Params)
		params.Reset()
		_ = root.search(segs, 0, params)

		prw := r.rwPool.Get().(*paramRW)
		prw.ResponseWriter = w
		prw.params = params

		handler(prw, req)

		resetParamRW(prw)
		r.rwPool.Put(prw)
		r.paramPool.Put(params)
		cleanupParts()
		return true
	}

	cleanupParts()
	return false
}

func (r *FrozenRouter) allowedMethodsInTable(matchPath string, table *frozenTable) (string, bool) {
	if !table.anyParams {
		if allow, ok := table.staticAllow[matchPath]; ok {
			return allow, true
		}
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
				return "", false
			}
			if len(segs.parts) > MaxDepth {
				r.partsPool.Put(segs)
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

	if bits == 0 && len(custom) == 0 {
		return "", false
	}
	if bits&allowMethodGet != 0 {
		bits |= allowMethodHead
	}
	bits |= allowMethodOptions

	return buildAllowHeader(bits, custom), true
}

func (n *frozenNode) search(segs *pathSegments, height int, params *Params) *frozenNode {
	parts := segs.parts
	if height > MaxDepth {
		return nil
	}

	if n.spanSegs > 0 {
		if height+n.spanSegs > len(parts) {
			return nil
		}
		start := segs.indices[height]
		last := height + n.spanSegs - 1
		end := segs.indices[last] + len(parts[last])
		if segs.match[start:end] != n.staticSpan {
			return nil
		}
		height += n.spanSegs
	}

	if height == len(parts) || (len(n.part) > 0 && n.part[0] == '*') {
		if n.pattern == "" {
			if height == len(parts) && n.wildChild != nil {
				return n.wildChild.search(segs, height, params)
			}
			return nil
		}
		if len(n.part) > 0 && n.part[0] == '*' && params != nil {
			start := segs.indices[height]
			if start < len(segs.path) && segs.path[start] == '/' {
				start++
			}
			params.Add(n.part[1:], segs.path[start:])
		}
		return n
	}

	part := parts[height]

	if n.staticChildren != nil {
		if child := n.staticChildren.get(part); child != nil {
			if res := child.search(segs, height, params); res != nil {
				return res
			}
		}
	}

	if child := n.paramChild; child != nil {
		snapshot := 0
		if params != nil {
			snapshot = len(params.Keys)
			start := segs.indices[height]
			end := start + len(parts[height])
			value := part
			if start >= 0 && end <= len(segs.path) {
				value = segs.path[start:end]
			}
			params.Add(child.part[1:], value)
		}
		if res := child.search(segs, height+1, params); res != nil {
			return res
		}
		if params != nil {
			params.Keys = params.Keys[:snapshot]
			params.Values = params.Values[:snapshot]
		}
	}

	if child := n.wildChild; child != nil {
		if res := child.search(segs, height, params); res != nil {
			return res
		}
	}

	return nil
}
