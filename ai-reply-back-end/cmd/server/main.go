// Command server — AI Reply бэкенді: REST API, AI шлюзі, әкімші панелі, лендинг.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/admin"
	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/alerts"
	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/auth/appleid"
	"github.com/aireply/ai-reply-back-end/internal/auth/idtoken"
	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/credits"
	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/email"
	"github.com/aireply/ai-reply-back-end/internal/iap"
	"github.com/aireply/ai-reply-back-end/internal/installations"
	"github.com/aireply/ai-reply-back-end/internal/limits"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/logging"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/payments"
	"github.com/aireply/ai-reply-back-end/internal/payments/bcc"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/prodevices"
	"github.com/aireply/ai-reply-back-end/internal/productevents"
	"github.com/aireply/ai-reply-back-end/internal/push"
	"github.com/aireply/ai-reply-back-end/internal/reports"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/retention"
	"github.com/aireply/ai-reply-back-end/internal/simulator"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
	"github.com/aireply/ai-reply-back-end/internal/support"
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
	queueOffer := flag.String("queue-sandbox-web-offer", "", "queue the standard website offer for one eligible existing sandbox user and exit")
	alertTest := flag.Bool("alert-test", false, "send one test alert to the configured Telegram chat and exit")
	flag.Parse()
	queueRequested := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "queue-sandbox-web-offer" {
			queueRequested = true
		}
	})
	modes := 0
	for _, on := range []bool{*check, queueRequested, *alertTest} {
		if on {
			modes++
		}
	}
	if flag.NArg() != 0 || modes > 1 {
		fmt.Fprintln(os.Stderr, "choose one of -check, -alert-test or -queue-sandbox-web-offer USER_ID, without positional arguments")
		os.Exit(2)
	}
	if queueRequested {
		os.Exit(queueSandboxWebOfferCommand(*envFile, *queueOffer))
	}
	if *alertTest {
		os.Exit(alertTestCommand(*envFile))
	}

	if *check {
		os.Exit(checkConfig(*envFile))
	}
	if err := run(*envFile); err != nil {
		// The process is about to exit: one synchronous CRITICAL message, if Telegram is configured.
		alerts.Emergency("server exited with an error", err, os.Getenv("APP_ENV"))
		fmt.Fprintf(os.Stderr, "\nstartup failed:\n%v\n\nSee .env.example for the required configuration.\n", err)
		os.Exit(1)
	}
}

// alertTestCommand — Telegram баптауын тексеру: бір WARNING хабарлама синхронды жіберіледі.
func alertTestCommand(envFile string) int {
	cfg, err := config.Load(envFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration check failed:\n%v\n", err)
		return 1
	}
	notifier := alerts.New(telegramConfig(cfg), logging.New(cfg.Log.Level, "text"))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := notifier.SendNow(ctx, alerts.Warning, "test alert", "source", "server -alert-test", "env", cfg.App.Env); err != nil {
		fmt.Fprintf(os.Stderr, "test alert failed: %v\n", err)
		return 1
	}
	fmt.Println("test alert delivered to Telegram")
	return 0
}

func telegramConfig(cfg config.Config) alerts.Config {
	return alerts.Config{
		Enabled: cfg.Telegram.Enabled, BotToken: cfg.Telegram.BotToken, ChatID: cfg.Telegram.ChatID,
		Service: "ai-reply", Environment: cfg.App.Env,
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
	if cfg.BillingSandboxEnabled {
		if _, err := billingSandboxConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "configuration check failed: %v\n", err)
			return 1
		}
		fmt.Println("isolated billing sandbox: /sandbox (separate database and sessions)")
	}
	fmt.Printf("configuration OK (APP_ENV=%s)\nsign-in client ids: google=%v apple=%v\n",
		cfg.App.Env, cfg.OAuth.GoogleClientIDs, cfg.OAuth.AppleClientIDs)
	fmt.Printf("in-app purchases: app_store=%v (bundle %s, sandbox %v) google_play=%v (package %s, test %v)\n",
		cfg.IAP.Apple(), cfg.IAP.AppleBundleID, cfg.IAP.AppleAcceptSandbox, cfg.IAP.Google(), cfg.IAP.GooglePackageName,
		cfg.IAP.GoogleAcceptTest)
	fmt.Printf("app store sandbox accounts on this server: %d\n", len(cfg.IAP.AppleSandboxAccounts))
	fmt.Printf("telegram alerts: %v\n", cfg.Telegram.Enabled)
	return 0
}

