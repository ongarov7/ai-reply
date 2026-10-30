package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/installations"
	"github.com/aireply/ai-reply-back-end/internal/push"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/migrations"
)

const queueFixtureUser = "81000000-0000-4000-8000-000000000001"

type queueOnlyProvider struct{}

func (queueOnlyProvider) Name() string { return "fcm" }
func (queueOnlyProvider) Send(context.Context, push.Target, push.Message) push.Result {
	panic("maintenance command must not send push notifications")
}

func queueFixtureProvider(config.Push, *slog.Logger) (push.Provider, error) {
	return queueOnlyProvider{}, nil
}

func queueOfferFixture(t *testing.T) (config.Config, *slog.Logger) {
	t.Helper()
	cfg := billingPrimaryFixture(t)
	cfg.BillingSandboxPushEnabled, cfg.BillingSandboxWebTopupOfferEnabled = true, true
	cfg.Push.FCM = config.FCM{ProjectID: "fixture-project", ClientEmail: "fixture@example.invalid", PrivateKey: "fixture-only-key"}
	cfg.Push.LinkHosts = []string{"billing.example.invalid"}
	return cfg, slog.New(slog.NewTextHandler(io.Discard, nil))
}

func seedQueueOfferDatabase(t *testing.T, cfg config.Config, consent bool, build string) *database.DB {
	t.Helper()
	ctx := context.Background()
	db, err := database.Open(database.Options{Path: cfg.Database.Path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := database.Migrate(ctx, db, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if err := catalog.New(db).EnsureInitial(ctx); err != nil {
		t.Fatal(err)
	}
	store := repository.New(db)
	if _, err := store.CreateUser(ctx, domain.User{ID: queueFixtureUser, Email: "queue-fixture@example.invalid", Locale: "en", Timezone: "Asia/Almaty"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetNotificationPreferences(ctx, queueFixtureUser, map[string]bool{domain.CategoryMarketing: consent}, time.Now()); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := installations.New(store, cfg.Auth.AccessSecret, log).Register(ctx, installations.Registration{
		InstallationID: "fixture-installation-queue", Platform: domain.PlatformIOS, AppVersion: "1.2.0", AppBuild: build,
		Locale: "en", Timezone: "Asia/Almaty", Permission: domain.PermissionAuthorized,
		Push: &installations.PushToken{Provider: "fcm", Token: "fixture-only-fcm-token"},
	}, queueFixtureUser); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestQueueSandboxWebOfferOnlyQueuesChildAndRetainsOnceGuard(t *testing.T) {
	cfg, log := queueOfferFixture(t)
	child, err := billingSandboxConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	primaryDB := seedQueueOfferDatabase(t, cfg, true, "14")
	childDB := seedQueueOfferDatabase(t, child, true, "14")
	for _, expected := range []string{"queued", "already_queued"} {
		result, err := queueSandboxWebOffer(context.Background(), cfg, queueFixtureUser, log, queueFixtureProvider)
		if err != nil || result != expected {
			t.Fatalf("queue result %q, expected %q; error %v", result, expected, err)
		}
	}
	var link string
	if err := childDB.Reader().QueryRow(`SELECT link FROM notifications WHERE user_id=? AND type='web_topup_offer'`, queueFixtureUser).Scan(&link); err != nil || link != "https://billing.example.invalid/sandbox/payment" {
		t.Fatal("maintenance offer did not use the child catalog URL")
	}
	for _, test := range []struct {
		db    *database.DB
		query string
		want  int
	}{
		{primaryDB, `SELECT count(*) FROM notifications`, 0},
		{primaryDB, `SELECT count(*) FROM notification_preferences WHERE web_topup_offer_queued_at IS NOT NULL`, 0},
		{childDB, `SELECT count(*) FROM notifications WHERE type='web_topup_offer'`, 1},
		{childDB, `SELECT count(*) FROM notification_deliveries WHERE channel='push' AND status='queued' AND attempt_count=0`, 1},
		{childDB, `SELECT count(*) FROM notification_deliveries WHERE channel='email'`, 0},
		{childDB, `SELECT count(*) FROM billing_orders`, 0},
		{childDB, `SELECT count(*) FROM credit_grants`, 0},
	} {
		var count int
		if err := test.db.Reader().QueryRow(test.query).Scan(&count); err != nil || count != test.want {
			t.Fatalf("unexpected maintenance side effect: count %d, expected %d; error %v", count, test.want, err)
		}
	}
}

func TestQueueSandboxWebOfferPreservesConsentAndClientCompatibility(t *testing.T) {
	for _, name := range []string{"opted_out", "old_build"} {
		t.Run(name, func(t *testing.T) {
			cfg, log := queueOfferFixture(t)
			child, err := billingSandboxConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			build := "14"
			if name == "old_build" {
				build = "13"
			}
			db := seedQueueOfferDatabase(t, child, name != "opted_out", build)
			result, err := queueSandboxWebOffer(context.Background(), cfg, queueFixtureUser, log, queueFixtureProvider)
			if err != nil || result != "not_eligible" {
				t.Fatalf("ineligible account result %q; error %v", result, err)
			}
			var count int
			if err := db.Reader().QueryRow(`SELECT count(*) FROM notifications`).Scan(&count); err != nil || count != 0 {
				t.Fatal("maintenance command bypassed recipient eligibility")
			}
		})
	}
}

func TestQueueSandboxWebOfferRejectsUnsafeInputWithoutCreatingDatabase(t *testing.T) {
	for _, name := range []string{"sandbox_disabled", "push_disabled", "offer_disabled", "invalid_user", "missing_database"} {
		t.Run(name, func(t *testing.T) {
			cfg, log := queueOfferFixture(t)
			root := filepath.Dir(cfg.Database.Path)
			cfg.Database.Path = filepath.Join(root, "must-not-be-created", "primary.sqlite")
			userID := queueFixtureUser
			switch name {
			case "sandbox_disabled":
				cfg.BillingSandboxEnabled = false
			case "push_disabled":
				cfg.BillingSandboxPushEnabled, cfg.BillingSandboxWebTopupOfferEnabled = false, false
			case "offer_disabled":
				cfg.BillingSandboxWebTopupOfferEnabled = false
			case "invalid_user":
				userID = "invalid-user'; DELETE FROM users; --"
			}
			if _, err := queueSandboxWebOffer(context.Background(), cfg, userID, log, queueFixtureProvider); err == nil {
				t.Fatal("unsafe or unavailable maintenance input was accepted")
			} else if strings.Contains(err.Error(), userID) || strings.Contains(err.Error(), cfg.Database.Path) {
				t.Fatal("maintenance error echoed private input")
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("rejected maintenance invocation created files or directories")
			}
		})
	}
}
