package middleware

import (
	"net/http"
	"strconv"
	"strings"
)

// CORSOptions configures CORS behavior.
type CORSOptions struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           int
	AllowOriginFunc  func(origin string) bool
}

// DefaultCORSOptions returns a conservative default.
// Note: no origins are allowed unless explicitly configured.
func DefaultCORSOptions() CORSOptions {
	return CORSOptions{
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
		},
	}
}

// CORS applies Cross-Origin Resource Sharing headers.
func CORS(opts CORSOptions, next http.Handler) http.Handler {
	if next == nil {
		return nil
	}

	cfg := corsConfig{
		allowOrigin:     opts.AllowOriginFunc,
		allowCreds:      opts.AllowCredentials,
		exposedHeaders:  strings.Join(opts.ExposedHeaders, ", "),
		allowedMethods:  strings.Join(sanitizeTokens(opts.AllowedMethods), ", "),
		allowedHeaders:  strings.Join(sanitizeTokens(opts.AllowedHeaders), ", "),
		maxAge:          "",
		allowReqHeaders: len(opts.AllowedHeaders) == 0,
	}

	if cfg.allowedMethods == "" {
		def := DefaultCORSOptions()
		cfg.allowedMethods = strings.Join(def.AllowedMethods, ", ")
	}

	if opts.MaxAge > 0 {
		cfg.maxAge = strconv.Itoa(opts.MaxAge)
	}

	originMap := make(map[string]struct{}, len(opts.AllowedOrigins))
	for _, origin := range opts.AllowedOrigins {
		if origin == "*" {
			cfg.allowAll = true
			continue
		}
		originMap[origin] = struct{}{}
	}
	if len(originMap) > 0 && cfg.allowOrigin == nil {
		cfg.allowOrigin = func(origin string) bool {
			_, ok := originMap[origin]
			return ok
		}
	}
	if cfg.allowAll && cfg.allowCreds {
		// Disallow wildcard with credentials; require explicit allowlist or AllowOriginFunc.
		cfg.allowAll = false
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		allowed := cfg.allowAll
		if !allowed && cfg.allowOrigin != nil {
			allowed = cfg.allowOrigin(origin)
		}
		if !allowed {
			if isPreflight(r) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		setAllowOrigin(w.Header(), origin, cfg.allowAll, cfg.allowCreds)
		if cfg.allowCreds {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if cfg.exposedHeaders != "" {
			w.Header().Set("Access-Control-Expose-Headers", cfg.exposedHeaders)
		}

		if !isPreflight(r) {
			next.ServeHTTP(w, r)
			return
		}

		addVary(w.Header(), "Access-Control-Request-Method")
		addVary(w.Header(), "Access-Control-Request-Headers")

		w.Header().Set("Access-Control-Allow-Methods", cfg.allowedMethods)
		if cfg.allowReqHeaders {
			if reqHeaders := r.Header.Get("Access-Control-Request-Headers"); reqHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", reqHeaders)
			}
		} else if cfg.allowedHeaders != "" {
			w.Header().Set("Access-Control-Allow-Headers", cfg.allowedHeaders)
		}
		if cfg.maxAge != "" {
			w.Header().Set("Access-Control-Max-Age", cfg.maxAge)
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

type corsConfig struct {
	allowAll        bool
	allowOrigin     func(string) bool
	allowCreds      bool
	exposedHeaders  string
	allowedMethods  string
	allowedHeaders  string
	maxAge          string
	allowReqHeaders bool
}

func isPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
}

func setAllowOrigin(h http.Header, origin string, allowAll, allowCreds bool) {
	if allowAll && !allowCreds {
		h.Set("Access-Control-Allow-Origin", "*")
		return
	}
	addVary(h, "Origin")
	h.Set("Access-Control-Allow-Origin", origin)
}

func addVary(h http.Header, value string) {
	if prev := h.Get("Vary"); prev != "" {
		for _, v := range strings.Split(prev, ",") {
			if strings.EqualFold(strings.TrimSpace(v), value) {
				return
			}
		}
		h.Set("Vary", prev+", "+value)
		return
	}
	h.Set("Vary", value)
}

func sanitizeTokens(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}
