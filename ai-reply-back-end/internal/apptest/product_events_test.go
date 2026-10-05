package apptest

import (
	"context"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/migrations"
)

func (h *harness) sendEvents(token string, events ...map[string]any) response {
	return h.do(http.MethodPost, "/api/v1/analytics/events", map[string]any{
		"platform": "ios", "app_version": "2.0.1", "events": events,
	}, h.auth(token))
}

// storedEvents — product_events жолдары: атауы мен JSON қасиеттері.
func (h *harness) storedEvents(userID string) []string {
	h.t.Helper()
	rows, err := h.db.Reader().Query(`SELECT name || ' ' || props || ' ' || platform || ' ' || app_version
		FROM product_events WHERE user_id = ? ORDER BY name`, userID)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			h.t.Fatal(err)
		}
		out = append(out, row)
	}
	return out
}

// Каталогтан өткен оқиғалар сақталады, қалғаны тек саналады.
func TestProductEventsKeepOnlyCatalogEvents(t *testing.T) {
	h := newHarness(t)
	if features, _ := h.do(http.MethodGet, "/api/v1/config", nil, nil).body["features"].(map[string]any); features["product_events"] != true {
		t.Fatalf("product events are on by default: %v", features)
	}
	session := h.signIn("+7 707 620 30 01")

	res := h.sendEvents(session.access,
		map[string]any{"name": "onboarding_started", "ts": "2026-03-10T08:59:00Z", "props": map[string]any{"version": 2, "trigger": "auto"}},
		map[string]any{"name": "gender_selected", "props": map[string]any{"source": "onboarding", "skipped": false}},
		map[string]any{"name": "keyboard_enabled_detected"},
		map[string]any{"name": "message_copied"},
		map[string]any{"name": "onboarding_reopened", "props": map[string]any{"note": "x"}},
		map[string]any{"name": "onboarding_started", "props": map[string]any{"version": "2"}},
	)
	if res.status != http.StatusOK || res.num("accepted") != 3 || res.num("rejected") != 3 {
		t.Fatalf("events: %d %s", res.status, res.raw)
	}
	want := []string{
		`gender_selected {"skipped":false,"source":"onboarding"} ios 2.0.1`,
		`keyboard_enabled_detected {} ios 2.0.1`,
		`onboarding_started {"trigger":"auto","version":2} ios 2.0.1`,
	}
	if got := h.storedEvents(session.userID); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("stored:\n%s", strings.Join(got, "\n"))
	}
	var clientTS, createdAt int64
	if err := h.db.Reader().QueryRow(`SELECT client_ts, created_at FROM product_events WHERE name = 'onboarding_started'`).
		Scan(&clientTS, &createdAt); err != nil || clientTS != time.Date(2026, 3, 10, 8, 59, 0, 0, time.UTC).UnixMilli() ||
		createdAt != h.clock.Now().UnixMilli() {
		t.Fatalf("times: client %d, created %d (%v)", clientTS, createdAt, err)
	}
	if !strings.Contains(h.logs.String(), `"msg":"product_events_rejected"`) {
		t.Fatal("a batch with rejected events is not logged")
	}

	// Nothing accepted is not an error: the batch is answered, nothing is stored.
	none := h.sendEvents(session.access, map[string]any{"name": "unknown_event"})
	if none.status != http.StatusOK || none.num("accepted") != 0 || none.num("rejected") != 1 {
		t.Fatalf("all rejected: %d %s", none.status, none.raw)
	}
}

// Еркін мәтін ешқашан дерекқорға да, журналға да түспейді.
func TestProductEventsNeverStoreText(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 707 620 30 02")
	const secret = "ҚҰПИЯ Айгерим 8 701 555 66 77"

	res := h.sendEvents(session.access,
		map[string]any{"name": "onboarding_step_viewed", "props": map[string]any{"step": secret}},
		map[string]any{"name": "onboarding_reopened", "props": map[string]any{secret: true}},
		map[string]any{"name": secret},
		map[string]any{"name": "gender_selected", "props": map[string]any{"source": "settings", "gender": "female"}},
		map[string]any{"name": "onboarding_reopened", "ts": secret},
		map[string]any{"name": "onboarding_reopened", "comment": secret},
	)
	if res.status != http.StatusOK || res.num("accepted") != 0 || res.num("rejected") != 6 {
		t.Fatalf("events: %d %s", res.status, res.raw)
	}
	for _, needle := range []string{secret, "ҚҰПИЯ", "female"} {
		if h.dbContains(needle) || strings.Contains(h.logs.String(), needle) {
			t.Fatalf("%q was written somewhere", needle)
		}
	}
	if version := h.do(http.MethodPost, "/api/v1/analytics/events", map[string]any{
		"platform": "ios", "app_version": "Айгерим", "events": []map[string]any{{"name": "onboarding_reopened"}},
	}, h.auth(session.access)); version.status != http.StatusBadRequest || h.dbContains("Айгерим") {
		t.Fatalf("a word as app_version: %d %s", version.status, version.raw)
	}
}

