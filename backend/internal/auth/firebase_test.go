package auth

import (
	"context"
	"errors"
	"testing"

	fbauth "firebase.google.com/go/v4/auth"
)

type fakeClient struct {
	tok *fbauth.Token
	err error
}

func (f fakeClient) VerifyIDToken(context.Context, string) (*fbauth.Token, error) {
	return f.tok, f.err
}

func TestFirebaseVerifier(t *testing.T) {
	ctx := context.Background()

	v := &FirebaseVerifier{client: fakeClient{tok: &fbauth.Token{UID: "u1", Claims: map[string]any{"email": "a@b.c"}}}}
	id, err := v.Verify(ctx, "tok")
	if err != nil || id.UID != "u1" || id.Email == nil || *id.Email != "a@b.c" {
		t.Fatalf("got %+v, %v", id, err)
	}

	v = &FirebaseVerifier{client: fakeClient{tok: &fbauth.Token{UID: "u2"}}}
	if id, err = v.Verify(ctx, "tok"); err != nil || id.Email != nil {
		t.Fatalf("no-email token: got %+v, %v", id, err)
	}

	v = &FirebaseVerifier{client: fakeClient{err: errors.New("expired")}}
	if _, err = v.Verify(ctx, "tok"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken, got %v", err)
	}
}

func TestNewFirebaseVerifierRequiresProject(t *testing.T) {
	if _, err := NewFirebaseVerifier(context.Background(), ""); err == nil {
		t.Fatal("want error for empty project id")
	}
}
