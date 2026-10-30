package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/installations"
	"github.com/aireply/ai-reply-back-end/internal/localization"
	"github.com/aireply/ai-reply-back-end/internal/notifications"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/push"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

var maintenanceUserID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func queueSandboxWebOfferCommand(envFile, userID string) int {
	cfg, err := config.Load(envFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sandbox offer configuration check failed")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// The command prints only its outcome. The existing worker owns delivery;
	// no account details, token fingerprints or credentials are printed here.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := queueSandboxWebOffer(ctx, cfg, userID, log, configurePush)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sandbox offer queue failed:", err)
		return 1
	}
	fmt.Println(result)
	return 0
}

// This maintenance operation uses the same eligibility and durable once guard
// as preference/installation registration. It starts no workers and sends nothing.
func queueSandboxWebOffer(ctx context.Context, primary config.Config, userID string, log *slog.Logger,
	providerFactory func(config.Push, *slog.Logger) (push.Provider, error),
) (string, error) {
	if !maintenanceUserID.MatchString(userID) {
		return "", fmt.Errorf("a valid user UUID is required")
	}
	if !primary.BillingSandboxEnabled {
		return "", fmt.Errorf("isolated billing sandbox is disabled")
	}
	if !primary.BillingSandboxPushEnabled || !primary.BillingSandboxWebTopupOfferEnabled {
		return "", fmt.Errorf("sandbox website offer push is disabled")
	}
	cfg, err := billingSandboxConfig(primary)
	if err != nil {
		return "", fmt.Errorf("sandbox configuration is invalid")
	}
	provider, err := providerFactory(cfg.Push, log)
	if err != nil || provider == nil {
		return "", fmt.Errorf("sandbox push provider is unavailable")
	}
	bundle, err := localization.Load()
	if err != nil {
		return "", fmt.Errorf("notification translations are unavailable")
	}
	db, err := database.Open(database.Options{Path: cfg.Database.Path, BusyTimeout: cfg.Database.BusyTimeout,
		MaxReadConns: 1, MustExist: true})
	if err != nil {
		return "", fmt.Errorf("existing sandbox database could not be opened")
	}
	defer db.Close()
	queued := func() (bool, error) {
		var exists bool
		err := db.Reader().QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM notification_preferences
			WHERE user_id=? AND category='marketing' AND web_topup_offer_queued_at IS NOT NULL)`, userID).Scan(&exists)
		return exists, err
	}
	before, err := queued()
	if err != nil {
		return "", fmt.Errorf("sandbox notification schema is unavailable; start the server before this command")
	}
	if before {
		return "already_queued", nil
	}
	store := repository.New(db)
	prices := catalog.New(db)
	if _, err := prices.Published(ctx); err != nil {
		return "", fmt.Errorf("sandbox published catalog is unavailable")
	}
	installSvc := installations.New(store, cfg.Auth.AccessSecret, log)
	notifySvc := notifications.New(notifications.Deps{Repo: store, Installations: installSvc, Provider: provider,
		Plans: plans.New(store), Config: cfg.Push, Location: cfg.App.Location(), Translate: bundle.T, Log: log})
	notifySvc.WithWebTopupOffers(prices, cfg.App.PublicBaseURL)
	notifySvc.MaybeQueueWebTopupOffer(ctx, userID)
	after, err := queued()
	if err != nil {
		return "", fmt.Errorf("sandbox offer outcome could not be read")
	}
	if after {
		return "queued", nil
	}
	return "not_eligible", nil
}
