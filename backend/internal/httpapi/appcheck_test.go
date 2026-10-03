package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/auth"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/httpapi"
)

const validAppCheck = "valid-app-check"

type fakeAppCheck struct{}

func (fakeAppCheck) VerifyAppCheck(token string) error {
	if token != validAppCheck {
		return auth.ErrInvalidToken
	}
	return nil
}

// getWithAppCheck sends GET path as alice with appCheck in the App Check header (none when empty)
// and returns the status and the error code, if any.
func (e *env) getWithAppCheck(path, appCheck string) (int, string) {
	e.t.Helper()
	req, err := http.NewRequestWithContext(e.t.Context(), http.MethodGet, e.srv.URL+path, nil)
	if err != nil {
		e.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	if appCheck != "" {
		req.Header.Set(auth.AppCheckHeader, appCheck)
	}
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body httpapi.Error
	if resp.StatusCode >= 400 {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			e.t.Fatalf("decode error body: %v", err)
		}
	}
	return resp.StatusCode, body.Code
}

func TestAppCheckEnforced(t *testing.T) {
	e := newEnvWith(t, func(o *httpapi.Options) {
		o.AppCheck = fakeAppCheck{}
		o.AppCheckEnforce = true
	})
	for _, tc := range []struct {
		name, path, token string
		want              int
	}{
		{"missing", "/v1/me", "", http.StatusForbidden},
		{"invalid", "/v1/me", "forged", http.StatusForbidden},
		{"valid", "/v1/me", validAppCheck, http.StatusOK},
		{"outside /v1/", "/health", "", http.StatusOK},
	} {
		code, errCode := e.getWithAppCheck(tc.path, tc.token)
		if code != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, code, tc.want)
		}
		// Never 401: the app answers a 401 by refreshing its ID token and then signing out.
		if code == http.StatusForbidden && errCode != "app_check_failed" {
			t.Errorf("%s: error code %q, want app_check_failed", tc.name, errCode)
		}
	}
}

func TestAppCheckNotEnforcedOnlyLogs(t *testing.T) {
	e := newEnvWith(t, func(o *httpapi.Options) { o.AppCheck = fakeAppCheck{} })
	for _, token := range []string{"", "forged", validAppCheck} {
		if code, _ := e.getWithAppCheck("/v1/me", token); code != http.StatusOK {
			t.Errorf("token %q: status %d, want 200", token, code)
		}
	}
}
