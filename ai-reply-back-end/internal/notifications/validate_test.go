package notifications

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func testService() *Service {
	return New(Deps{
		Config: config.Push{LinkHosts: []string{"ai-reply.kz"}},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func fieldOf(t *testing.T, err error) string {
	t.Helper()
	var field *domain.FieldError
	if !errors.As(err, &field) || !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("want a field error, got %v", err)
	}
	return field.Field
}

func TestValidateLinkAllowsOnlyKnownScreensAndHosts(t *testing.T) {
	hosts := []string{"ai-reply.kz"}
	for raw, want := range map[string]string{
		"":                                 "",
		"aireply://subscription":           "aireply://subscription",
		"AIREPLY://Settings/":              "aireply://settings",
		"https://ai-reply.kz/pricing":      "https://ai-reply.kz/pricing",
		"https://help.ai-reply.kz/a?b=1#c": "https://help.ai-reply.kz/a?b=1#c",
	} {
		got, err := ValidateLink(raw, hosts)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{
		"aireply://wallet", "aireply://home/extra", "aireply://home?x=1", "aireply://user@home",
		"http://ai-reply.kz", "https://evil.kz", "https://ai-reply.kz.evil.kz", "https://user@ai-reply.kz",
		"https://ai-reply.kz:8443/", "javascript:alert(1)", "intent://x", "https://ai-reply.kz/a\nb",
		"https://ai-reply.kz/" + strings.Repeat("a", 600),
	} {
		if _, err := ValidateLink(raw, hosts); err == nil || fieldOf(t, err) != "link" {
			t.Errorf("%q accepted", raw)
		}
	}
}

func TestValidateAudienceNormalisesAndRejectsUnknownValues(t *testing.T) {
	f, err := ValidateAudience(domain.AudienceFilter{
		Segment: " ALL ", Platforms: []string{"iOS", "android", "ios"}, Languages: []string{"ru", "KK"},
		Quota: "Near_Exhaustion", Subscription: "active",
		UserIDs: []string{"00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000001",
			"00000000-0000-4000-8000-000000000002"},
		Emails:  []string{" Person@Example.com", "person@example.com"},
		PlanIDs: []string{"00000000-0000-4000-8000-000000000003"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.Segment != "" || strings.Join(f.Platforms, ",") != "android,ios" || strings.Join(f.Languages, ",") != "kk,ru" ||
		f.Quota != domain.QuotaNearExhausted || len(f.UserIDs) != 2 || f.UserIDs[0] != "00000000-0000-4000-8000-000000000001" ||
		len(f.Emails) != 1 || f.Emails[0] != "person@example.com" {
		t.Fatalf("normalised = %+v", f)
	}
	for name, c := range map[string]struct {
		filter domain.AudienceFilter
		field  string
	}{
		"segment":      {domain.AudienceFilter{Segment: "vip"}, "audience.segment"},
		"subscription": {domain.AudienceFilter{Subscription: "none"}, "audience.subscription"},
		"quota":        {domain.AudienceFilter{Quota: "half"}, "audience.quota"},
		"platform":     {domain.AudienceFilter{Platforms: []string{"windows"}}, "audience.platforms"},
		"language":     {domain.AudienceFilter{Languages: []string{"de"}}, "audience.languages"},
		"plan id":      {domain.AudienceFilter{PlanIDs: []string{"pro' OR 1=1"}}, "audience.plan_ids"},
		"user id":      {domain.AudienceFilter{UserIDs: []string{"x"}}, "audience.user_ids"},
		"email":        {domain.AudienceFilter{Emails: []string{"Name <a@b.kz>"}}, "audience.emails"},
		"too many":     {domain.AudienceFilter{Emails: make([]string, 501)}, "audience.user_ids"},
	} {
		if _, err := ValidateAudience(c.filter); err == nil || fieldOf(t, err) != c.field {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestContentValidation(t *testing.T) {
	s := testService()
	ok, err := s.validateContent(Content{Title: "  Тариф  ", Body: "Мәтін\r\nекінші жол", Link: "aireply://home",
		Data: map[string]string{"promo_id": " 42 "}})
	if err != nil || ok.Title != "Тариф" || ok.Body != "Мәтін\nекінші жол" || ok.Category != domain.CategorySystem ||
		ok.Data["promo_id"] != "42" {
		t.Fatalf("content = %+v, %v", ok, err)
	}
	for name, c := range map[string]struct {
		content Content
		field   string
	}{
		"empty title":   {Content{Body: "b"}, "title"},
		"long title":    {Content{Title: strings.Repeat("ә", 81), Body: "b"}, "title"},
		"two lines":     {Content{Title: "a\nb", Body: "b"}, "title"},
		"long body":     {Content{Title: "t", Body: strings.Repeat("ә", 401)}, "body"},
		"category":      {Content{Title: "t", Body: "b", Category: "promo"}, "category"},
		"reserved key":  {Content{Title: "t", Body: "b", Data: map[string]string{"nid": "x"}}, "data.nid"},
		"google prefix": {Content{Title: "t", Body: "b", Data: map[string]string{"google_utm": "x"}}, "data.google_utm"},
		"gcm prefix":    {Content{Title: "t", Body: "b", Data: map[string]string{"gcm_campaign": "x"}}, "data.gcm_campaign"},
		"key format":    {Content{Title: "t", Body: "b", Data: map[string]string{"Bad-Key": "x"}}, "data.Bad-Key"},
		"long value":    {Content{Title: "t", Body: "b", Data: map[string]string{"k": strings.Repeat("x", 257)}}, "data.k"},
		"foreign link":  {Content{Title: "t", Body: "b", Link: "https://example.com"}, "link"},
		"too many keys": {Content{Title: "t", Body: "b", Data: tooManyKeys()}, "data"},
	} {
		if _, err := s.validateContent(c.content); err == nil || fieldOf(t, err) != c.field {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func tooManyKeys() map[string]string {
	out := map[string]string{}
	for i := 0; i < 11; i++ {
		out["k"+string(rune('a'+i))] = "v"
	}
	return out
}

// Науқан: бос тіл рұқсат, жартылай тіл — қате, қор тіл міндетті.
func TestCampaignContentPerLanguage(t *testing.T) {
	s := testService()
	content, fallback, envelope, err := s.validateCampaign(CampaignInput{
		Title: map[string]string{"kk": "Жаңалық", "ru": "Новость", "en": "News"},
		Body:  map[string]string{"kk": "Мәтін", "ru": "Текст", "en": "Text", "uz": "  "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fallback != "ru" || len(content) != 3 || content["kk"].Title != "Жаңалық" || envelope.Category != domain.CategoryMarketing {
		t.Fatalf("content = %+v, fallback %q, envelope %+v", content, fallback, envelope)
	}
	for name, c := range map[string]struct {
		in    CampaignInput
		field string
	}{
		"fallback empty": {CampaignInput{Title: map[string]string{"kk": "t"}, Body: map[string]string{"kk": "b"}}, "title.ru"},
		"half language": {CampaignInput{Title: map[string]string{"ru": "t", "kk": "t"},
			Body: map[string]string{"ru": "b"}}, "body.kk"},
		"unknown language": {CampaignInput{Title: map[string]string{"ru": "t", "de": "t"},
			Body: map[string]string{"ru": "b"}}, "title.de"},
		"fallback language": {CampaignInput{FallbackLocale: "de", Title: map[string]string{"ru": "t"},
			Body: map[string]string{"ru": "b"}}, "fallback_locale"},
		"long title": {CampaignInput{Title: map[string]string{"ru": "t", "en": strings.Repeat("x", 81)},
			Body: map[string]string{"ru": "b", "en": "b"}}, "title.en"},
	} {
		if _, _, _, err := s.validateCampaign(c.in); err == nil || fieldOf(t, err) != c.field {
			t.Errorf("%s: %v", name, err)
		}
	}
	// uz may be the fallback when it is filled.
	if _, fallback, _, err := s.validateCampaign(CampaignInput{FallbackLocale: "UZ",
		Title: map[string]string{"uz": "Yangilik"}, Body: map[string]string{"uz": "Matn"}}); err != nil || fallback != "uz" {
		t.Fatalf("uz fallback: %q %v", fallback, err)
	}
}

// Тіл бос болса — қор тіл, ол да бос болса — kk, ru, en, uz ретімен алғашқы толығы.
func TestCampaignTextForEachLanguage(t *testing.T) {
	c := domain.Campaign{FallbackLocale: "ru", Content: map[string]domain.LocalizedText{
		"kk": {Title: "Жаңалық", Body: "Мәтін"}, "ru": {Title: "Новость", Body: "Текст"},
		"en": {Title: "News"},
	}}
	for lang, want := range map[string]string{"kk": "Жаңалық", "ru": "Новость", "en": "Новость", "uz": "Новость"} {
		if text, _ := c.TextFor(lang); text.Title != want {
			t.Errorf("%s: %q, want %q", lang, text.Title, want)
		}
	}
	c.FallbackLocale = "uz"
	if text, from := c.TextFor("en"); text.Title != "Жаңалық" || from != "kk" {
		t.Fatalf("first filled: %q from %q", text.Title, from)
	}
}

func TestBackoffScheduleWithJitter(t *testing.T) {
	for attempt, want := range map[int]time.Duration{1: 5 * time.Second, 2: 30 * time.Second, 3: 2 * time.Minute,
		4: 10 * time.Minute, 5: 30 * time.Minute, 9: 30 * time.Minute} {
		if got := Backoff(attempt, 0, 0.5); got != want {
			t.Errorf("attempt %d: %v, want %v", attempt, got, want)
		}
	}
	if got := Backoff(1, 0, 0); got != 4*time.Second {
		t.Errorf("lowest jitter: %v", got)
	}
	if got := Backoff(1, 90*time.Second, 0.5); got != 90*time.Second {
		t.Errorf("Retry-After wins: %v", got)
	}
	if got := Backoff(1, 5*time.Hour, 0.5); got != time.Hour {
		t.Errorf("capped at an hour: %v", got)
	}
}

func TestPreferenceCategories(t *testing.T) {
	optional := OptionalCategories()
	if strings.Join(optional, ",") != "account,subscription,system,marketing" {
		t.Fatalf("optional = %v", optional)
	}
	if domain.CategoryOptional(domain.CategorySecurity) || !domain.CategoryImportant(domain.CategorySubscription) ||
		domain.CategoryImportant(domain.CategoryMarketing) {
		t.Fatal("security is always on; account, subscription and security are important")
	}
}

func TestPreferredLanguageNormalisation(t *testing.T) {
	for raw, want := range map[string]string{
		"kk": "kk", "RU": "ru", "ru-KZ": "ru", "en_US": "en", " uz ": "uz", "de": "", "": "", "russian": "",
	} {
		if got := domain.NormalizePreferredLanguage(raw); got != want {
			t.Errorf("%q: %q, want %q", raw, got, want)
		}
	}
	if domain.ResolveLanguage("", "de", "kk") != "kk" || domain.ResolveLanguage("", "", "") != "en" ||
		domain.ResolveLanguage("uz", "ru", "kk") != "uz" {
		t.Fatal("resolution order: preferred, device, account, en")
	}
}
