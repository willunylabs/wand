package auth

import "net/http"

// Identity represents an authenticated principal.
type Identity interface {
	ID() string
}

// Authenticator verifies a request and returns an Identity.
type Authenticator interface {
	Authenticate(*http.Request) (Identity, error)
}

// AuthenticatorFunc adapts a function to the Authenticator interface.
type AuthenticatorFunc func(*http.Request) (Identity, error)

func (f AuthenticatorFunc) Authenticate(r *http.Request) (Identity, error) {
	return f(r)
}
