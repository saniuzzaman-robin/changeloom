// Package auth verifies bearer tokens and carries the signed-in user through request contexts.
package auth

import (
	"context"
	"errors"
	"strings"
)

// ErrInvalidToken is returned by a Verifier when a token is missing, malformed or rejected.
var ErrInvalidToken = errors.New("invalid token")

// Identity is the verified identity behind a token.
type Identity struct {
	UID   string
	Email *string
}

// Verifier turns a bearer token into an Identity.
type Verifier interface {
	Verify(ctx context.Context, token string) (Identity, error)
}

// DevTokenPrefix marks tokens accepted by DevVerifier, e.g. "dev:alice".
const DevTokenPrefix = "dev:"

// DevVerifier accepts "dev:<name>" tokens without any cryptographic check.
// It must only be wired up when ENV=dev.
type DevVerifier struct{}

// Verify maps "dev:<name>" to UID "dev:<name>".
func (DevVerifier) Verify(_ context.Context, token string) (Identity, error) {
	name, ok := strings.CutPrefix(token, DevTokenPrefix)
	if !ok || strings.TrimSpace(name) == "" {
		return Identity{}, ErrInvalidToken
	}
	return Identity{UID: token}, nil
}

// User is the signed-in user attached to a request context.
type User struct {
	ID    int64
	Email *string
}

type userKey struct{}

// WithUser returns a copy of ctx carrying u.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

// UserFrom returns the user stored in ctx by WithUser.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey{}).(User)
	return u, ok
}
