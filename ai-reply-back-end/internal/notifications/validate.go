package notifications

import (
	"context"
	"errors"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"
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
	MaxRecipients     = 500
	MaxPlanFilter     = 50
	maxParams         = 16
	maxParamRunes     = 200
)

// ContentLocales — науқан мәтіні берілетін тілдер; RequiredLocales — әкімші панелі
// міндетті деп көрсететіндері; DefaultFallbackLocale — тіл бос болса жіберілетін тіл.
var (
	ContentLocales        = domain.Locales
	RequiredLocales       = []string{"kk", "ru", "en"}
	DefaultFallbackLocale = "ru"
)

// Content — хабарламаның мазмұны (бизнес-оқиға не науқанның бір тілі).
type Content struct {
	Title    string
	Body     string
	Category string
	Link     string
	Data     map[string]string
	Campaign bool
}

// reservedDataKeys — payload-тағы біздің кілттер және провайдерлердің тыйым салынған атаулары.
// FCM also refuses the whole message for any key that starts with "google" or "gcm" (reservedDataKey).
var reservedDataKeys = map[string]bool{
	"nid": true, "did": true, "type": true, "category": true, "link": true, "title": true, "body": true,
	"aps": true, "from": true, "notification": true, "message_type": true, "collapse_key": true,
	"priority": true, "ttl": true, "fcm": true, "data": true,
}

func reservedDataKey(k string) bool {
	return reservedDataKeys[k] || strings.HasPrefix(k, "google") || strings.HasPrefix(k, "gcm")
}

var (
	dataKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	userIDPattern  = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)
)

// validateContent — мазмұнды тексеріп, тазалайды.
func (s *Service) validateContent(c Content) (Content, error) {
	title, body, err := validateText("", c.Title, c.Body)
	if err != nil {
		return c, err
	}
	c.Title, c.Body = title, body
	if c, err = s.validateEnvelope(c); err != nil {
		return c, err
	}
	if err := checkSize("body", c); err != nil {
		return c, err
	}
	return c, nil
}

// validateText — тақырып: 1–80 таңба, бір жол; мәтін: 1–400 таңба. suffix — өріс атауына (".kk").
func validateText(suffix, title, body string) (string, string, error) {
	title = strings.TrimSpace(title)
	body = strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n")
	if title == "" || utf8.RuneCountInString(title) > MaxTitleRunes || hasControl(title) {
		return title, body, domain.InvalidField("title"+suffix, "1-80 characters, one line")
	}
	if body == "" || utf8.RuneCountInString(body) > MaxBodyRunes ||
		hasControl(strings.NewReplacer("\n", "", "\t", "").Replace(body)) {
		return title, body, domain.InvalidField("body"+suffix, "1-400 characters")
	}
	return title, body, nil
}

// validateEnvelope — санат, сілтеме және қосымша кілттер.
func (s *Service) validateEnvelope(c Content) (Content, error) {
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
	return c, nil
}

// checkSize — хабарлама біз жасайтын ең ұзын идентификаторлармен де FCM мен APNs шегіне сыяды.
func checkSize(field string, c Content) error {
	sample := push.Message{Title: c.Title, Body: c.Body, Category: c.Category, CollapseID: strings.Repeat("x", 36),
		TTL: DefaultTTL, Data: map[string]string{"nid": strings.Repeat("x", 36), "did": strings.Repeat("x", 32),
			"type": "subscription_activated", "category": c.Category, "link": c.Link}}
	for k, v := range c.Data {
		sample.Data[k] = v
	}
	fcm, apns := push.PayloadSizes(sample)
	if fcm > push.MaxFCMPayloadBytes-200 || apns > push.MaxAPNsPayloadBytes-200 {
		return domain.InvalidField(field, "the notification is too large")
	}
	return nil
}

