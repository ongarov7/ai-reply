package apptest

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Әкімші панелінің (admin-app.js) әр кілті төрт тілде де болуы керек.
//
// The SPA falls back to English and then to the key itself, so a missing
// translation shows up in the browser as "admin.push.form.title" — easy to
// miss by eye. The test reads the script, collects every static key, and adds
// the key families the script builds from backend enums (statuses,
// categories, screens, …): a new enum value without a translation fails here
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
func adminStaticKeys(t *testing.T, sources ...string) []string {
	t.Helper()
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
		// The audience enums notifications.ValidateAudience accepts.
		"admin.push.auth.":         {"authenticated", "anonymous"},
		"admin.push.payment.":      {"paid", "unpaid"},
		"admin.push.subscription.": {"active", "expired", "none"},
		"admin.logs.outcome.":      {domain.OutcomeSuccess, domain.OutcomeFailure},
		// Dashboard range buttons.
		"common.": {"today", "7d", "30d", "month", "prev_month"},
	}
}

var placeholderPattern = regexp.MustCompile(`\{(\w+)\}`)

func placeholders(s string) []string {
	var out []string
	for _, m := range placeholderPattern.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

func TestAdminTranslationsAreCompleteInEveryLocale(t *testing.T) {
	script := adminAsset(t, "static", "admin-app.js")
	shell := adminAsset(t, "templates", "admin_app.gohtml")

	required := adminStaticKeys(t, script, shell)
	if len(required) < 300 {
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
			if !reflect.DeepEqual(placeholders(value), placeholders(english[key])) {
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
