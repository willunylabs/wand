package router

import (
	"fmt"
	"net/http"
	"strings"
)

// Use appends global middlewares to the router.
// It must be called before any routes are registered.
func (r *Router) Use(mw ...Middleware) error {
	if len(mw) == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.routesCount > 0 {
		return fmt.Errorf("cannot add middleware after routes are registered")
	}
	r.middlewares = append(r.middlewares, mw...)
	return nil
}

// Group creates a new route group with a path prefix and middlewares.
func (r *Router) Group(prefix string, mw ...Middleware) *Group {
	return newGroup(r, "", cleanPrefix(prefix), mw)
}

// Host creates a host-specific route group.
func (r *Router) Host(host string) *Group {
	return newGroup(r, normalizeHost(host), "", nil)
}

// Group represents a nested routing group with its own prefix and middleware chain.
// [Design Pattern: Composition]
// Groups allow sharing configuration (middleware, layout) across multiple routes.
// - Nested groups inherit parent middlewares (Parent -> Child -> Grandchild).
// - Paths are joined efficiently (e.g. /v1 + /users = /v1/users).
type Group struct {
	router      *Router
	host        string
	prefix      string
	middlewares []Middleware
}

// Use appends middlewares to the group.
func (g *Group) Use(mw ...Middleware) *Group {
	if len(mw) == 0 {
		return g
	}
	g.middlewares = append(g.middlewares, mw...)
	return g
}

// Group creates a nested group that inherits the parent's prefix and middlewares.
// [Middleware Inheritance]:
// We copy the parent's middleware slice to the new group.
// This ensures that modifying the parent later doesn't affect the child (snapshot),
// and modifying the child doesn't affect the parent (isolation).
func (g *Group) Group(prefix string, mw ...Middleware) *Group {
	combined := make([]Middleware, 0, len(g.middlewares)+len(mw))
	combined = append(combined, g.middlewares...)
	combined = append(combined, mw...)
	return newGroup(g.router, g.host, joinPaths(g.prefix, cleanPrefix(prefix)), combined)
}

// Handle registers a route with the group's prefix and middlewares.
func (g *Group) Handle(method, pattern string, handler HandleFunc) error {
	return g.router.handle(g.host, method, joinPaths(g.prefix, pattern), handler, g.middlewares)
}

func (g *Group) GET(pattern string, handler HandleFunc) error {
	return g.Handle(http.MethodGet, pattern, handler)
}

func (g *Group) HEAD(pattern string, handler HandleFunc) error {
	return g.Handle(http.MethodHead, pattern, handler)
}

func (g *Group) POST(pattern string, handler HandleFunc) error {
	return g.Handle(http.MethodPost, pattern, handler)
}

func (g *Group) PUT(pattern string, handler HandleFunc) error {
	return g.Handle(http.MethodPut, pattern, handler)
}

func (g *Group) PATCH(pattern string, handler HandleFunc) error {
	return g.Handle(http.MethodPatch, pattern, handler)
}

func (g *Group) DELETE(pattern string, handler HandleFunc) error {
	return g.Handle(http.MethodDelete, pattern, handler)
}

func (g *Group) OPTIONS(pattern string, handler HandleFunc) error {
	return g.Handle(http.MethodOptions, pattern, handler)
}

func newGroup(r *Router, host, prefix string, mw []Middleware) *Group {
	chain := make([]Middleware, 0, len(mw))
	chain = append(chain, mw...)
	return &Group{
		router:      r,
		host:        host,
		prefix:      prefix,
		middlewares: chain,
	}
}

func cleanPrefix(prefix string) string {
	if prefix == "" {
		return ""
	}
	prefix = cleanPath(prefix)
	if prefix == "/" {
		return ""
	}
	return strings.TrimSuffix(prefix, "/")
}

func joinPaths(prefix, pattern string) string {
	if prefix == "" {
		return pattern
	}
	if pattern == "" {
		return prefix
	}
	if pattern[0] != '/' {
		pattern = "/" + pattern
	}
	prefix = strings.TrimSuffix(prefix, "/")
	return prefix + pattern
}

func applyMiddlewares(handler HandleFunc, mws []Middleware) (HandleFunc, error) {
	if len(mws) == 0 {
		return handler, nil
	}
	var h http.Handler = http.HandlerFunc(handler)
	for i := len(mws) - 1; i >= 0; i-- {
		mw := mws[i]
		if mw == nil {
			return nil, fmt.Errorf("nil middleware")
		}
		h = mw(h)
		if h == nil {
			return nil, fmt.Errorf("middleware returned nil handler")
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	}, nil
}

// HandleMiddleware adapts a HandleFunc middleware to the standard Middleware type.
// The composition happens at registration time, not per request.
type HandleMiddleware func(HandleFunc) HandleFunc

// WrapHandle adapts a HandleFunc middleware to Middleware.
func WrapHandle(mw HandleMiddleware) Middleware {
	if mw == nil {
		return nil
	}
	return func(next http.Handler) http.Handler {
		if next == nil {
			return nil
		}
		wrapped := mw(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
		if wrapped == nil {
			return nil
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrapped(w, r)
		})
	}
}
