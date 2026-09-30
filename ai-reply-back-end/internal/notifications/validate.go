package notifications

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/push"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Мазмұн шектері.
const (
	MaxTitleRunes     = 80
	MaxBodyRunes      = 400
	MaxDataKeys       = 10
	MaxDataValueRunes = 256
	MaxDataBytes      = 1024
	MaxLinkLength     = 512
	MaxUserIDs        = 500
)

// Content — хабарламаның мазмұны (бизнес-оқиға не науқан).
type Content struct {
	Title    string
	Body     string
	Category string
	Link     string
	Data     map[string]string
	Campaign bool
}

// reservedDataKeys — payload-тағы біздің кілттер және провайдерлердің тыйым салынған атаулары.
var reservedDataKeys = map[string]bool{
	"nid": true, "did": true, "type": true, "category": true, "link": true, "title": true, "body": true,
	"aps": true, "from": true, "notification": true, "message_type": true, "collapse_key": true,
	"priority": true, "ttl": true, "google": true, "gcm": true, "fcm": true, "data": true,
}

var dataKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// validateContent — мазмұнды тексеріп, тазалайды.
func (s *Service) validateContent(c Content) (Content, error) {
	c.Title = strings.TrimSpace(c.Title)
	c.Body = strings.ReplaceAll(strings.TrimSpace(c.Body), "\r\n", "\n")
	if c.Title == "" || utf8.RuneCountInString(c.Title) > MaxTitleRunes || hasControl(c.Title) {
		return c, domain.InvalidField("title", "1-80 characters, one line")
	}
	if c.Body == "" || utf8.RuneCountInString(c.Body) > MaxBodyRunes || hasControl(strings.NewReplacer("\n", "", "\t", "").Replace(c.Body)) {
		return c, domain.InvalidField("body", "1-400 characters")
	}
	if c.Category == "" {
		c.Category = domain.CategorySystem
		if c.Campaign {
			c.Category = domain.CategoryMarketing
		}
	}
	if !domain.IsNotificationCategory(c.Category) {
		return c, domain.InvalidField("category", "unknown")
	}
	link, err := ValidateLink(c.Link, s.cfg.LinkHosts)
	if err != nil {
		return c, err
	}
	c.Link = link
	data, err := validateData(c.Data)
	if err != nil {
		return c, err
	}
	c.Data = data

	// Both providers must accept the message with the longest ids we generate.
	sample := push.Message{Title: c.Title, Body: c.Body, Category: c.Category, CollapseID: strings.Repeat("x", 36),
		Data: map[string]string{"nid": strings.Repeat("x", 36), "did": strings.Repeat("x", 32),
			"type": "subscription_expiring", "category": c.Category, "link": c.Link}}
	for k, v := range data {
		sample.Data[k] = v
	}
	fcm, apns := push.PayloadSizes(sample)
	if fcm > push.MaxFCMPayloadBytes-200 || apns > push.MaxAPNsPayloadBytes-200 {
		return c, domain.InvalidField("body", "the notification is too large")
	}
	return c, nil
}

func validateData(in map[string]string) (map[string]string, error) {
	if len(in) == 0 {
		return map[string]string{}, nil
	}
	if len(in) > MaxDataKeys {
		return nil, domain.InvalidField("data", "at most 10 keys")
	}
	out := make(map[string]string, len(in))
	size := 0
	for k, v := range in {
		if !dataKeyPattern.MatchString(k) || reservedDataKeys[k] {
			return nil, domain.InvalidField("data."+traits.Clamp(k, 32), "key must be lowercase snake_case and not reserved")
		}
		v = strings.TrimSpace(v)
		if utf8.RuneCountInString(v) > MaxDataValueRunes || hasControl(v) {
			return nil, domain.InvalidField("data."+k, "at most 256 characters, one line")
		}
		size += len(k) + len(v)
		out[k] = v
	}
	if size > MaxDataBytes {
		return nil, domain.InvalidField("data", "at most 1 KB in total")
	}
	return out, nil
}

