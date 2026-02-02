package router

import (
	"net/http"
	"strings"
	"sync"
)

type FrozenRouter struct {
	roots     map[string]*frozenNode
	static    map[string]map[string]HandleFunc
	hasParams map[string]bool
	paramPool sync.Pool
	partsPool sync.Pool
	rwPool    sync.Pool
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
		roots:     make(map[string]*frozenNode),
		static:    make(map[string]map[string]HandleFunc),
		hasParams: make(map[string]bool),
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

func (r *Router) Freeze() (*FrozenRouter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	fr := NewFrozenRouter()
	fr.static = cloneStatic(r.static)
	fr.hasParams = cloneHasParams(r.hasParams)

	for method, root := range r.roots {
		fr.roots[method] = freezeRoot(root)
	}
	return fr, nil
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
	segs := r.partsPool.Get().(*pathSegments)

	segs.path = path
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

func (r *FrozenRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
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

func (r *FrozenRouter) serveMethod(w http.ResponseWriter, req *http.Request, method, cleaned string) bool {
	if m, ok := r.static[method]; ok {
		if handler, ok := m[cleaned]; ok {
			handler(w, req)
			return true
		}
		if !r.hasParams[method] {
			return false
		}
	} else if !r.hasParams[method] {
		return false
	}

	segs, ok := r.getParts(cleaned)
	if !ok {
		return false
	}
	if len(segs.parts) > MaxDepth {
		r.partsPool.Put(segs)
		return false
	}
	cleanupParts := func() { r.partsPool.Put(segs) }

	root, ok := r.roots[method]
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

func (r *FrozenRouter) allowedMethods(cleaned string) (string, bool) {
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
		if segs.path[start:end] != n.staticSpan {
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
			params.Add(child.part[1:], part)
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
