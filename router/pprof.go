package router

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"strings"
)

// RegisterPprof registers net/http/pprof handlers under the given prefix.
// This helper now requires an explicit Allow policy; prefer RegisterPprofWith.
// Example prefix: "/debug/pprof"
func RegisterPprof(r *Router, prefix string) error {
	return RegisterPprofWith(r, PprofOptions{Prefix: prefix})
}

// PprofOptions controls how pprof endpoints are registered.
type PprofOptions struct {
	// Prefix is the mount path. Defaults to /debug/pprof.
	Prefix string
	// Allow decides whether a request is allowed. If nil, all requests are allowed.
	Allow func(*http.Request) bool
	// Deny handles disallowed requests. Defaults to 403 if nil.
	Deny HandleFunc
}

// RegisterPprofWith registers pprof endpoints with access control.
func RegisterPprofWith(r *Router, opts PprofOptions) error {
	if r == nil {
		return fmt.Errorf("nil router")
	}
	if opts.Allow == nil {
		return fmt.Errorf("pprof requires explicit Allow policy; use RegisterPprofWith with PprofOptions.Allow")
	}
	base := cleanPath(opts.Prefix)
	if base == "/" {
		base = "/debug/pprof"
	}
	base = strings.TrimSuffix(base, "/")
	index := base + "/"

	allow := opts.Allow
	deny := opts.Deny
	wrap := func(h HandleFunc) HandleFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			if allow != nil && !allow(req) {
				if deny != nil {
					deny(w, req)
					return
				}
				w.WriteHeader(http.StatusForbidden)
				return
			}
			h(w, req)
		}
	}

	if err := r.GET(index, wrap(pprof.Index)); err != nil {
		return err
	}
	if err := r.GET(base+"/cmdline", wrap(pprof.Cmdline)); err != nil {
		return err
	}
	if err := r.GET(base+"/profile", wrap(pprof.Profile)); err != nil {
		return err
	}
	if err := r.GET(base+"/symbol", wrap(pprof.Symbol)); err != nil {
		return err
	}
	if err := r.POST(base+"/symbol", wrap(pprof.Symbol)); err != nil {
		return err
	}
	if err := r.GET(base+"/trace", wrap(pprof.Trace)); err != nil {
		return err
	}

	handlers := []string{
		"allocs",
		"block",
		"goroutine",
		"heap",
		"mutex",
		"threadcreate",
	}
	for _, name := range handlers {
		h := pprof.Handler(name)
		if err := r.GET(base+"/"+name, wrap(h.ServeHTTP)); err != nil {
			return err
		}
	}
	return nil
}
