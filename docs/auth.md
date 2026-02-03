# Auth Interfaces

Wand ships only the minimal interfaces to avoid framework coupling.

```go
type Identity interface {
	ID() string
}

type Authenticator interface {
	Authenticate(*http.Request) (Identity, error)
}
```

## JWT Example (External)

```go
// Pseudocode only. Use github.com/golang-jwt/jwt/v5 (or similar).
authenticator := auth.AuthenticatorFunc(func(r *http.Request) (auth.Identity, error) {
	token := r.Header.Get("Authorization")
	// parse/verify token...
	return userIdentity, nil
})
```

## Session Example (External)

```go
// Pseudocode only. Use your session store of choice.
authenticator := auth.AuthenticatorFunc(func(r *http.Request) (auth.Identity, error) {
	sessionID := readSessionCookie(r)
	// load session, return identity...
	return userIdentity, nil
})
```