// validateCampaign — науқанның әр тілдегі мәтіні, қор тілі және ортақ өрістері.
//
// A language is either complete (title and body) or empty. The fallback
// language (default ru) must be complete: it is what a recipient whose own
// language is empty receives.
func (s *Service) validateCampaign(in CampaignInput) (map[string]domain.LocalizedText, string, Content, error) {
	fallback := strings.ToLower(strings.TrimSpace(in.FallbackLocale))
	if fallback == "" {
		fallback = DefaultFallbackLocale
	}
	if !oneOf(fallback, ContentLocales) {
		return nil, "", Content{}, domain.InvalidField("fallback_locale", "unknown language")
	}
	for field, texts := range map[string]map[string]string{"title": in.Title, "body": in.Body} {
		for l := range texts {
			if !oneOf(l, ContentLocales) {
				return nil, "", Content{}, domain.InvalidField(field+"."+traits.Clamp(l, 8), "unknown language")
			}
		}
	}
	envelope, err := s.validateEnvelope(Content{Category: in.Category, Link: in.Link, Data: in.Data, Campaign: true})
	if err != nil {
		return nil, "", Content{}, err
	}
	content := map[string]domain.LocalizedText{}
	for _, l := range ContentLocales {
		title, body := strings.TrimSpace(in.Title[l]), strings.TrimSpace(in.Body[l])
		if title == "" && body == "" {
			continue
		}
		title, body, err := validateText("."+l, title, body)
		if err != nil {
			return nil, "", Content{}, err
		}
		sized := envelope
		sized.Title, sized.Body = title, body
		if err := checkSize("body."+l, sized); err != nil {
			return nil, "", Content{}, err
		}
		content[l] = domain.LocalizedText{Title: title, Body: body}
	}
	if !content[fallback].Filled() {
		return nil, "", Content{}, domain.InvalidField("title."+fallback, "the fallback language must be filled")
	}
	return content, fallback, envelope, nil
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
		if !dataKeyPattern.MatchString(k) || reservedDataKey(k) {
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

// validateParams — пошта үлгісінің мәндері (серверде қалады).
func validateParams(in map[string]string) (map[string]string, error) {
	if len(in) > maxParams {
		return nil, domain.InvalidField("params", "too many values")
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if !dataKeyPattern.MatchString(k) || utf8.RuneCountInString(v) > maxParamRunes || hasControl(v) {
			return nil, domain.InvalidField("params."+traits.Clamp(k, 32), "a short one-line value")
		}
		out[k] = v
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

// ValidateAudience — сүзгіні тексеріп, қалыпқа келтіреді. Белгісіз мән қабылданбайды.
//
// Lists are lowercased where it matters, deduplicated and sorted, and
// segment "all" becomes empty, so the same audience always has the same
// normalized form (a retried campaign compares equal).
func ValidateAudience(f domain.AudienceFilter) (domain.AudienceFilter, error) {
	var err error
	f.Segment = strings.ToLower(strings.TrimSpace(f.Segment))
	if f.Segment == domain.SegmentAll {
		f.Segment = ""
	}
	if !oneOf(f.Segment, []string{"", domain.SegmentFree, domain.SegmentPaid, domain.SegmentDemo}) {
		return f, domain.InvalidField("audience.segment", "unknown")
	}
	f.Subscription = strings.ToLower(strings.TrimSpace(f.Subscription))
	if !oneOf(f.Subscription, []string{"", domain.SubscriptionFilterActive, domain.SubscriptionFilterExpired}) {
		return f, domain.InvalidField("audience.subscription", "unknown")
	}
	f.Quota = strings.ToLower(strings.TrimSpace(f.Quota))
	if !oneOf(f.Quota, []string{"", domain.QuotaHasRemaining, domain.QuotaNearExhausted, domain.QuotaExhausted}) {
		return f, domain.InvalidField("audience.quota", "unknown")
	}
	if f.Platforms, err = enumList("audience.platforms", f.Platforms, domain.PlatformAndroid, domain.PlatformIOS); err != nil {
		return f, err
	}
	if f.Languages, err = enumList("audience.languages", f.Languages, domain.Locales...); err != nil {
		return f, err
	}
	if len(f.PlanIDs) > MaxPlanFilter {
		return f, domain.InvalidField("audience.plan_ids", "too many plans")
	}
	if f.PlanIDs, err = idList("audience.plan_ids", f.PlanIDs); err != nil {
		return f, err
	}
	if len(f.UserIDs)+len(f.Emails) > MaxRecipients {
		return f, domain.InvalidField("audience.user_ids", "at most 500 people")
	}
	if f.UserIDs, err = idList("audience.user_ids", f.UserIDs); err != nil {
		return f, err
	}
	seen := map[string]bool{}
	var emails []string
	for _, e := range f.Emails {
		e = strings.ToLower(strings.TrimSpace(e))
		if addr, err := mail.ParseAddress(e); err != nil || addr.Address != e || len(e) > 254 {
			return f, domain.InvalidField("audience.emails", "not an e-mail address")
		}
		if !seen[e] {
			seen[e] = true
			emails = append(emails, e)
		}
	}
	sort.Strings(emails)
	f.Emails = emails
	return f, nil
}

// validateAudience — ValidateAudience және тарифтердің бар-жоғы (тарифтер дерекқордан, кодта емес).
func (s *Service) validateAudience(ctx context.Context, f domain.AudienceFilter) (domain.AudienceFilter, error) {
	f, err := ValidateAudience(f)
	if err != nil {
		return f, err
	}
	for _, id := range f.PlanIDs {
		if _, err := s.repo.Plan(ctx, id); errors.Is(err, domain.ErrNotFound) {
			return f, domain.InvalidField("audience.plan_ids", "unknown plan")
		} else if err != nil {
			return f, err
		}
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

func idList(field string, values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if !userIDPattern.MatchString(v) {
			return nil, domain.InvalidField(field, "not an id")
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
