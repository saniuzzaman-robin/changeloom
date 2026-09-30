package auth

import (
	"context"
	"errors"
	"fmt"

	firebase "firebase.google.com/go/v4"
	fbauth "firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

// tokenVerifier is the part of the Firebase auth client FirebaseVerifier uses.
type tokenVerifier interface {
	VerifyIDToken(ctx context.Context, idToken string) (*fbauth.Token, error)
}

// FirebaseVerifier verifies Firebase ID tokens for one project.
type FirebaseVerifier struct {
	client tokenVerifier
}

// NewFirebaseVerifier returns a verifier for projectID. It needs no service-account
// credentials: ID tokens are checked against Google's public signing keys.
func NewFirebaseVerifier(ctx context.Context, projectID string) (*FirebaseVerifier, error) {
	if projectID == "" {
		return nil, errors.New("firebase project id is empty")
	}
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID}, option.WithoutAuthentication())
	if err != nil {
		return nil, fmt.Errorf("init firebase app: %w", err)
	}
	client, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("init firebase auth client: %w", err)
	}
	return &FirebaseVerifier{client: client}, nil
}

// Verify checks token and returns the Firebase UID and (if present) email.
// Any rejection is reported as ErrInvalidToken.
func (v *FirebaseVerifier) Verify(ctx context.Context, token string) (Identity, error) {
	t, err := v.client.VerifyIDToken(ctx, token)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	id := Identity{UID: t.UID}
	if email, ok := t.Claims["email"].(string); ok && email != "" {
		id.Email = &email
	}
	return id, nil
}
