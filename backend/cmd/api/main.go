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
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/saniuzzaman-robin/changeloom/backend/internal/auth"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/config"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/httpapi"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/logging"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/push"
	"github.com/saniuzzaman-robin/changeloom/backend/internal/telemetry"
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

	shutdownTracing := func(context.Context) error { return nil }
	if cfg.OTelEnabled {
		if shutdownTracing, err = telemetry.Setup(ctx, cfg.FirebaseProjectID, cfg.ServiceName, cfg.OTelSampleRatio); err != nil {
			return fmt.Errorf("tracing: %w", err)
		}
		slog.Info("tracing enabled", "sample_ratio", cfg.OTelSampleRatio)
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	poolCfg.MaxConns = cfg.DBMaxConns
	if cfg.OTelEnabled {
		poolCfg.ConnConfig.Tracer = telemetry.NewPgxTracer()
	}
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
		TimelineScore:          httpapi.TimelineScore(cfg.TimelineScore),
		TimelineMix:            httpapi.TimelineMix(cfg.TimelineMix),
		TopicRequestMaxPending: cfg.TopicRequestMaxPending,
		NotifySecret:           cfg.NotifySecret,
		RequestTimeout:         cfg.RequestTimeout,
		RateLimitPerIP:         cfg.RateLimitIPPerMin,
		RateLimitPerUser:       cfg.RateLimitUserPerMin,
	}
	if err := addAppCheck(ctx, cfg, &opts); err != nil {
		return err
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

	handler := httpapi.NewHandler(httpapi.NewServer(pool, opts), verifier)
	if cfg.OTelEnabled {
		// otelhttp names the span after the method; the handler renames it to the matched route.
		handler = otelhttp.NewHandler(handler, "api")
	}
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
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

	var serveErr error
	select {
	case serveErr = <-errCh:
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
	case <-ctx.Done():
		slog.Info("shutting down api")
	}
	// The server shutdown and the span flush share one budget, under Cloud Run's 10s.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if ctx.Err() != nil {
		serveErr = srv.Shutdown(shutdownCtx)
	}
	return errors.Join(serveErr, flushTraces(shutdownCtx, shutdownTracing))
}

func flushTraces(ctx context.Context, shutdown func(context.Context) error) error {
	if err := shutdown(ctx); err != nil {
		return fmt.Errorf("flush traces: %w", err)
	}
	return nil
}

// addAppCheck checks App Check tokens outside dev. Its public keys are fetched at startup; when
// that fails the api still starts, unchecked, unless APPCHECK_ENFORCE is on.
func addAppCheck(ctx context.Context, cfg config.Config, opts *httpapi.Options) error {
	if cfg.Env == config.EnvDev {
		return nil
	}
	appCheck, err := auth.NewFirebaseAppCheck(ctx, cfg.FirebaseProjectID)
	if err != nil {
		if cfg.AppCheckEnforce {
			return fmt.Errorf("app check: %w", err)
		}
		slog.Warn("app check disabled: couldn't fetch its public keys", "err", err)
		return nil
	}
	opts.AppCheck = appCheck
	opts.AppCheckEnforce = cfg.AppCheckEnforce
	slog.Info("app check enabled", "enforce", cfg.AppCheckEnforce)
	return nil
}

// newVerifier picks the token verifier: the dev stub in dev, Firebase in staging and prod.
func newVerifier(ctx context.Context, cfg config.Config) (auth.Verifier, error) {
	if cfg.Env == config.EnvDev {
		slog.Warn("dev auth enabled: any 'Bearer dev:<name>' token is accepted")
		return auth.DevVerifier{}, nil
	}
	return auth.NewFirebaseVerifier(ctx, cfg.FirebaseProjectID)
}
