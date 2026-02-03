package middleware

import (
	"log"
	"net/http"
	"runtime/debug"
)

// RecoveryOptions configures panic recovery behavior.
type RecoveryOptions struct {
	// Logger is called with the panic value and stack trace.
	// Defaults to log.Printf if nil (when LogStack is true).
	Logger func(*http.Request, any, []byte)
	// Handler writes the HTTP response for a recovered panic.
	// Defaults to 500 if nil.
	Handler func(http.ResponseWriter, *http.Request, any)
	// LogStack controls whether a stack trace is captured and logged.
	// Defaults to true.
	LogStack *bool
}

// Recovery recovers from panics, logs a stack trace, and returns 500.
func Recovery(next http.Handler) http.Handler {
	return RecoveryWith(RecoveryOptions{})(next)
}

// RecoveryWith returns a middleware with custom logging and response behavior.
func RecoveryWith(opts RecoveryOptions) func(http.Handler) http.Handler {
	logger := opts.Logger
	logStack := true
	if opts.LogStack != nil {
		logStack = *opts.LogStack
	}
	if logger == nil {
		if logStack {
			logger = func(r *http.Request, rec any, stack []byte) {
				log.Printf("panic recovered: %v\n%s", rec, stack)
			}
		} else {
			logger = func(r *http.Request, rec any, stack []byte) {
				_ = stack
				log.Printf("panic recovered: %v", rec)
			}
		}
	}
	handler := opts.Handler
	if handler == nil {
		handler = func(w http.ResponseWriter, _ *http.Request, _ any) {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}

	return func(next http.Handler) http.Handler {
		if next == nil {
			return nil
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					var stack []byte
					if logStack {
						stack = debug.Stack()
					}
					logger(r, rec, stack)
					handler(w, r, rec)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