func TestProductEventsRejectAnUnusableBatch(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 707 620 30 03")
	one := []map[string]any{{"name": "onboarding_reopened"}}
	many := make([]map[string]any, 21)
	for i := range many {
		many[i] = map[string]any{"name": "onboarding_reopened"}
	}
	cases := []struct {
		body  map[string]any
		field string
	}{
		{map[string]any{"platform": "web", "events": one}, "platform"},
		{map[string]any{"events": one}, "platform"},
		{map[string]any{"platform": "android", "app_version": "latest", "events": one}, "app_version"},
		{map[string]any{"platform": "android", "app_version": "8 701 555 66 77", "events": one}, "app_version"},
		{map[string]any{"platform": "android", "events": []map[string]any{}}, "events"},
		{map[string]any{"platform": "android"}, "events"},
		{map[string]any{"platform": "android", "events": many}, "events"},
		{map[string]any{"platform": "android", "events": one, "device": "Pixel"}, ""},
	}
	for _, c := range cases {
		res := h.do(http.MethodPost, "/api/v1/analytics/events", c.body, h.auth(session.access))
		if res.status != http.StatusBadRequest || res.errorCode() != "INVALID_REQUEST" || res.str("error", "details", "field") != c.field {
			t.Fatalf("%v: %d %s", c.body, res.status, res.raw)
		}
	}
	if res := h.do(http.MethodPost, "/api/v1/analytics/events", map[string]any{"platform": "android", "events": many},
		h.auth(session.access)); res.num("error", "details", "max_events") != 20 {
		t.Fatalf("the limit is not reported: %s", res.raw)
	}
	if got := h.storedEvents(session.userID); len(got) != 0 {
		t.Fatalf("a rejected batch stored %v", got)
	}

	// Android without an app version, and a version with a build number: fine.
	for _, version := range []string{"", "1.0", "2.0.1 (57)"} {
		if res := h.do(http.MethodPost, "/api/v1/analytics/events", map[string]any{"platform": " Android ",
			"app_version": version, "events": one}, h.auth(session.access)); res.status != http.StatusOK || res.num("accepted") != 1 {
			t.Fatalf("android %q: %d %s", version, res.status, res.raw)
		}
	}
}

func TestProductEventsNeedASignedInUser(t *testing.T) {
	h := newHarness(t)
	res := h.do(http.MethodPost, "/api/v1/analytics/events", map[string]any{
		"platform": "ios", "events": []map[string]any{{"name": "onboarding_reopened"}},
	}, nil)
	if res.status != http.StatusUnauthorized {
		t.Fatalf("anonymous events: %d", res.status)
	}
}

func TestProductEventsCanBeSwitchedOff(t *testing.T) {
	h := newHarness(t, withEnv("PRODUCT_EVENTS_ENABLED", "false"))
	features, _ := h.do(http.MethodGet, "/api/v1/config", nil, nil).body["features"].(map[string]any)
	if features["product_events"] != false {
		t.Fatalf("features = %v", features)
	}
	session := h.signIn("+7 707 620 30 04")
	res := h.sendEvents(session.access, map[string]any{"name": "onboarding_reopened"})
	if res.status != http.StatusNotFound || res.errorCode() != "NOT_FOUND" || len(h.storedEvents(session.userID)) != 0 {
		t.Fatalf("switched off: %d %s", res.status, res.raw)
	}
}

