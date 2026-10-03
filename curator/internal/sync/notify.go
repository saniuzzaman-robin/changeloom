package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
)

const notifyTimeout = 30 * time.Second

// Notify asks the hosted api to send push notifications for new stories and returns how many it
// sent. The caller logs a failure but does not undo the sync: notification is driven by the
// hosted notified_at, so the next call picks up anything missed.
func Notify(ctx context.Context, client *http.Client, r config.Remote) (int, error) {
	if r.APIBaseURL == "" || r.NotifySecret == "" {
		return 0, fmt.Errorf("API_BASE_URL_<ENV> and NOTIFY_SECRET_<ENV> must be set to notify (see curator/.env.example)")
	}
	ctx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.APIBaseURL+"/internal/notify", nil)
	if err != nil {
		return 0, fmt.Errorf("build notify request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.NotifySecret)
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("call %s/internal/notify: %w", r.APIBaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%s/internal/notify returned %s (404 means NOTIFY_SECRET is unset on the api; 401 means the secrets differ)", r.APIBaseURL, resp.Status)
	}
	var body struct {
		Sent int `json:"sent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("decode notify response: %w", err)
	}
	return body.Sent, nil
}
