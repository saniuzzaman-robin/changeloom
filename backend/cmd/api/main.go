// Command api serves the Changeloom REST API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/auth"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/config"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/httpapi"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/logging"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/push"
)

const (
	readHeaderTimeout = 5 * time.Second
	// readTimeout covers the whole request body, which handlers cap at 1 MiB.
	readTimeout = 15 * time.Second
	// writeSlack is how much longer than REQUEST_TIMEOUT a handler gets to write its response.
	writeSlack  = 5 * time.Second
	idleTimeout = 120 * time.Second
	// shutdownTimeout stays under Cloud Run's 10s between SIGTERM and SIGKILL.
	shutdownTimeout  = 8 * time.Second
	startupDBTimeout = 10 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("api exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// Hosted environments run in the GCP project that is also the Firebase project (deploy/README.md),
	// which is where Cloud Run's traces live.
	traceProject := ""
	if cfg.Env != config.EnvDev {
		traceProject = cfg.FirebaseProjectID
	}
	slog.SetDefault(slog.New(logging.NewHandler(os.Stdout, cfg.LogLevel, traceProject)))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	verifier, err := newVerifier(ctx, cfg)
	if err != nil {
		return err
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	poolCfg.MaxConns = cfg.DBMaxConns
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("configure database pool: %w", err)
	}
	defer pool.Close()

	startupCtx, cancel := context.WithTimeout(ctx, startupDBTimeout)
	defer cancel()
	if err := pool.Ping(startupCtx); err != nil {
		return fmt.Errorf("connect to database (is it running? try `make db-up`): %w", err)
	}

	opts := httpapi.Options{
		TimelineWindow:         cfg.TimelineWindow,
		TopicRequestMaxPending: cfg.TopicRequestMaxPending,
		NotifySecret:           cfg.NotifySecret,
		RequestTimeout:         cfg.RequestTimeout,
	}
	if cfg.PushEnabled {
		sender, err := push.NewFCMSender(ctx, cfg.FirebaseProjectID)
		if err != nil {
			return fmt.Errorf("push notifications: %w", err)
		}
		opts.Notifier = push.NewNotifier(pool, sender)
	}
	if cfg.NotifySecret == "" {
		slog.Warn("NOTIFY_SECRET is empty: POST /internal/notify is disabled")
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewHandler(httpapi.NewServer(pool, opts), verifier),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      cfg.RequestTimeout + writeSlack,
		IdleTimeout:       idleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		slog.Info("shutting down api")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// newVerifier picks the token verifier: the dev stub in dev, Firebase in staging and prod.
func newVerifier(ctx context.Context, cfg config.Config) (auth.Verifier, error) {
	if cfg.Env == config.EnvDev {
		slog.Warn("dev auth enabled: any 'Bearer dev:<name>' token is accepted")
		return auth.DevVerifier{}, nil
	}
	return auth.NewFirebaseVerifier(ctx, cfg.FirebaseProjectID)
}
