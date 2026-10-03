package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/saniuzzaman-robin/changeloom/curator/internal/config"
)

const (
	notifyTimeout = 30 * time.Second
	// maxNotifyCalls bounds the batches one Notify asks for; the api pushes up to 10 stories per call.
	maxNotifyCalls = 50
)

// Notify asks the hosted api to send push notifications for new stories, one batch per call, and
// returns how many it sent. It repeats while the api reports stories remaining and the last batch
// made progress. The caller logs a failure but does not undo the sync: notification is driven by
// the hosted notified_at, so the next call picks up anything missed.
func Notify(ctx context.Context, client *http.Client, r config.Remote) (int, error) {
	if r.APIBaseURL == "" || r.NotifySecret == "" {
		return 0, fmt.Errorf("API_BASE_URL_<ENV> and NOTIFY_SECRET_<ENV> must be set to notify (see curator/.env.example)")
	}
	total := 0
	for range maxNotifyCalls {
		sent, remaining, err := notifyBatch(ctx, client, r)
		total += sent
		if err != nil {
			return total, err
		}
		if remaining == 0 || sent == 0 {
			return total, nil
		}
	}
	return total, fmt.Errorf("stories still pending after %d notify calls; the next sync continues", maxNotifyCalls)
}

func notifyBatch(ctx context.Context, client *http.Client, r config.Remote) (sent, remaining int, err error) {
	ctx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.APIBaseURL+"/internal/notify", nil)
	if err != nil {
		return 0, 0, fmt.Errorf("build notify request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.NotifySecret)
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("call %s/internal/notify: %w", r.APIBaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("%s/internal/notify returned %s (404 means NOTIFY_SECRET is unset on the api; 401 means the secrets differ)", r.APIBaseURL, resp.Status)
	}
	var body struct {
		Sent      int `json:"sent"`
		Remaining int `json:"remaining"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, 0, fmt.Errorf("decode notify response: %w", err)
	}
	return body.Sent, body.Remaining, nil
}
