package middleware

import (
	"net/http"
	"time"
)

// Timeout enforces a request timeout using http.TimeoutHandler.
func Timeout(d time.Duration, next http.Handler) http.Handler {
	if d <= 0 {
		return next
	}
	return http.TimeoutHandler(next, d, "")
}
