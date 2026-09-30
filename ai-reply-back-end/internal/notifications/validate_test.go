package notifications

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func TestValidateLinkAllowsOnlyKnownScreensAndHosts(t *testing.T) {
	hosts := []string{"ai-reply.kz"}
	ok := map[string]string{
		"":                                "",
		"aireply://subscription":          "aireply://subscription",
		"AIREPLY://Settings/":             "aireply://settings",
		"https://ai-reply.kz/offer":       "https://ai-reply.kz/offer",
		"https://help.ai-reply.kz/a?b=c":  "https://help.ai-reply.kz/a?b=c",
		"https://AI-REPLY.kz/privacy#top": "https://ai-reply.kz/privacy#top",
	}
	for in, want := range ok {
		got, err := ValidateLink(in, hosts)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"aireply://wallet", "aireply://settings/x", "aireply://settings?x=1", "aireply://user@settings",
		"http://ai-reply.kz/offer", "https://evil.kz/ai-reply.kz", "https://ai-reply.kz.evil.com/",
		"https://user:pass@ai-reply.kz/", "https://ai-reply.kz:8443/", "javascript:alert(1)",
		"intent://scan/#Intent;scheme=zxing;end", "file:///etc/passwd", "tel:+77011234567",
		"https://ai-reply.kz/\nSet-Cookie:x", "//ai-reply.kz/offer", strings.Repeat("a", 600),
	} {
		if _, err := ValidateLink(bad, hosts); err == nil || !errors.Is(err, domain.ErrInvalidRequest) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestValidateAudienceRejectsUnknownAndContradictoryFilters(t *testing.T) {
	good, err := ValidateAudience(domain.AudienceFilter{
		Platforms: []string{"iOS", "android", "ios"}, Payment: "paid", Locales: []string{"kk", "RU"},
		AppVersionMin: "1.3.0", AppVersionMax: "2.0", ActiveWithinDays: 30,
		RegisteredFrom: "2026-01-01", RegisteredTo: "2026-02-01",
		UserIDs: []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000001"},
	})
	if err != nil {
		t.Fatalf("valid filter refused: %v", err)
	}
	if len(good.Platforms) != 2 || good.Platforms[0] != "android" || len(good.UserIDs) != 1 {
		t.Fatalf("normalised = %+v", good)
	}
	for name, f := range map[string]domain.AudienceFilter{
		"platform":       {Platforms: []string{"web"}},
		"locale":         {Locales: []string{"de"}},
		"auth":           {Auth: "admins"},
		"payment":        {Payment: "rich"},
		"subscription":   {Subscription: "gold"},
		"version":        {AppVersionMin: "latest"},
		"version order":  {AppVersionMin: "2.0.0", AppVersionMax: "1.0.0"},
		"days":           {ActiveWithinDays: -1},
		"never matches":  {ActiveWithinDays: 7, InactiveForDays: 30},
		"date":           {RegisteredFrom: "01.02.2026"},
		"date order":     {RegisteredFrom: "2026-03-01", RegisteredTo: "2026-02-01"},
		"user id":        {UserIDs: []string{"'; DROP TABLE users; --"}},
		"anonymous paid": {Auth: "anonymous", Payment: "paid"},
	} {
		if _, err := ValidateAudience(f); err == nil {
			t.Errorf("%s: accepted %+v", name, f)
		}
	}
}

func TestContentValidation(t *testing.T) {
	svc := New(Deps{Config: config.Push{LinkHosts: []string{"ai-reply.kz"}}})
	good, err := svc.validateContent(Content{Title: " Тариф ", Body: "Жол 1\r\nЖол 2", Data: map[string]string{"promo_code": "AUTUMN"}})
	if err != nil {
		t.Fatalf("valid content refused: %v", err)
	}
	if good.Title != "Тариф" || good.Body != "Жол 1\nЖол 2" || good.Category != domain.CategorySystem {
		t.Fatalf("normalised = %+v", good)
	}
	campaign, _ := svc.validateContent(Content{Title: "t", Body: "b", Campaign: true})
	if campaign.Category != domain.CategoryMarketing {
		t.Fatalf("campaigns default to marketing, got %q", campaign.Category)
	}
	for name, c := range map[string]Content{
		"empty title":   {Body: "b"},
		"long title":    {Title: strings.Repeat("т", 81), Body: "b"},
		"newline title": {Title: "a\nb", Body: "b"},
		"empty body":    {Title: "t"},
		"long body":     {Title: "t", Body: strings.Repeat("б", 401)},
		"control body":  {Title: "t", Body: "a\x00b"},
		"category":      {Title: "t", Body: "b", Category: "urgent"},
		"reserved key":  {Title: "t", Body: "b", Data: map[string]string{"nid": "x"}},
		"aps key":       {Title: "t", Body: "b", Data: map[string]string{"aps": "x"}},
		"uppercase key": {Title: "t", Body: "b", Data: map[string]string{"Promo": "x"}},
		"google prefix": {Title: "t", Body: "b", Data: map[string]string{"google.c.a": "x"}},
		"long value":    {Title: "t", Body: "b", Data: map[string]string{"k": strings.Repeat("v", 257)}},
		"too many keys": {Title: "t", Body: "b", Data: map[string]string{"a": "1", "b": "1", "c": "1", "d": "1", "e": "1", "f": "1", "g": "1", "h": "1", "i": "1", "j": "1", "k": "1"}},
		"bad link":      {Title: "t", Body: "b", Link: "https://evil.example/"},
	} {
		if _, err := svc.validateContent(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestBackoffScheduleWithJitter(t *testing.T) {
	cases := []struct {
		attempt int
		base    time.Duration
	}{{1, 5 * time.Second}, {2, 30 * time.Second}, {3, 2 * time.Minute}, {4, 10 * time.Minute}, {9, 30 * time.Minute}}
	for _, c := range cases {
		low, high := Backoff(c.attempt, 0, 0), Backoff(c.attempt, 0, 0.999999)
		if low != time.Duration(float64(c.base)*0.8) || high < c.base || high > time.Duration(float64(c.base)*1.2) {
			t.Errorf("attempt %d: [%v, %v] around %v", c.attempt, low, high, c.base)
		}
	}
	if got := Backoff(1, 2*time.Minute, 0.5); got != 2*time.Minute {
		t.Errorf("Retry-After longer than the schedule must win: %v", got)
	}
	if got := Backoff(1, 10*time.Hour, 0.5); got != time.Hour {
		t.Errorf("waits are capped at an hour: %v", got)
	}
}

func TestPreferencesKeepSecurityOn(t *testing.T) {
	for _, c := range domain.NotificationCategories {
		if c == domain.CategorySecurity && domain.CategoryOptional(c) {
			t.Fatal("security notifications must not be optional")
		}
	}
}
