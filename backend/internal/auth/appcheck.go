package auth

import (
	"context"
	"errors"
	"fmt"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/appcheck"
	"google.golang.org/api/option"
)

// AppCheckHeader carries the Firebase App Check token on /v1/ requests.
const AppCheckHeader = "X-Firebase-AppCheck"

// AppCheckVerifier checks Firebase App Check tokens, which attest that a request comes from the
// genuine app on a genuine device.
type AppCheckVerifier interface {
	VerifyAppCheck(token string) error
}

// appCheckClient is the part of the Firebase App Check client FirebaseAppCheck uses.
type appCheckClient interface {
	VerifyToken(token string) (*appcheck.DecodedAppCheckToken, error)
}

// FirebaseAppCheck verifies App Check tokens for one project.
type FirebaseAppCheck struct {
	client appCheckClient
}

// NewFirebaseAppCheck returns a verifier for projectID. It fetches App Check's public keys now and
// refreshes them in the background until ctx ends; like ID tokens, it needs no credentials.
func NewFirebaseAppCheck(ctx context.Context, projectID string) (*FirebaseAppCheck, error) {
	if projectID == "" {
		return nil, errors.New("firebase project id is empty")
	}
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID}, option.WithoutAuthentication())
	if err != nil {
		return nil, fmt.Errorf("init firebase app: %w", err)
	}
	client, err := app.AppCheck(ctx)
	if err != nil {
		return nil, fmt.Errorf("init firebase app check client: %w", err)
	}
	return &FirebaseAppCheck{client: client}, nil
}

// VerifyAppCheck reports any rejection as ErrInvalidToken.
func (v *FirebaseAppCheck) VerifyAppCheck(token string) error {
	if _, err := v.client.VerifyToken(token); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	return nil
}
