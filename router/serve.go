package router

import "net/http"

// routeContext holds the preprocessed request information for routing.
// This avoids recalculating these values in both Router and FrozenRouter.
type routeContext struct {
	method     string
	matchPath  string // normalized path for matching (may be lowercased)
	paramPath  string // path for parameter extraction (raw or decoded)
	useRaw     bool
	redirectFn func(http.ResponseWriter, *http.Request, string)
}

// prepareRouteContext preprocesses the request and returns a routeContext.
// Returns nil if the request should not be processed further (e.g., already responded).
func prepareRouteContext(w http.ResponseWriter, req *http.Request, useRawPath, ignoreCase bool) (routeContext, bool) {
	useRaw := useRawPath && req.URL.RawPath != "" && req.URL.RawPath == req.URL.EscapedPath()

	if len(req.URL.Path) > MaxPathLength {
		w.WriteHeader(http.StatusRequestURITooLong)
		return routeContext{}, false
	}
	if useRaw && len(req.URL.RawPath) > MaxPathLength {
		w.WriteHeader(http.StatusRequestURITooLong)
		return routeContext{}, false
	}

	cleaned := req.URL.Path
	if !useRaw {
		cleaned = cleanPath(req.URL.Path)
		if len(cleaned) > MaxPathLength {
			w.WriteHeader(http.StatusRequestURITooLong)
			return routeContext{}, false
		}
		if cleaned != req.URL.Path {
			redirectToPath(w, req, cleaned)
			return routeContext{}, false
		}
	}

	matchPath := cleaned
	paramPath := cleaned
	redirectFn := redirectToPath
	if useRaw {
		matchPath = req.URL.RawPath
		paramPath = req.URL.RawPath
		redirectFn = redirectToRawPath
	}
	if ignoreCase {
		matchPath = lowerASCII(matchPath)
	}

	return routeContext{
		method:     req.Method,
		matchPath:  matchPath,
		paramPath:  paramPath,
		useRaw:     useRaw,
		redirectFn: redirectFn,
	}, true
}

// respondMethodNotAllowed writes the 405 response with Allow header.
func respondMethodNotAllowed(w http.ResponseWriter, req *http.Request, allow string, handler HandleFunc) bool {
	w.Header().Set("Allow", allow)
	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return true
	}
	if handler != nil {
		handler(w, req)
		return true
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
	return true
}

// alternatePath returns the path with the trailing slash toggled.
func alternatePath(p string) (string, bool) {
	if p == "" || p == "/" {
		return "", false
	}
	if p[len(p)-1] == '/' {
		return p[:len(p)-1], true
	}
	return p + "/", true
}