func run(envFile string) error {
	cfg, err := config.Load(envFile)
	if err != nil {
		return err
	}
	// Telegram alerts: every ERROR-level record and every panic of the wrapped
	// logger becomes a message; the notifier itself logs through the plain one.
	base := logging.New(cfg.Log.Level, cfg.Log.Format)
	notifier := alerts.New(telegramConfig(cfg), base)
	log := slog.New(notifier.Handler(base.Handler()))
	for _, warning := range cfg.Warnings() {
		// Each start repeats them in Telegram: a setting that silently weakens
		// production (proxy, Apple revocation, legal name) is seen, not just logged.
		log.Warn("configuration warning: "+warning, logging.Alert())
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	var workers sync.WaitGroup
	var applications []*application
	// The notifier outlives the workers so that errors raised while they stop are still delivered.
	alertCtx, stopAlerts := context.WithCancel(context.Background())
	var alertWorker sync.WaitGroup
	alertWorker.Add(1)
	go func() {
		defer alertWorker.Done()
		notifier.Run(alertCtx)
	}()
	defer func() {
		stop()
		workers.Wait()
		for i := len(applications) - 1; i >= 0; i-- {
			applications[i].close()
		}
		stopAlerts()
		alertWorker.Wait()
	}()
	primary, err := buildApplication(ctx, cfg, log, notifier, &workers)
	if err != nil {
		return err
	}
	applications = append(applications, primary)
	if cfg.BillingSandboxEnabled {
		sandboxConfig, err := billingSandboxConfig(cfg)
		if err != nil {
			return err
		}
		// The sandbox shares the process and the alert channel; its records carry app=sandbox.
		sandbox, err := buildApplication(ctx, sandboxConfig, log.With("app", "sandbox"), alerts.New(alerts.Config{}, base), &workers)
		if err != nil {
			return fmt.Errorf("initialize isolated billing sandbox: %w", err)
		}
		applications = append(applications, sandbox)
		primary.mux.Handle("/sandbox/", http.StripPrefix("/sandbox", sandbox.handler))
		primary.mux.HandleFunc("GET /sandbox", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/sandbox/", http.StatusTemporaryRedirect)
		})
		log.Info("isolated billing sandbox available", "path", "/sandbox", "database_separate", true)
	}
	server := &http.Server{
		Addr: fmt.Sprintf("%s:%d", cfg.App.Host, cfg.App.Port), Handler: primary.handler,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 60 * time.Second, IdleTimeout: 90 * time.Second,
	}
	defer server.Close()
	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening", "addr", server.Addr, "env", cfg.App.Env, "timezone", cfg.App.Timezone)
		notifier.Notify(alerts.Recovered, "server started", "env", cfg.App.Env, "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		notifier.Notify(alerts.Warning, "server shutting down", "env", cfg.App.Env, "reason", "signal")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// A request still running after 15 s is cut off; that is a normal stop, not a failure.
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Warn("shutdown did not drain every request", "error", err.Error())
		}
		return nil
	}
}

type application struct {
	mux     *http.ServeMux
	handler http.Handler
	closers []func()
}

func (a *application) close() {
	for i := len(a.closers) - 1; i >= 0; i-- {
		a.closers[i]()
	}
}