// ValidateLink — deep link: "aireply://<screen>" не рұқсат етілген хосттағы https мекенжайы.
//
// The apps open nothing else: a link naming an unknown screen or a foreign
// host is refused here, and the apps check again before navigating.
func ValidateLink(raw string, hosts []string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > MaxLinkLength || hasControl(raw) {
		return "", domain.InvalidField("link", "too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", domain.InvalidField("link", "not a URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "aireply":
		screen := strings.ToLower(u.Host)
		if u.User != nil || u.Port() != "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" ||
			!oneOf(screen, domain.LinkScreens) {
			return "", domain.InvalidField("link", "unknown screen")
		}
		return "aireply://" + screen, nil
	case "https":
		host := strings.ToLower(u.Hostname())
		if u.User != nil || u.Port() != "" || host == "" || !hostAllowed(host, hosts) {
			return "", domain.InvalidField("link", "host is not allowed")
		}
		u.Scheme, u.Host = "https", host
		return u.String(), nil
	default:
		return "", domain.InvalidField("link", "only aireply:// screens and https links")
	}
}

func hostAllowed(host string, allowed []string) bool {
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a != "" && (host == a || strings.HasSuffix(host, "."+a)) {
			return true
		}
	}
	return false
}

var (
	datePattern   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	userIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)
)

// ValidateAudience — сүзгіні тексеріп, қалыпқа келтіреді. Белгісіз мән қабылданбайды.
func ValidateAudience(f domain.AudienceFilter) (domain.AudienceFilter, error) {
	var err error
	if f.Platforms, err = enumList("audience.platforms", f.Platforms, domain.PlatformAndroid, domain.PlatformIOS); err != nil {
		return f, err
	}
	if f.Locales, err = enumList("audience.locales", f.Locales, domain.Locales...); err != nil {
		return f, err
	}
	if !oneOf(f.Auth, []string{"", "authenticated", "anonymous"}) {
		return f, domain.InvalidField("audience.auth", "unknown")
	}
	if !oneOf(f.Payment, []string{"", "paid", "unpaid"}) {
		return f, domain.InvalidField("audience.payment", "unknown")
	}
	if !oneOf(f.Subscription, []string{"", "active", "expired", "none"}) {
		return f, domain.InvalidField("audience.subscription", "unknown")
	}
	f.AppVersionMin, f.AppVersionMax = strings.TrimSpace(f.AppVersionMin), strings.TrimSpace(f.AppVersionMax)
	f.OSVersionMin = strings.TrimSpace(f.OSVersionMin)
	for field, v := range map[string]string{
		"audience.app_version_min": f.AppVersionMin, "audience.app_version_max": f.AppVersionMax,
		"audience.os_version_min": f.OSVersionMin,
	} {
		if v != "" && (len(v) > 32 || domain.VersionNumber(v) == 0) {
			return f, domain.InvalidField(field, "use a version like 1.3.0")
		}
	}
	if f.AppVersionMin != "" && f.AppVersionMax != "" &&
		domain.VersionNumber(f.AppVersionMin) > domain.VersionNumber(f.AppVersionMax) {
		return f, domain.InvalidField("audience.app_version_max", "lower than the minimum")
	}
	if f.ActiveWithinDays < 0 || f.ActiveWithinDays > 3650 {
		return f, domain.InvalidField("audience.active_within_days", "0-3650")
	}
	if f.InactiveForDays < 0 || f.InactiveForDays > 3650 {
		return f, domain.InvalidField("audience.inactive_for_days", "0-3650")
	}
	if f.ActiveWithinDays > 0 && f.InactiveForDays > 0 && f.InactiveForDays >= f.ActiveWithinDays {
		return f, domain.InvalidField("audience.inactive_for_days", "no device can match both")
	}
	for field, v := range map[string]string{"audience.registered_from": f.RegisteredFrom, "audience.registered_to": f.RegisteredTo} {
		if v == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", v); err != nil || !datePattern.MatchString(v) {
			return f, domain.InvalidField(field, "use YYYY-MM-DD")
		}
	}
	if f.RegisteredFrom != "" && f.RegisteredTo != "" && f.RegisteredFrom > f.RegisteredTo {
		return f, domain.InvalidField("audience.registered_to", "before the start date")
	}
	if len(f.UserIDs) > MaxUserIDs {
		return f, domain.InvalidField("audience.user_ids", "at most 500 users")
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(f.UserIDs))
	for _, id := range f.UserIDs {
		id = strings.TrimSpace(id)
		if !userIDPattern.MatchString(id) {
			return f, domain.InvalidField("audience.user_ids", "not a user id")
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	f.UserIDs = ids
	if f.Auth == "anonymous" && f.NeedsAccount() {
		return f, domain.InvalidField("audience.auth", "anonymous devices have no account, plan or registration date")
	}
	return f, nil
}

func enumList(field string, values []string, allowed ...string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if !oneOf(v, allowed) {
			return nil, domain.InvalidField(field, "unknown value")
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out, nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
