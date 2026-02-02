package router

import (
	"fmt"
	"strings"
)

const MaxDepth = 50 // Maximum route depth (DoS protection).

type node struct {
	pattern string // full route pattern, e.g. /hello/:name (only set on leaf)
	part    string // current segment: static "users", param ":id", wildcard "*filepath"

	// Structured children (O(1) hot path + two pointers).
	// [Design]: We use distinct fields for different node types to minimize pointer chasing and type assertions.
	// - staticChildren: For exact string matches (e.g. "users").
	// - paramChild:     For wildcard matches (e.g. ":id"). Only one per level allowed.
	// - wildChild:      For catch-all matches (e.g. "*any"). Must be at the end.
	staticChildren *staticChildren // static children: part -> *node
	paramChild     *node           // param child (at most one per level)
	wildChild      *node           // wildcard child (at most one per level; '*' must be last)

	// Leaf nodes store handler directly (avoid Router.handlers map + key build).
	handler HandleFunc

	// [Optimization]: leaf-only flag for param routes (skip params on static routes).
	hasParams bool
}

const staticChildThreshold = 4

type staticChild struct {
	part string
	node *node
}

type staticChildren struct {
	small []staticChild
	m     map[string]*node
}

func (s *staticChildren) len() int {
	if s == nil {
		return 0
	}
	if s.m != nil {
		return len(s.m)
	}
	return len(s.small)
}

func (s *staticChildren) only() (string, *node, bool) {
	if s == nil {
		return "", nil, false
	}
	if s.m != nil {
		if len(s.m) != 1 {
			return "", nil, false
		}
		for part, child := range s.m {
			return part, child, true
		}
		return "", nil, false
	}
	if len(s.small) != 1 {
		return "", nil, false
	}
	return s.small[0].part, s.small[0].node, true
}

func (s *staticChildren) rangeFn(fn func(part string, child *node) bool) {
	if s == nil {
		return
	}
	if s.m != nil {
		for part, child := range s.m {
			if !fn(part, child) {
				return
			}
		}
		return
	}
	for i := range s.small {
		if !fn(s.small[i].part, s.small[i].node) {
			return
		}
	}
}

func (s *staticChildren) get(part string) *node {
	if s == nil {
		return nil
	}
	if s.m != nil {
		return s.m[part]
	}
	for i := len(s.small) - 1; i >= 0; i-- {
		if s.small[i].part == part {
			return s.small[i].node
		}
	}
	return nil
}

func (s *staticChildren) set(part string, child *node) {
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
	if len(s.small) < staticChildThreshold {
		s.small = append(s.small, staticChild{part: part, node: child})
		return
	}
	m := make(map[string]*node, len(s.small)+1)
	for _, e := range s.small {
		m[e.part] = e.node
	}
	m[part] = child
	s.small = nil
	s.m = m
}

func (n *node) String() string {
	return "node{pattern=" + n.pattern + ", part=" + n.part + "}"
}

// insert recursively inserts a route (fail fast).
func (n *node) insert(pattern string, parts []string, height int, handler HandleFunc, routeHasParams bool) error {
	// [Safety]: DoS protection (depth explosion).
	if height > MaxDepth {
		return fmt.Errorf("route too deep, possible DoS attack: %s", pattern)
	}

	// Base case: at leaf.
	if height == len(parts) {
		if n.pattern != "" {
			return fmt.Errorf("duplicate route: %s", pattern)
		}
		if handler == nil {
			return fmt.Errorf("nil handler for route: %s", pattern)
		}
		n.pattern = pattern
		n.handler = handler // attach handler
		n.hasParams = routeHasParams
		return nil
	}

	part := parts[height]
	if len(part) > 0 && (part[0] == ':' || part[0] == '*') {
		routeHasParams = true
	}

	// registration-time validation
	if len(part) > 0 {
		if part[0] == '*' {
			// '*' must be the last segment
			if len(parts) > height+1 {
				return fmt.Errorf("wildcard * must be at the end of path: %s", pattern)
			}
			if len(part) == 1 {
				return fmt.Errorf("wildcard must have a name (e.g., *filepath): %s", pattern)
			}
		}
		if part[0] == ':' {
			if len(part) == 1 {
				return fmt.Errorf("parameter must have a name (e.g., :id): %s", pattern)
			}
		}
	}

	cleaned, err := sanitizePart(part)
	if err != nil {
		return err
	}
	part = cleaned

	// select or create child
	child := n.matchChildForInsert(part)
	if child != nil {
		// [Conflict Detection]: detect same-level param/wildcard name conflicts.
		// Rule: /users/:id and /users/:name is a conflict.
		if len(part) > 0 {
			if part[0] == ':' && child.part != part {
				return fmt.Errorf("conflict: parameter '%s' conflicts with existing '%s' in path '%s' at index %d", part, child.part, pattern, height)
			}
			if part[0] == '*' && child.part != part {
				return fmt.Errorf("conflict: wildcard '%s' conflicts with existing '%s' in path '%s' at index %d", part, child.part, pattern, height)
			}
		}
	} else {
		child = &node{part: part}
		switch part[0] {
		case ':':
			// [Conflict Detection]: only one of param or wildcard per level
			if n.wildChild != nil {
				return fmt.Errorf("conflict: parameter '%s' conflicts with existing wildcard '%s' in path '%s' at index %d", part, n.wildChild.part, pattern, height)
			}
			// one param child per level
			n.paramChild = child
		case '*':
			// [Conflict Detection]: only one of param or wildcard per level
			if n.paramChild != nil {
				return fmt.Errorf("conflict: wildcard '%s' conflicts with existing parameter '%s' in path '%s' at index %d", part, n.paramChild.part, pattern, height)
			}
			// one wildcard child per level
			n.wildChild = child
		default:
			if n.staticChildren == nil {
				n.staticChildren = &staticChildren{}
			}
			n.staticChildren.set(part, child)
		}
	}

	return child.insert(pattern, parts, height+1, handler, routeHasParams)
}

