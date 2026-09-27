// Command server — AI Reply бэкенді: REST API, AI шлюзі, әкімші панелі, лендинг.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/admin"
	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/limits"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/logging"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/payments"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/simulator"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
	"github.com/aireply/ai-reply-back-end/internal/transport/adminapi"
	"github.com/aireply/ai-reply-back-end/internal/transport/api"
	"github.com/aireply/ai-reply-back-end/internal/transport/simulatorapi"
	"github.com/aireply/ai-reply-back-end/internal/transport/web"
	"github.com/aireply/ai-reply-back-end/internal/users"
	"github.com/aireply/ai-reply-back-end/migrations"
)

func main() {
	envFile := flag.String("env", ".env", "path to the env file")
	flag.Parse()

	if err := run(*envFile); err != nil {
		fmt.Fprintf(os.Stderr, "\nstartup failed:\n%v\n\nSee .env.example for the required configuration.\n", err)
		os.Exit(1)
	}
}

func run(envFile string) error {
	cfg, err := config.Load(envFile)
	if err != nil {
		return err
	}
	log := logging.New(cfg.Log.Level, cfg.Log.Format)

	db, err := database.Open(database.Options{
		Path:         cfg.Database.Path,
		BusyTimeout:  cfg.Database.BusyTimeout,
		MaxReadConns: cfg.Database.MaxReadConns,
	})
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Database.MigrateOnStart {
		applied, err := database.Migrate(ctx, db, migrations.FS)
		if err != nil {
			return err
		}
		if len(applied) > 0 {
			log.Info("migrations applied", "count", len(applied), "names", applied)
		}
	}

	// ---------------------------------------------------------------- wiring
	store := repository.New(db)
	planSvc := plans.New(store)
	subSvc := subscriptions.New(store, planSvc, cfg.App.Location())
	userSvc := users.New(store)
	sender := auth.NewSender(cfg.Auth.OTPChannel, log)
	authSvc := auth.New(store, cfg.Auth, sender, subSvc, log)
	provider := ai.NewOpenAI(cfg.OpenAI)
	limitSvc := limits.New(store, limits.Limits{
		SourceChars:      cfg.Limits.SourceTextChars,
		InstructionChars: cfg.Limits.InstructionChars,
		MaxOutputTokens:  cfg.OpenAI.MaxOutputTokens,
	})
	aiSvc := ai.New(store, subSvc, provider, limitSvc, log)
	paymentSvc := payments.New(store, subSvc, payments.DemoProvider{}, cfg.Payments.Mode)
	notifySvc := notifications.New(store)
	adminSvc := admin.New(store, subSvc, planSvc, cfg, log)
	simulatorSvc := simulator.New(simulator.Deps{
		Repo: store, Users: userSvc, Subs: subSvc, Plans: planSvc, AI: aiSvc, Limits: limitSvc,
		Config: cfg, Log: log,
	})

	if err := adminSvc.Bootstrap(ctx); err != nil {
		return fmt.Errorf("admin bootstrap: %w", err)
	}

	bundle, err := localization.Load()
	if err != nil {
		return err
	}
	limiter := middleware.NewLimiter()

	mux := http.NewServeMux()
	api.New(api.Deps{
		Config: cfg, Auth: authSvc, Users: userSvc, Plans: planSvc, Subs: subSvc,
		AI: aiSvc, Limits: limitSvc, Payments: paymentSvc, Limiter: limiter, Log: log,
		Ping: func(ctx context.Context) error { return db.Reader().PingContext(ctx) },
	}).Register(mux)

	adminapi.New(adminapi.Deps{
		Config: cfg, Admin: adminSvc, Limits: limitSvc, Notifications: notifySvc, Log: log,
	}).Register(mux)

	simulatorapi.New(simulatorapi.Deps{
		Config: cfg, Admin: adminSvc, Simulator: simulatorSvc, Limiter: limiter, Log: log,
	}).Register(mux)

	webServer, err := web.New(web.Deps{
		Config: cfg, Admin: adminSvc, Plans: planSvc, Notifications: notifySvc,
		Bundle: bundle, Limiter: limiter, Log: log,
	})
	if err != nil {
		return err
	}
	webServer.Register(mux)

	handler := middleware.Chain(mux,
		middleware.RequestID,
		middleware.Recover(log),
		middleware.Logging(log),
		middleware.SecurityHeaders(cfg.App.IsProduction()),
		middleware.CORS(cfg.App.CORSOrigins),
	)

	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.App.Host, cfg.App.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go expireSubscriptions(ctx, subSvc, log)

	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening",
			"addr", server.Addr, "env", cfg.App.Env, "timezone", cfg.App.Timezone,
			"demo_mode", cfg.Auth.DemoMode, "payment_mode", cfg.Payments.Mode,
			"legacy_api", cfg.Auth.LegacyEnabled, "model", cfg.OpenAI.Model)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

// expireSubscriptions — мерзімі өткен жазылымдарды белгілейтін фон тапсырмасы.
func expireSubscriptions(ctx context.Context, subs *subscriptions.Service, log *slog.Logger) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		if count, err := subs.ExpireDue(ctx); err != nil {
			log.Error("subscription expiry sweep failed", "error", err.Error())
		} else if count > 0 {
			log.Info("subscriptions expired", "count", count)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
