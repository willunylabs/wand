package middleware

import (
	"net"
	"net/http"
	"strings"
)

// XForwardedFor returns a list of IPs from the X-Forwarded-For header.
// This function does not apply any trust policy.
func XForwardedFor(r *http.Request) []string {
	if r == nil {
		return nil
	}
	return splitCSV(r.Header.Get("X-Forwarded-For"))
}

// XForwardedProto returns the first value from X-Forwarded-Proto.
func XForwardedProto(r *http.Request) string {
	if r == nil {
		return ""
	}
	return firstCSV(r.Header.Get("X-Forwarded-Proto"))
}

// XForwardedHost returns the first value from X-Forwarded-Host.
func XForwardedHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	return firstCSV(r.Header.Get("X-Forwarded-Host"))
}

// ProxyTrustFunc reports whether an IP is a trusted proxy.
// Return true only for addresses you control (e.g., your load balancers).
type ProxyTrustFunc func(ip string) bool

// ClientIP returns the best-effort client IP, considering X-Forwarded-For
// only when the immediate peer is trusted.
func ClientIP(r *http.Request, trust ProxyTrustFunc) string {
	if r == nil {
		return ""
	}
	remote := remoteIP(r.RemoteAddr)
	if trust == nil || !trust(remote) {
		return remote
	}
	xff := splitCSV(r.Header.Get("X-Forwarded-For"))
	if len(xff) == 0 {
		return remote
	}
	for i := len(xff) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(xff[i])
		if ip == "" {
			continue
		}
		if !trust(ip) {
			return ip
		}
	}
	return strings.TrimSpace(xff[0])
}

// NewCIDRTrustFunc returns a ProxyTrustFunc for a list of CIDRs.
func NewCIDRTrustFunc(cidrs []string) (ProxyTrustFunc, error) {
	if len(cidrs) == 0 {
		return nil, nil
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, raw := range cidrs {
		c := strings.TrimSpace(raw)
		if c == "" {
			continue
		}
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, err
		}
		nets = append(nets, n)
	}
	if len(nets) == 0 {
		return nil, nil
	}
	return func(ip string) bool {
		parsed := net.ParseIP(strings.TrimSpace(ip))
		if parsed == nil {
			return false
		}
		for _, n := range nets {
			if n.Contains(parsed) {
				return true
			}
		}
		return false
	}, nil
}

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func firstCSV(v string) string {
	if v == "" {
		return ""
	}
	if idx := strings.IndexByte(v, ','); idx >= 0 {
		v = v[:idx]
	}
	return strings.TrimSpace(v)
}

func remoteIP(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}
