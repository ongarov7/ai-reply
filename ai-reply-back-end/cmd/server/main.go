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
	"sync"
	"syscall"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/admin"
	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/auth/appleid"
	"github.com/aireply/ai-reply-back-end/internal/auth/idtoken"
	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/email"
	"github.com/aireply/ai-reply-back-end/internal/installations"
	"github.com/aireply/ai-reply-back-end/internal/limits"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/logging"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/payments"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/productevents"
	"github.com/aireply/ai-reply-back-end/internal/push"
	"github.com/aireply/ai-reply-back-end/internal/reports"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/retention"
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
	check := flag.Bool("check", false, "validate the configuration, print its warnings and exit (opens nothing)")
	flag.Parse()

	if *check {
		os.Exit(checkConfig(*envFile))
	}
	if err := run(*envFile); err != nil {
		fmt.Fprintf(os.Stderr, "\nstartup failed:\n%v\n\nSee .env.example for the required configuration.\n", err)
		os.Exit(1)
	}
}

// checkConfig — тек баптауды тексереді: дерекқор да, порт та ашылмайды (жаңартудан бұрын).
//
// Every problem that would stop the start is listed at once, then every
// warning, so an existing .env can be checked against a new build before the
// running container is replaced.
func checkConfig(envFile string) int {
	cfg, err := config.Load(envFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration check failed:\n%v\n", err)
		return 1
	}
	for _, warning := range cfg.Warnings() {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	fmt.Printf("configuration OK (APP_ENV=%s)\nsign-in client ids: google=%v apple=%v\n",
		cfg.App.Env, cfg.OAuth.GoogleClientIDs, cfg.OAuth.AppleClientIDs)
	return 0
}

func run(envFile string) error {
	cfg, err := config.Load(envFile)
	if err != nil {
		return err
	}
	log := logging.New(cfg.Log.Level, cfg.Log.Format)
	// Іске қосылуды тоқтатпайтын олқылықтар: оператор аты, Apple токенін кері қайтару, APPLE_CLIENT_ID пішіні.
	for _, warning := range cfg.Warnings() {
		log.Warn("configuration warning: " + warning)
	}

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

	// Background workers use the database: they are stopped through ctx and
	// awaited before the deferred db.Close runs.
	var workers sync.WaitGroup
	defer func() {
		stop()
		workers.Wait()
	}()

	if cfg.Database.MigrateOnStart {
		applied, err := database.Migrate(ctx, db, migrations.FS)
		if err != nil {
			return err
		}
		if len(applied) > 0 {
			log.Info("migrations applied", "count", len(applied), "names", applied)
		}
	}

	bundle, err := localization.Load()
	if err != nil {
		return err
	}

	// ---------------------------------------------------------------- wiring
	store := repository.New(db)
	planSvc := plans.New(store)
	subSvc := subscriptions.New(store, planSvc, cfg.App.Location())
	userSvc := users.New(store).WithLogger(log)
	if err := configureAppleRevocation(userSvc, cfg.OAuth); err != nil {
		return err
	}
	sender := auth.NewSender(cfg.Auth.OTPChannel, log)
	authSvc := auth.New(store, cfg.Auth, sender, subSvc, log)
	// Account-deletion codes are issued in the background; let them finish
	// before the database closes.
	defer authSvc.Wait()
	mailer, err := newMailer(cfg, bundle)
	if err != nil {
		return err
	}
	if err := configureSignIn(authSvc, cfg, mailer); err != nil {
		return err
	}
	// Client id-лер — ашық мәндер: токеннің aud-ы сәйкес келмесе, журналда екеуі де көрінеді.
	log.Info("sign-in client ids", "google", cfg.OAuth.GoogleClientIDs, "apple", cfg.OAuth.AppleClientIDs,
		"apple_token_revocation", cfg.OAuth.AppleRevocation())
	installSvc := installations.New(store, cfg.Auth.AccessSecret, log)
	pushProvider, err := configurePush(cfg.Push, log)
	if err != nil {
		return err
	}
	notifySvc := notifications.New(notifications.Deps{
		Repo: store, Installations: installSvc, Provider: pushProvider, Plans: planSvc,
		Config: cfg.Push, Location: cfg.App.Location(), Translate: bundle.T, Log: log,
	})
	if mailer != nil {
		notifySvc.WithEmail(mailer, email.NotificationTemplates{Translate: bundle.T, Brand: cfg.Email.FromName})
	}
	// Automatic notifications: plan activated (payment, admin), plan expiring
	// or expired (the expiry sweep below), quota low or exhausted (AI gateway).
	notifyEvents := notifications.NewEvents(notifySvc).
		WithDemoPayments(cfg.Payments.DemoCheckout && !cfg.App.IsProduction())
	provider := ai.NewOpenAI(cfg.OpenAI)
	limitSvc := limits.New(store, limits.Limits{
		SourceChars:      cfg.Limits.SourceTextChars,
		InstructionChars: cfg.Limits.InstructionChars,
		MaxOutputTokens:  cfg.OpenAI.MaxOutputTokens,
	})
	aiSvc := ai.New(store, subSvc, provider, limitSvc, log).WithRepair(cfg.AI.RepairEnabled).WithQuotaEvents(notifyEvents).
		WithPolishDailyLimit(cfg.Limits.PolishPerDay)
	// No live provider exists yet (StoreKit / Play Billing come later), so
	// nothing can be bought unless a development server opts into demo checkout.
	paymentSvc := payments.New(store, subSvc, payments.DemoProvider{}, cfg.Payments.Mode).
		WithEvents(notifyEvents).WithDemoCheckout(cfg.Payments.DemoCheckout).WithProduction(cfg.App.IsProduction())
	eventSvc := productevents.New(store, log)
	reportSvc := reports.New(store, log)
	adminSvc := admin.New(store, subSvc, planSvc, cfg, log).WithEvents(notifyEvents)
	simulatorSvc := simulator.New(simulator.Deps{
		Repo: store, Users: userSvc, Subs: subSvc, Plans: planSvc, AI: aiSvc, Limits: limitSvc,
		Config: cfg, Log: log,
	})

	if err := adminSvc.Bootstrap(ctx); err != nil {
		return fmt.Errorf("admin bootstrap: %w", err)
	}

	limiter := middleware.NewLimiter()

	mux := http.NewServeMux()
	api.New(api.Deps{
		Config: cfg, Auth: authSvc, Users: userSvc, Plans: planSvc, Subs: subSvc,
		AI: aiSvc, Limits: limitSvc, Payments: paymentSvc, Events: eventSvc,
		Installations: installSvc, Notifications: notifySvc, Reports: reportSvc, Limiter: limiter, Log: log,
		Ping: func(ctx context.Context) error { return db.Reader().PingContext(ctx) },
	}).Register(mux)

	adminapi.New(adminapi.Deps{
		Config: cfg, Admin: adminSvc, Limits: limitSvc, Notifications: notifySvc, Payments: paymentSvc,
		Reports: reportSvc, Limiter: limiter, Log: log,
	}).Register(mux)

	simulatorapi.New(simulatorapi.Deps{
		Config: cfg, Admin: adminSvc, Simulator: simulatorSvc, Limiter: limiter, Log: log,
	}).Register(mux)

	webServer, err := web.New(web.Deps{
		Config: cfg, Admin: adminSvc, Plans: planSvc, Payments: paymentSvc, Notifications: notifySvc,
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
		middleware.SecurityHeaders(cfg.App.HSTS()),
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

	for _, worker := range []func(context.Context){
		func(ctx context.Context) { expireSubscriptions(ctx, subSvc, notifyEvents, log) },
		notifySvc.Run,
		notifySvc.RunRetention,
		// Spent codes, dead sessions, abandoned installations, old metadata (RETENTION_*).
		retention.New(store, cfg.Retention, log).Run,
	} {
		workers.Add(1)
		go func(run func(context.Context)) {
			defer workers.Done()
			run(ctx)
		}(worker)
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening",
			"addr", server.Addr, "env", cfg.App.Env, "timezone", cfg.App.Timezone,
			"demo_mode", cfg.Auth.DemoMode, "payment_mode", cfg.Payments.Mode,
			"demo_checkout", cfg.Payments.DemoCheckout,
			"legacy_api", cfg.Auth.LegacyEnabled, "model", cfg.OpenAI.Model,
			"email_otp", authSvc.EmailDelivery(), "google_sign_in", authSvc.GoogleEnabled(),
			"apple_sign_in", authSvc.AppleEnabled(), "push", notifySvc.Ready(),
			"notification_email", notifySvc.Status().Email)
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

// newMailer — Resend жіберушісі (бапталмаса nil): кіру кодтары да, хабарлама хаттары да осы арқылы.
func newMailer(cfg config.Config, bundle *localization.Bundle) (*email.Resend, error) {
	if !cfg.Email.Enabled() {
		return nil, nil
	}
	mailer, err := email.NewResend(email.ResendConfig{
		APIKey:    cfg.Email.ResendAPIKey,
		FromEmail: cfg.Email.FromEmail,
		FromName:  cfg.Email.FromName,
		Translate: bundle.T,
		// Replies to a code or a plan notice reach a person, not noreply@.
		ReplyTo: cfg.App.ContactEmail(),
	})
	if err != nil {
		return nil, fmt.Errorf("email: %w", err)
	}
	return mailer, nil
}

// configureSignIn — пошта жеткізушісі (Resend) мен Google/Apple тексерушілерін қосады.
//
// Everything comes from the environment. A provider without configuration is
// simply off: its endpoint answers AUTH_PROVIDER_UNAVAILABLE and /api/v1/config
// says so, which the apps use to hide the button.
func configureSignIn(authSvc *auth.Service, cfg config.Config, mailer *email.Resend) error {
	if mailer != nil {
		authSvc.WithEmailSender(mailer)
	}

	var google, apple auth.IDTokenVerifier
	if len(cfg.OAuth.GoogleClientIDs) > 0 {
		verifier, err := idtoken.NewGoogle(cfg.OAuth.GoogleClientIDs,
			idtoken.NewRemoteKeys(idtoken.GoogleKeysURL, nil))
		if err != nil {
			return fmt.Errorf("google sign-in: %w", err)
		}
		google = verifier
	}
	if len(cfg.OAuth.AppleClientIDs) > 0 {
		verifier, err := idtoken.NewApple(cfg.OAuth.AppleClientIDs,
			idtoken.NewRemoteKeys(idtoken.AppleKeysURL, nil))
		if err != nil {
			return fmt.Errorf("apple sign-in: %w", err)
		}
		apple = verifier
	}
	authSvc.WithIdentityProviders(google, apple)
	return nil
}

// configureAppleRevocation — тіркелгі жойылғанда Sign in with Apple токенін кері қайтару.
//
// Needed whenever Sign in with Apple is on (App Store guideline 5.1.1(v));
// without APPLE_TEAM_ID, APPLE_KEY_ID and APPLE_PRIVATE_KEY the server still
// starts with a warning (config.Warnings), accounts are still deleted, and
// the Apple token stays until the person removes the app from their Apple ID
// settings, which is all the deletion page then promises. A key that is set
// but cannot be used stops the start; the key itself never reaches a log line
// or an error.
func configureAppleRevocation(userSvc *users.Service, cfg config.OAuth) error {
	if !cfg.AppleRevocation() {
		return nil
	}
	revoker, err := appleid.New(appleid.Config{
		TeamID: cfg.AppleTeamID, KeyID: cfg.AppleKeyID, PrivateKey: cfg.ApplePrivateKey, ClientIDs: cfg.AppleClientIDs,
	})
	if err != nil {
		return fmt.Errorf("apple token revocation: %w", err)
	}
	userSvc.WithAppleRevoker(revoker)
	return nil
}

// configurePush — Firebase Cloud Messaging (Android және iOS). Бапталмаса nil: сервер push-сыз жұмыс істейді.
//
// With PUSH_NOTIFICATIONS_ENABLED=true a key that cannot be used stops the
// start: the operator asked for push and would otherwise get silence. The
// key itself never reaches a log line or an error.
func configurePush(cfg config.Push, log *slog.Logger) (push.Provider, error) {
	if !cfg.FCM.Configured() {
		if cfg.Enabled {
			log.Warn("push notifications are enabled but Firebase is not configured; nothing will be sent " +
				"(set FIREBASE_SERVICE_ACCOUNT_FILE or FIREBASE_PROJECT_ID, FIREBASE_CLIENT_EMAIL and FIREBASE_PRIVATE_KEY)")
		}
		return nil, nil
	}
	provider, err := push.NewFCM(push.FCMConfig{
		ProjectID: cfg.FCM.ProjectID, ClientEmail: cfg.FCM.ClientEmail, PrivateKey: cfg.FCM.PrivateKey,
	})
	if err != nil {
		if cfg.Enabled {
			return nil, fmt.Errorf("push: %w", err)
		}
		log.Warn("firebase credentials cannot be used; push stays off", "error", err.Error())
		return nil, nil
	}
	return provider, nil
}

// expireSubscriptions — мерзімі өткен жазылымдарды белгілейтін, мерзімі туралы
// еске салатын және жазылмай қалған «тариф қосылды» хабарын жіберетін фон тапсырмасы.
func expireSubscriptions(ctx context.Context, subs *subscriptions.Service, events *notifications.Events, log *slog.Logger) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		if count, err := subs.ExpireDue(ctx); err != nil {
			log.Error("subscription expiry sweep failed", "error", err.Error())
		} else if count > 0 {
			log.Info("subscriptions expired", "count", count)
		}
		events.SweepSubscriptions(ctx)
		events.ReconcileActivations(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
