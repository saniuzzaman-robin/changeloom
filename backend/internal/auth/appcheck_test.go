package auth

import (
	"context"
	"errors"
	"testing"

	"firebase.google.com/go/v4/appcheck"
)

type fakeAppCheckClient struct{ err error }

func (f fakeAppCheckClient) VerifyToken(string) (*appcheck.DecodedAppCheckToken, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &appcheck.DecodedAppCheckToken{AppID: "app"}, nil
}

func TestFirebaseAppCheck(t *testing.T) {
	if err := (&FirebaseAppCheck{client: fakeAppCheckClient{}}).VerifyAppCheck("tok"); err != nil {
		t.Fatalf("valid token: %v", err)
	}
	err := (&FirebaseAppCheck{client: fakeAppCheckClient{err: errors.New("expired")}}).VerifyAppCheck("tok")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken, got %v", err)
	}
}

func TestNewFirebaseAppCheckRequiresProject(t *testing.T) {
	if _, err := NewFirebaseAppCheck(context.Background(), ""); err == nil {
		t.Fatal("want an error for an empty project id")
	}
}
