package apptest

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// Әкімші панелінің (admin-app.js) әр кілті төрт тілде де болуы керек.
//
// The SPA falls back to English and then to the key itself, so a missing
// translation shows up in the browser as "admin.push.form.title" — easy to
// miss by eye. The test reads the script, collects every static key, and adds
// the key families the script builds from backend enums (statuses,
// categories, segments, …): a new enum value without a translation fails here
// instead of in front of an administrator.

func adminAsset(t *testing.T, parts ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(append([]string{"..", "transport", "web"}, parts...)...))
	if err != nil {
		t.Fatalf("read %v: %v", parts, err)
	}
	return string(raw)
}

// adminStaticKeys — 'admin.*', 'common.*' and 'pricing.*' string literals in the SPA and its shell.
func adminStaticKeys(sources ...string) []string {
	pattern := regexp.MustCompile("['\"`]((?:admin|common|pricing)\\.[a-z0-9_.]+)['\"`]")
	found := map[string]bool{}
	for _, source := range sources {
		for _, match := range pattern.FindAllStringSubmatch(source, -1) {
			key := match[1]
			// Prefixes joined with a value ("admin.push.category." + c) are
			// checked through adminKeyFamilies.
			if strings.HasSuffix(key, ".") || strings.HasSuffix(key, "_") {
				continue
			}
			found[key] = true
		}
	}
	out := make([]string, 0, len(found))
	for key := range found {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// adminKeyFamilies — prefix → every value the script can append to it.
func adminKeyFamilies() map[string][]string {
	return map[string][]string{
		"admin.push.campaign_status.": domain.CampaignStatuses,
		"admin.push.delivery_status.": domain.DeliveryStatuses,
		"admin.push.category.":        domain.NotificationCategories,
		"admin.push.screen.":          domain.LinkScreens,
		"admin.push.push_status.":     {domain.PushNone, domain.PushActive, domain.PushInvalid, domain.PushReplaced},
		"admin.push.permission.": {domain.PermissionAuthorized, domain.PermissionDenied, domain.PermissionNotDetermined,
			domain.PermissionProvisional, domain.PermissionEphemeral, domain.PermissionUnknown},
		// The audience enums notifications.ValidateAudience accepts (GET /notifications lists them).
		"admin.push.segment.":      {domain.SegmentAll, domain.SegmentFree, domain.SegmentPaid, domain.SegmentDemo},
		"admin.push.subscription.": {domain.SubscriptionFilterActive, domain.SubscriptionFilterExpired},
		"admin.push.quota.":        {domain.QuotaHasRemaining, domain.QuotaNearExhausted, domain.QuotaExhausted},
		// Delivery filters and columns.
		"admin.push.channel.": {domain.ChannelPush, domain.ChannelEmail},
		"admin.push.source.":  {repository.SourceCampaign, repository.SourceAutomatic},
		"admin.push.type.":    append([]string{domain.TypeCampaign}, domain.AutomaticNotificationTypes...),
		// Reports on AI replies.
		"admin.reports.reason.": domain.ReportReasons,
		"admin.reports.status.": domain.ReportStatuses,
		"admin.reports.mode.":   domain.ReportModes,
		// Dashboard range buttons.
		"common.": {"today", "7d", "30d", "month", "prev_month"},
	}
}

var adminPlaceholderPattern = regexp.MustCompile(`\{(\w+)\}`)

func adminPlaceholders(s string) []string {
	var out []string
	for _, m := range adminPlaceholderPattern.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

func TestAdminTranslationsAreCompleteInEveryLocale(t *testing.T) {
	script := adminAsset(t, "static", "admin-app.js")
	shell := adminAsset(t, "templates", "admin_app.gohtml")

	required := adminStaticKeys(script, shell)
	if len(required) < 250 {
		t.Fatalf("suspiciously few admin keys collected: %d", len(required))
	}
	for prefix, values := range adminKeyFamilies() {
		// A family that is no longer built in the script would make this list stale.
		if !strings.Contains(script, "'"+prefix+"' +") && !strings.Contains(script, "\""+prefix+"\" +") {
			t.Errorf("admin-app.js no longer builds keys from %q: update adminKeyFamilies", prefix)
		}
		for _, value := range values {
			required = append(required, prefix+value)
		}
	}

	english := loadLocale(t, "en")
	for _, key := range required {
		// The shell sends the SPA only admin.*, common.* and pricing.per_day (web.adminMessages).
		if strings.HasPrefix(key, "pricing.") && key != "pricing.per_day" {
			t.Errorf("%s is used by the admin SPA but never sent to it", key)
		}
	}
	for _, locale := range domain.Locales {
		messages := loadLocale(t, locale)
		var missing, mismatched []string
		for _, key := range required {
			value := strings.TrimSpace(messages[key])
			if value == "" {
				missing = append(missing, key)
				continue
			}
			if !reflect.DeepEqual(adminPlaceholders(value), adminPlaceholders(english[key])) {
				mismatched = append(mismatched, key)
			}
		}
		sort.Strings(missing)
		if len(missing) > 12 {
			missing = append(missing[:12], "…")
		}
		if len(missing) > 0 {
			t.Errorf("locale %s is missing %d admin keys: %v", locale, len(missing), missing)
		}
		if len(mismatched) > 0 {
			t.Errorf("locale %s has other {placeholders} than en in: %v", locale, mismatched)
		}
	}
}

// The status filters of the SPA list exactly the statuses the backend has.
func TestAdminScriptStatusListsMatchTheBackend(t *testing.T) {
	script := adminAsset(t, "static", "admin-app.js")
	list := func(name string) []string {
		m := regexp.MustCompile(`var ` + name + ` = \[([^\]]*)\]`).FindStringSubmatch(script)
		if m == nil {
			t.Fatalf("admin-app.js has no %s list", name)
		}
		var out []string
		for _, item := range regexp.MustCompile(`"([a-z_]+)"`).FindAllStringSubmatch(m[1], -1) {
			out = append(out, item[1])
		}
		return out
	}
	for name, want := range map[string][]string{
		"CAMPAIGN_STATUSES": domain.CampaignStatuses,
		"DELIVERY_STATUSES": domain.DeliveryStatuses,
		"PUSH_STATUSES":     {domain.PushNone, domain.PushActive, domain.PushInvalid, domain.PushReplaced},
	} {
		if got := list(name); !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, backend has %v", name, got, want)
		}
	}
}

// The shell versions the admin script and stylesheet, so a deploy is never
// served a cached script from the previous one.
func TestAdminShellVersionsItsAssets(t *testing.T) {
	h := newHarness(t)
	adminSession := h.signInAdmin()

	req, _ := http.NewRequest(http.MethodGet, h.server.URL+"/admin/notifications/new?user_ids=abc", nil)
	req.AddCookie(&http.Cookie{Name: h.cfg.Admin.CookieName, Value: adminSession.cookie})
	res, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatalf("shell: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("shell status %d", res.StatusCode)
	}
	page := string(body)
	for _, asset := range []string{"admin-app.js", "admin.css"} {
		if !regexp.MustCompile(regexp.QuoteMeta("/static/"+asset) + `\?v=[0-9a-f]{12}"`).MatchString(page) {
			t.Errorf("shell does not version %s", asset)
		}
	}
	if status, _ := fetch(t, h, "/static/admin-app.js?v=0123456789ab"); status != http.StatusOK {
		t.Fatalf("versioned script: status %d", status)
	}
}