// search recursively matches a route (Static > Param > Wild).
// [Algorithmic Detail]:
// The search function uses recursion but relies on the `segs` struct to avoid string slicing.
// At each level `height`, we look at `segs.parts[height]`.
//
// 1. Check Static Children: O(1) in map or O(N) in small slice.
// 2. Check Param Child: matches anything except empty (unless wildcard involved).
// 3. Check Wildcard Child: matches EVERYTHING remaining.
//
// Backtracking is implicitly handled by the order of checks. If Static fails, we try Param.
func (n *node) search(segs *pathSegments, height int, params *Params) *node {
	parts := segs.parts
	if height > MaxDepth {
		return nil
	}

	// Base case: path exhausted or current node is wildcard.
	if height == len(parts) || (len(n.part) > 0 && n.part[0] == '*') {
		if n.pattern == "" {
			// [Fix]: if we end at a static node that is not a leaf and it has a wildcard child,
			// the wildcard should match the empty remainder (e.g., /static/ matches /static/*filepath).
			if height == len(parts) && n.wildChild != nil {
				return n.wildChild.search(segs, height, params)
			}
			return nil
		}
		// wildcard capture
		if len(n.part) > 0 && n.part[0] == '*' && params != nil {
			// [Optimization]: zero-alloc slicing using indices and original path.
			// With sentinel indices[len(parts)] = len(path), this is always safe.
			start := segs.indices[height]
			if start < len(segs.path) && segs.path[start] == '/' {
				start++
			}
			params.Add(n.part[1:], segs.path[start:])
		}
		return n
	}

	part := parts[height]

	// 1) Static (small slice or map)
	if n.staticChildren != nil {
		if child := n.staticChildren.get(part); child != nil {
			if res := child.search(segs, height+1, params); res != nil {
				return res
			}
		}
	}

	// 2) Param
	if child := n.paramChild; child != nil {
		snapshot := 0
		if params != nil {
			snapshot = len(params.Keys)
			// child.part looks like ":id"
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

		// Backtracking
		if params != nil {
			params.Keys = params.Keys[:snapshot]
			params.Values = params.Values[:snapshot]
		}
	}

	// 3) Wildcard (consume from current segment; pass height)
	if child := n.wildChild; child != nil {
		if res := child.search(segs, height, params); res != nil {
			return res
		}
	}

	return nil
}

// matchChildForInsert reuses a child by type.
func (n *node) matchChildForInsert(part string) *node {
	if part == "" {
		return nil
	}
	switch part[0] {
	case ':':
		return n.paramChild
	case '*':
		return n.wildChild
	default:
		if n.staticChildren == nil {
			return nil
		}
		return n.staticChildren.get(part)
	}
}

// Params holds route parameters.
// [Optimization]: with sync.Pool this is amortized zero-alloc.
// Note: if params exceed the preallocated capacity, slice growth will allocate.
type Params struct {
	Keys   []string
	Values []string
}

func (p *Params) Add(key, value string) {
	p.Keys = append(p.Keys, key)
	p.Values = append(p.Values, value)
}

func (p *Params) Reset() {
	p.Keys = p.Keys[:0]
	p.Values = p.Values[:0]
}

func (p *Params) Get(key string) (string, bool) {
	for i, k := range p.Keys {
		if k == key {
			return p.Values[i], true
		}
	}
	return "", false
}

func sanitizePart(part string) (string, error) {
	if part == "" {
		return "", fmt.Errorf("invalid part in route: empty")
	}
	// Disallow '/' (even though split already handles it) and control chars.
	if strings.ContainsAny(part, "/\x00\r\n") {
		return "", fmt.Errorf("invalid part in route: %s", part)
	}
	return part, nil
}
