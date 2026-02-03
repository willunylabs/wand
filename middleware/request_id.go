package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"sync/atomic"
)

const HeaderRequestID = "X-Request-ID"

var requestIDCounter uint64

// RequestIDGenerator can be overridden to avoid per-request allocations.
// If set to nil, RequestID will only pass through existing IDs.
var RequestIDGenerator = defaultRequestIDGenerator

func defaultRequestIDGenerator() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		var dst [32]byte
		hex.Encode(dst[:], b[:])
		return string(dst[:])
	}
	id := atomic.AddUint64(&requestIDCounter, 1)
	return strconv.FormatUint(id, 16)
}

// RequestID ensures an ID is available in both request and response headers.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if id == "" {
			gen := RequestIDGenerator
			if gen != nil {
				id = gen()
			}
		}
		if id != "" {
			r.Header.Set(HeaderRequestID, id)
			w.Header().Set(HeaderRequestID, id)
		}
		next.ServeHTTP(w, r)
	})
}
