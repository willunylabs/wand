package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testIdentity struct {
	id string
}

func (i testIdentity) ID() string { return i.id }

func TestAuthenticatorFunc_Authenticate(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	want := testIdentity{id: "u-1"}

	a := AuthenticatorFunc(func(r *http.Request) (Identity, error) {
		_ = r
		return want, nil
	})

	identity, err := a.Authenticate(req)
	if err != nil {
		t.Fatalf("authenticate returned unexpected error: %v", err)
	}
	if identity == nil || identity.ID() != want.ID() {
		t.Fatalf("expected identity %q, got %#v", want.ID(), identity)
	}
}

func TestAuthenticatorFunc_AuthenticateError(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	wantErr := errors.New("boom")

	a := AuthenticatorFunc(func(r *http.Request) (Identity, error) {
		_ = r
		return nil, wantErr
	})

	identity, err := a.Authenticate(req)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error %v, got %v", wantErr, err)
	}
	if identity != nil {
		t.Fatalf("expected nil identity on error, got %#v", identity)
	}
}