// Each application owns its database, authentication keys, billing state and workers.
// notifier drives the periodic health checks (database, disk); a disabled one adds none.
func buildApplication(ctx context.Context, cfg config.Config, log *slog.Logger, notifier *alerts.Notifier, workers *sync.WaitGroup) (_ *application, err error) {
	instance := &application{}
	defer func() {
		if err != nil {
			instance.close()
		}
	}()
	db, err := database.Open(database.Options{
		Path:         cfg.Database.Path,
		BusyTimeout:  cfg.Database.BusyTimeout,
		MaxReadConns: cfg.Database.MaxReadConns,
	})
	if err != nil {
		return nil, err
	}
	instance.closers = append(instance.closers, func() { _ = db.Close() })

	if cfg.Database.MigrateOnStart {
		applied, err := database.Migrate(ctx, db, migrations.FS)
		if err != nil {
			return nil, err
		}
		if len(applied) > 0 {
			log.Info("migrations applied", "count", len(applied), "names", applied)
		}
	}

	bundle, err := localization.Load()
	if err != nil {
		return nil, err
	}

	// ---------------------------------------------------------------- wiring
	store := repository.New(db)
	catalogSvc := catalog.New(db)
	if err := catalogSvc.EnsureInitial(ctx); err != nil {
		return nil, fmt.Errorf("initialize billing catalog: %w", err)
	}
	bccCheckout, err := bcc.NewCheckout(cfg.BCCCheckout, db, catalogSvc)
	if err != nil {
		return nil, fmt.Errorf("initialize BCC checkout: %w", err)
	}
	bccCheckout.WithLogger(log)
	planSvc := plans.New(store)
	subSvc := subscriptions.New(store, planSvc, cfg.App.Location())
	userSvc := users.New(store).WithLogger(log)
	if err := configureAppleRevocation(userSvc, cfg.OAuth); err != nil {
		return nil, err
	}
	sender := auth.NewSender(cfg.Auth.OTPChannel, log)
	// Welcome bonus, admin credits and the ledger; a reservation still open
	// after StaleAfter(OPENAI_TIMEOUT) is refunded by the 15-minute sweep.
	creditSvc := credits.New(store, log).WithStaleAfter(credits.StaleAfter(cfg.OpenAI.Timeout))
	authSvc := auth.New(store, cfg.Auth, sender, subSvc, log).WithWelcome(creditSvc)
	// Account-deletion codes are issued in the background; let them finish
	// before the database closes.
	instance.closers = append(instance.closers, authSvc.Wait)
	mailer, err := newMailer(cfg, bundle)
	if err != nil {
		return nil, err
	}
	if err := configureSignIn(authSvc, cfg, mailer); err != nil {
		return nil, err
	}
	// Client id-лер — ашық мәндер: токеннің aud-ы сәйкес келмесе, журналда екеуі де көрінеді.
	log.Info("sign-in client ids", "google", cfg.OAuth.GoogleClientIDs, "apple", cfg.OAuth.AppleClientIDs,
		"apple_token_revocation", cfg.OAuth.AppleRevocation())
	installSvc := installations.New(store, cfg.Auth.AccessSecret, log)
	pushProvider, err := configurePush(cfg.Push, log)
	if err != nil {
		return nil, err
	}
	notifySvc := notifications.New(notifications.Deps{
		Repo: store, Installations: installSvc, Provider: pushProvider, Plans: planSvc,
		Config: cfg.Push, Location: cfg.App.Location(), Translate: bundle.T, Log: log,
	})
	notifySvc.WithProDeviceNotificationDelivery(cfg.ProDevices.NotificationsEnabled)
	notifySvc.WithWebTopupOffers(catalogSvc, cfg.App.PublicBaseURL)
	if mailer != nil {
		notifySvc.WithEmail(mailer, email.NotificationTemplates{Translate: bundle.T, Brand: cfg.Email.FromName})
		bccCheckout.WithReceiptEmail(mailer, email.NotificationTemplates{Translate: bundle.T, Brand: cfg.Email.FromName})
	}
	// Automatic notifications: plan activated (payment, admin), plan expiring
	// or expired (the expiry sweep below), quota low or exhausted (AI gateway).
	notifyEvents := notifications.NewEvents(notifySvc).
		WithDemoPayments(cfg.Payments.DemoCheckout && !cfg.App.IsProduction())
	proDeviceSvc := prodevices.New(store, subSvc, cfg.ProDevices, log).WithIdentityKey(cfg.Auth.AccessSecret).WithNotifications(notifySvc)
	provider := ai.NewOpenAI(cfg.OpenAI)
	limitSvc := limits.New(store, limits.Limits{
		SourceChars:      cfg.Limits.SourceTextChars,
		InstructionChars: cfg.Limits.InstructionChars,
		MaxOutputTokens:  cfg.OpenAI.MaxOutputTokens,
	})
	aiSvc := ai.New(store, subSvc, provider, limitSvc, log).WithProDevices(proDeviceSvc).WithMacContext(cfg.MacContext).WithRepair(cfg.AI.RepairEnabled).WithQuotaEvents(notifyEvents).
		WithPolishDailyLimit(cfg.Limits.PolishPerDay).
		// The same OpenAI client and key: /audio/transcriptions with OPENAI_TRANSCRIBE_MODEL.
		WithTranscriber(provider).WithTranscribeDailyLimit(cfg.Limits.TranscribePerDay)
	// No live provider exists yet (StoreKit / Play Billing come later), so
	// nothing can be bought unless a development server opts into demo checkout.
	paymentSvc := payments.New(store, subSvc, payments.DemoProvider{}, cfg.Payments.Mode).
		WithEvents(notifyEvents).WithDemoCheckout(cfg.Payments.DemoCheckout).WithProduction(cfg.App.IsProduction())
	eventSvc := productevents.New(store, log)
	reportSvc := reports.New(store, log)
	adminSvc := admin.New(store, subSvc, planSvc, cfg, log).WithEvents(notifyEvents)
	// Support chat: an operator reply sends one support_reply push per unread burst;
	// the conversation page shows the person's keyboard and permission diagnostics.
	supportSvc := support.New(store, subSvc, log).WithNotifier(notifyEvents).WithDiagnostics(adminSvc.SupportDiagnostics)
	// Response packs and passes through StoreKit 2 and Play Billing: verified against Apple's
	// root and the Play Developer API, granted through the credit ledger. Off until a store is
	// configured, an admin creates products and switches iap_enabled on.
	iapSvc, err := iap.New(iap.Deps{Repo: store, Config: cfg.IAP, Secret: cfg.Auth.AccessSecret, Log: log})
	if err != nil {
		return nil, err
	}
	simulatorSvc := simulator.New(simulator.Deps{
		Repo: store, Users: userSvc, Subs: subSvc, Plans: planSvc, AI: aiSvc, Limits: limitSvc,
		Config: cfg, Log: log,
	})

	if err := adminSvc.Bootstrap(ctx); err != nil {
		return nil, fmt.Errorf("admin bootstrap: %w", err)
	}

	limiter := middleware.NewLimiter()

	mux := http.NewServeMux()
	catalogSvc.RegisterPublic(mux)
	bccNotify, err := registerBCCSandboxNotify(mux, cfg)
	if err != nil {
		return nil, err
	}
	if bccNotify != nil {
		instance.closers = append(instance.closers, func() { _ = bccNotify.Close() })
	}
	apiServer := api.New(api.Deps{
		Config: cfg, Auth: authSvc, Users: userSvc, Plans: planSvc, Subs: subSvc,
		AI: aiSvc, Limits: limitSvc, Payments: paymentSvc, Events: eventSvc,
		Installations: installSvc, Notifications: notifySvc, Reports: reportSvc, Support: supportSvc,
		Limiter: limiter, Log: log,
		Ping:       func(ctx context.Context) error { return db.Reader().PingContext(ctx) },
		ProDevices: proDeviceSvc, Credits: creditSvc, IAP: iapSvc,
	})
	apiServer.Register(mux)
	apiServer.RegisterBCC(mux, bccCheckout)
	apiServer.RegisterWebAccount(mux, bccCheckout, catalogSvc)

	adminapi.New(adminapi.Deps{
		Catalog: catalogSvc,
		Bank:    bccCheckout,
		Config:  cfg, Admin: adminSvc, Limits: limitSvc, Notifications: notifySvc, Payments: paymentSvc,
		Reports: reportSvc, Credits: creditSvc, Support: supportSvc, IAP: iapSvc, Limiter: limiter, Log: log,
	}).Register(mux)

	simulatorapi.New(simulatorapi.Deps{
		Config: cfg, Admin: adminSvc, Simulator: simulatorSvc, Limiter: limiter, Log: log,
	}).Register(mux)

	webServer, err := web.New(web.Deps{
		Catalog: catalogSvc,
		Config:  cfg, Admin: adminSvc, Plans: planSvc, Payments: paymentSvc, Notifications: notifySvc,
		Bundle: bundle, Limiter: limiter, Log: log,
	})
	if err != nil {
		return nil, err
	}
	webServer.Register(mux)

	handler := middleware.Chain(mux,
		middleware.RequestID,
		middleware.Recover(log),
		middleware.Logging(log),
		middleware.ProxyHint(cfg.App.TrustProxy, log),
		middleware.SecurityHeaders(cfg.App.HSTS()),
		middleware.CORS(cfg.App.CORSOrigins),
	)

	backgroundWorkers := []func(context.Context){
		func(ctx context.Context) { bccCheckout.Run(ctx, log) },
		func(ctx context.Context) { expireSubscriptions(ctx, subSvc, notifyEvents, creditSvc, log) },
		func(ctx context.Context) { sweepReminders(ctx, notifyEvents) },
		func(ctx context.Context) {
			if !cfg.ProDevices.NotificationsEnabled {
				return
			}
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				if err := notifySvc.PumpProDeviceNotifications(ctx, cfg.ProDevices.NotificationCooldown); err != nil && ctx.Err() == nil {
					log.Error("pro device notification worker failed", "error", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		},
		notifySvc.Run,
		notifySvc.RunRetention,
		// Google Play: consume retries every 15 minutes, voided purchases every IAP_GOOGLE_VOIDED_POLL.
		iapSvc.Run,
		// Spent codes, dead sessions, abandoned installations, old metadata (RETENTION_*).
		retention.New(store, cfg.Retention, log).Run,
	}
	if notifier.Enabled() {
		// Downtime and recovery, where the process itself can see it: the database
		// stops answering, or the disk under it runs out (SQLite then fails every write).
		backgroundWorkers = append(backgroundWorkers, func(ctx context.Context) {
			notifier.Watch(ctx, time.Minute,
				// A real read from the file and an empty write transaction on the
				// single writer: a locked, stuck or unreadable database fails here
				// (go-sqlite3's Ping does no I/O).
				alerts.Check{Name: "database", Severity: alerts.Critical, Run: func(ctx context.Context) error {
					var n int
					if err := db.Reader().QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
						return err
					}
					return db.Tx(ctx, func(*sql.Tx) error { return nil })
				}},
				alerts.Check{Name: "disk space below 2 GiB", Severity: alerts.Warning, Run: alerts.DiskSpace(cfg.Database.Path, 2<<30)},
				alerts.Check{Name: "disk space below 500 MiB", Severity: alerts.Critical, Run: alerts.DiskSpace(cfg.Database.Path, 500<<20)},
			)
		})
	}
	for _, worker := range backgroundWorkers {
		workers.Add(1)
		go func(run func(context.Context)) {
			defer workers.Done()
			runWorker(ctx, log, run)
		}(worker)
	}

	instance.mux, instance.handler = mux, handler
	return instance, nil
}

// runWorker — фон тапсырмасы панинкадан бүкіл процесті құлатпайды: CRITICAL журнал мен
// ескерту (стек тек журналда), 10 секундтан кейін сол тапсырма қайта іске қосылады.
//
// Every worker loops until ctx is done, so a normal return means shutdown.
func runWorker(ctx context.Context, log *slog.Logger, run func(context.Context)) {
	for ctx.Err() == nil {
		panicked := func() (panicked bool) {
			defer func() {
				if rec := recover(); rec != nil {
					panicked = true
					log.LogAttrs(ctx, logging.LevelCritical, "background worker panicked",
						slog.String("panic", fmt.Sprint(rec)), slog.String("stack", string(debug.Stack())))
				}
			}()
			run(ctx)
			return false
		}()
		// A worker that returns by itself is done (some return at once when
		// their feature is off); only a panic brings it back.
		if !panicked || ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
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

// sweepReminders — диагностика дәлелі бар тіркелгілерге еске салу (30 минут сайын, reminders.enabled болса).
func sweepReminders(ctx context.Context, events *notifications.Events) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		events.SweepReminders(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// expireSubscriptions — мерзімі өткен жазылымдарды белгілейтін, мерзімі туралы
// еске салатын, жазылмай қалған «тариф қосылды» хабарын жіберетін және иесіз
// қалған генерация брондарын қайтаратын фон тапсырмасы.
func expireSubscriptions(ctx context.Context, subs *subscriptions.Service, events *notifications.Events,
	creditSvc *credits.Service, log *slog.Logger) {
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
		// A process that died mid-generation left its reservation open: the
		// person gets the unit back instead of paying for a reply never sent.
		if _, err := creditSvc.RefundStale(ctx); err != nil && ctx.Err() == nil {
			log.Error("stale reservation sweep failed", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
