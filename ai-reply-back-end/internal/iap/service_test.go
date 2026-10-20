package iap

import (
	"regexp"
	"strings"
	"testing"

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
}
