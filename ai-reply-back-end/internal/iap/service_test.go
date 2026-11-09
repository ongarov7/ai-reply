package iap

import (
	"regexp"
	"strings"
	"testing"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func newTestService(t *testing.T, secret string) *Service {
	t.Helper()
	s, err := New(Deps{Secret: secret})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Тіркелгі токені тұрақты, тіркелгіге және құпияға тәуелді, v4 UUID пішінінде.
func TestAccountTokenIsAStableUUIDPerAccount(t *testing.T) {
	a := newTestService(t, strings.Repeat("a", 40))
	b := newTestService(t, strings.Repeat("b", 40))
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	first := a.AccountToken("user-1")
	if !uuid.MatchString(first) || first != a.AccountToken("user-1") {
		t.Fatalf("token %q", first)
	}
	if first == a.AccountToken("user-2") || first == b.AccountToken("user-1") {
		t.Fatal("tokens collide across accounts or secrets")
	}
	if _, err := New(Deps{Secret: "short"}); err == nil {
		t.Fatal("a short secret accepted")
	}
}

func TestSealedTokensOpenOnlyWithTheirKey(t *testing.T) {
	a := newTestService(t, strings.Repeat("a", 40))
	b := newTestService(t, strings.Repeat("b", 40))
	sealed, err := a.sealer.seal("purchase-token-1")
	if err != nil || strings.Contains(sealed, "purchase-token-1") || !strings.HasPrefix(sealed, "v1:") {
		t.Fatalf("sealed %q %v", sealed, err)
	}
	if plain, err := a.sealer.open(sealed); err != nil || plain != "purchase-token-1" {
		t.Fatalf("open %q %v", plain, err)
	}
	if _, err := b.sealer.open(sealed); err == nil {
		t.Fatal("another key opened the token")
	}
	if _, err := a.sealer.open(sealed[:len(sealed)-2] + "AA"); err == nil {
		t.Fatal("a tampered token opened")
	}
}

func TestSubscriptionTokensSurviveARotationOfTheAccessSecret(t *testing.T) {
	stable := strings.Repeat("s", 40)
	first, err := New(Deps{Secret: strings.Repeat("a", 40), Config: config.IAP{GoogleTokenSecret: stable}})
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := New(Deps{Secret: strings.Repeat("b", 40), Config: config.IAP{GoogleTokenSecret: stable}})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := first.sealer.seal("purchase-token-1")
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := rotated.sealer.open(sealed); err != nil || plain != "purchase-token-1" {
		t.Fatalf("after a rotation: %q %v", plain, err)
	}

	// What was sealed under the access secret before the stable one was set stays readable.
	before := newTestService(t, strings.Repeat("a", 40))
	old, err := before.sealer.seal("purchase-token-2")
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := first.sealer.open(old); err != nil || plain != "purchase-token-2" {
		t.Fatalf("an older token: %q %v", plain, err)
	}
	// New tokens do not depend on the access secret.
	if _, err := before.sealer.open(sealed); err == nil {
		t.Fatal("a token sealed with the stable secret opened without it")
	}
}

func TestProductRules(t *testing.T) {
	ok := ProductInput{Code: "pass_24h", Kind: "pass", Units: 40, DurationHours: 24, GoogleProductID: "pass_24h",
		IsActive: true, Name: map[string]string{"ru": "  24  часа\n"}}
	p, err := validate(ok)
	if err != nil || p.Name["ru"] != "24 часа" || p.Name["kk"] != "" {
		t.Fatalf("valid: %+v %v", p, err)
	}
	inactive := ok
	inactive.GoogleProductID, inactive.IsActive = "", false
	if _, err := validate(inactive); err != nil {
		t.Fatalf("an inactive product without store ids is a draft: %v", err)
	}
	if !(domain.IAPProduct{IsActive: true, Units: 1, AppleProductID: "x"}).Offered(domain.PlatformIOS) ||
		(domain.IAPProduct{IsActive: true, Units: 1, AppleProductID: "x"}).Offered(domain.PlatformAndroid) {
		t.Fatal("Offered must follow the platform's store id")
	}
	sub := ProductInput{Code: "base_x", Kind: "subscription", PlanID: "plan", BillingPeriod: "month", GoogleProductID: "kz.aireply.base.x", IsActive: true}
	if _, err := validate(sub); err != nil {
		t.Fatalf("a subscription may carry a Google id: %v", err)
	}
	both := domain.IAPProduct{IsActive: true, Kind: domain.ProductSubscription, PlanID: "plan", BillingPeriod: "month", AppleProductID: "a", GoogleProductID: "g"}
	if !both.Offered(domain.PlatformIOS) || !both.Offered(domain.PlatformAndroid) {
		t.Fatal("a subscription is offered on both stores")
	}
	androidOff, iosOff := both, both
	androidOff.OffAndroid, iosOff.OffIOS = true, true
	if !androidOff.Offered(domain.PlatformIOS) || androidOff.Offered(domain.PlatformAndroid) ||
		iosOff.Offered(domain.PlatformIOS) || !iosOff.Offered(domain.PlatformAndroid) {
		t.Fatal("each store has its own switch")
	}
}