// Оқиғалардың өз шелегі бар: олар AI лимитін жемейді, AI оларды жемейді.
func TestProductEventsHaveTheirOwnRateLimit(t *testing.T) {
	h := newHarness(t, withEnv("RATE_EVENTS_PER_MINUTE", "2"), withEnv("RATE_AI_PER_MINUTE", "2"))
	session := h.signIn("+7 707 620 30 05")
	for i := 0; i < 2; i++ {
		if res := h.sendEvents(session.access, map[string]any{"name": "onboarding_reopened"}); res.status != http.StatusOK {
			t.Fatalf("event %d: %d", i, res.status)
		}
	}
	if res := h.sendEvents(session.access, map[string]any{"name": "onboarding_reopened"}); res.status != http.StatusTooManyRequests {
		t.Fatalf("third batch: %d, want 429", res.status)
	}
	if res := h.generate(session.access, "Сәлеметсіз бе, бағасы қанша?"); res.status != http.StatusOK {
		t.Fatalf("the events bucket spilled into AI: %d %s", res.status, res.raw)
	}
}

func TestAdminDashboardCountsProductEvents(t *testing.T) {
	h := newHarness(t)
	for _, phone := range []string{"+7 707 620 30 06", "+7 707 620 30 07"} {
		session := h.signIn(phone)
		h.sendEvents(session.access, map[string]any{"name": "onboarding_completed", "props": map[string]any{"version": 2, "skipped": false}},
			map[string]any{"name": "autocorrect_disabled"})
	}
	h.sendEvents(h.signIn("+7 707 620 30 06").access, map[string]any{"name": "onboarding_completed"})

	counts, err := h.store.ProductEventCounts(context.Background(), h.clock.Now().Add(-time.Hour), h.clock.Now().Add(time.Hour))
	want := []repository.Point{{Label: "onboarding_completed", Value: 3}, {Label: "autocorrect_disabled", Value: 2}}
	if err != nil || len(counts) != 2 || counts[0] != want[0] || counts[1] != want[1] {
		t.Fatalf("counts = %v (%v)", counts, err)
	}

	admin := h.signInAdmin()
	dashboard := h.do(http.MethodGet, "/api/v1/admin/dashboard?range=today", nil, admin.headers(h.cfg.Admin.CookieName))
	series, _ := dashboard.body["series"].(map[string]any)
	events, _ := series["product_events"].([]any)
	if dashboard.status != http.StatusOK || len(events) != 2 {
		t.Fatalf("dashboard: %d %s", dashboard.status, dashboard.raw)
	}
}

// 0010 бар дерекқорға қолданылады: жаңа кесте ғана, бұрынғы деректер өзгермейді.
func TestProductEventsMigration(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "old.db"), MaxReadConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	before := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") && e.Name() < "0010" {
			body, _ := fs.ReadFile(migrations.FS, e.Name())
			before[e.Name()] = &fstest.MapFile{Data: body}
		}
	}
	if _, err := database.Migrate(ctx, db, before); err != nil {
		t.Fatalf("old migrations: %v", err)
	}
	if _, err := db.Writer().ExecContext(ctx,
		`INSERT INTO users (id, email, status, created_at, updated_at) VALUES ('u-1', 'one@example.com', 'active', 1000, 1000)`); err != nil {
		t.Fatal(err)
	}

	applied, err := database.Migrate(ctx, db, migrations.FS)
	if err != nil || len(applied) != 1 || applied[0] != "0010_product_events.sql" {
		t.Fatalf("applied = %v (%v)", applied, err)
	}
	store := repository.New(db)
	at := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	if err := store.InsertProductEvents(ctx, "u-1", "android", "1.0", []repository.ProductEvent{
		{Name: "onboarding_reopened", Props: "{}"}, {Name: "autocorrect_enabled", Props: "{}", ClientTime: at},
	}, at); err != nil {
		t.Fatal(err)
	}
	var withoutClock int
	if err := db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM product_events WHERE client_ts IS NULL`).Scan(&withoutClock); err != nil || withoutClock != 1 {
		t.Fatalf("an event without a device time must store NULL: %d (%v)", withoutClock, err)
	}

	// Deleting the account deletes its events.
	if _, err := db.Writer().ExecContext(ctx, `DELETE FROM users WHERE id = 'u-1'`); err != nil {
		t.Fatal(err)
	}
	if counts, err := store.ProductEventCounts(ctx, at.Add(-time.Hour), at.Add(time.Hour)); err != nil || len(counts) != 0 {
		t.Fatalf("events outlived the account: %v (%v)", counts, err)
	}
	if again, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(again) != 0 {
		t.Fatalf("second run applied %v (%v)", again, err)
	}
}
