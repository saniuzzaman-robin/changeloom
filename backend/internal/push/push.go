// Package push sends FCM notifications for high-severity security stories.
package push

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/db"
)

const (
	// maxTokensPerSend is the FCM multicast limit.
	maxTokensPerSend = 500
	// storyWindow is how long after creation a story can still be pushed.
	storyWindow  = 24 * time.Hour
	maxBodyRunes = 160
	// maxStoriesPerRun bounds one Run so it fits in a request; the caller repeats until none remain.
	maxStoriesPerRun = 10
)

// Message is one notification for a set of device tokens.
type Message struct {
	Tokens  []string
	Title   string
	Body    string
	StoryID int64
}

// Sender delivers a Message. It returns the tokens FCM reported as no longer registered.
type Sender interface {
	Send(ctx context.Context, msg Message) (unregistered []string, err error)
}

// FCMSender implements Sender with Firebase Cloud Messaging. It authenticates with
// Application Default Credentials (e.g. GOOGLE_APPLICATION_CREDENTIALS).
type FCMSender struct {
	client *messaging.Client
}

// NewFCMSender creates a sender for the Firebase project.
func NewFCMSender(ctx context.Context, projectID string) (*FCMSender, error) {
	if projectID == "" {
		return nil, errors.New("firebase project id is empty")
	}
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("init firebase app: %w", err)
	}
	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("init firebase messaging client (needs Application Default Credentials): %w", err)
	}
	return &FCMSender{client: client}, nil
}

// Send implements Sender.
func (s *FCMSender) Send(ctx context.Context, msg Message) ([]string, error) {
	if len(msg.Tokens) == 0 || len(msg.Tokens) > maxTokensPerSend {
		return nil, fmt.Errorf("send needs 1-%d tokens, got %d", maxTokensPerSend, len(msg.Tokens))
	}
	resp, err := s.client.SendEachForMulticast(ctx, &messaging.MulticastMessage{
		Tokens:       msg.Tokens, //nolint:staticcheck // registration tokens, not FIDs
		Notification: &messaging.Notification{Title: msg.Title, Body: msg.Body},
		Data:         map[string]string{"story_id": strconv.FormatInt(msg.StoryID, 10)},
		Android:      &messaging.AndroidConfig{Priority: "high"},
	})
	if err != nil {
		return nil, fmt.Errorf("send multicast: %w", err)
	}
	var dead []string
	var firstErr error
	for i, r := range resp.Responses {
		switch {
		case r.Success:
		case messaging.IsUnregistered(r.Error):
			dead = append(dead, msg.Tokens[i])
		case firstErr == nil:
			firstErr = r.Error
		}
	}
	if firstErr != nil {
		slog.WarnContext(ctx, "some push messages failed", "story_id", msg.StoryID, "failed", resp.FailureCount-len(dead), "first_err", firstErr)
	}
	return dead, nil
}

// Notifier pushes new high-severity security stories to the devices of users who follow
// one of the story's topics.
type Notifier struct {
	pool   *pgxpool.Pool
	sender Sender
	now    func() time.Time
}

// NewNotifier creates a Notifier.
func NewNotifier(pool *pgxpool.Pool, sender Sender) *Notifier {
	return &Notifier{pool: pool, sender: sender, now: time.Now}
}

// Run pushes up to maxStoriesPerRun pending stories and returns how many it pushed and how many
// are still pending. Each story is claimed with a row lock, so concurrent runs never push the same
// one, and is marked notified as soon as its sends succeed. A story whose send fails is left
// pending, skipped for the rest of this run and retried on the next (until it is a day old).
func (n *Notifier) Run(ctx context.Context) (pushed, remaining int, err error) {
	since := n.now().Add(-storyWindow)
	var errs []error
	skip := []int64{}
	for pushed+len(skip) < maxStoriesPerRun {
		id, claimed, err := n.pushNext(ctx, since, skip)
		if err != nil {
			if id == 0 {
				return pushed, 0, errors.Join(append(errs, err)...)
			}
			errs = append(errs, fmt.Errorf("story %d: %w", id, err))
			skip = append(skip, id)
			continue
		}
		if !claimed {
			break
		}
		pushed++
	}
	left, err := db.New(n.pool).CountStoriesToNotify(ctx, since)
	if err != nil {
		errs = append(errs, fmt.Errorf("count stories to notify: %w", err))
	}
	return pushed, int(left), errors.Join(errs...)
}

// pushNext claims one pending story outside skip and pushes it in one transaction. It returns
// claimed=false when no story is left to claim, and the story's id with any error after a claim.
func (n *Notifier) pushNext(ctx context.Context, since time.Time, skip []int64) (id int64, claimed bool, err error) {
	err = pgx.BeginFunc(ctx, n.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		st, err := q.ClaimStoryToNotify(ctx, db.ClaimStoryToNotifyParams{Since: since, SkipIds: skip})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("claim story to notify: %w", err)
		}
		id, claimed = st.ID, true
		return n.notify(ctx, q, st)
	})
	return id, claimed, err
}

func (n *Notifier) notify(ctx context.Context, q *db.Queries, st db.ClaimStoryToNotifyRow) error {
	tokens, err := q.ListDeviceTokensForStory(ctx, st.ID)
	if err != nil {
		return fmt.Errorf("list device tokens: %w", err)
	}
	for start := 0; start < len(tokens); start += maxTokensPerSend {
		chunk := tokens[start:min(start+maxTokensPerSend, len(tokens))]
		dead, err := n.sender.Send(ctx, Message{Tokens: chunk, Title: st.Title, Body: truncate(st.Summary, maxBodyRunes), StoryID: st.ID})
		if err != nil {
			return err
		}
		if len(dead) > 0 {
			if err := q.DeleteDeviceTokens(ctx, dead); err != nil {
				return fmt.Errorf("delete unregistered tokens: %w", err)
			}
		}
	}
	if err := q.MarkStoryNotified(ctx, st.ID); err != nil {
		return fmt.Errorf("mark story notified: %w", err)
	}
	slog.InfoContext(ctx, "story pushed", "story_id", st.ID, "devices", len(tokens))
	return nil
}

func truncate(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes-1]) + "…"
}
