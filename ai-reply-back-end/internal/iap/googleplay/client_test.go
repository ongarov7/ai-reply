package googleplay_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/iap/googleplay"
	"github.com/aireply/ai-reply-back-end/internal/iap/googleplay/playtest"
)

const token = "kdjfhgkjdhfgkjhdfg.AO-J1OzX9hVvF4Y3m8sQ_wTt2Lr0nKp5aB7cD9eF1gH3iJ5kL7mN9oP1qR3sT5uV7wX9yZ1aB3cD5eF7"

func client(t *testing.T, g *playtest.Server) *googleplay.Client {
	t.Helper()
	c, err := googleplay.New(g.Config(t))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Сатып алу → тұтыну: токен бір рет алынады, күй Google-дағыдай өзгереді.
func TestGetAndConsume(t *testing.T) {
	g := playtest.New(t, "kz.yerek.aireply")
	g.Put("pack_50", token, googleplay.ProductPurchase{PurchaseTimeMillis: "1760950800000", OrderID: "GPA.3300-1111-2222-33333",
		ObfuscatedExternalAccountID: "acc", Quantity: 2})
	c := client(t, g)
	p, err := c.GetProductPurchase(context.Background(), "pack_50", token)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p.PurchaseState != googleplay.StatePurchased || p.ConsumptionState != googleplay.NotConsumed || p.Units() != 2 ||
		p.OrderID != "GPA.3300-1111-2222-33333" || p.PurchasedAt() == nil || !p.PurchasedAt().Equal(time.UnixMilli(1760950800000)) {
		t.Fatalf("purchase %+v", p)
	}
	if err := c.Consume(context.Background(), "pack_50", token); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got := g.Purchase("pack_50", token); got.ConsumptionState != googleplay.Consumed {
		t.Fatalf("not consumed: %+v", got)
	}
	if g.TokenCalls.Load() != 1 {
		t.Fatalf("token calls %d: the access token is cached", g.TokenCalls.Load())
	}
}

func TestErrorsAreClassifiedWithoutTheToken(t *testing.T) {
	g := playtest.New(t, "kz.yerek.aireply")
	c := client(t, g)
	_, err := c.GetProductPurchase(context.Background(), "pack_50", token)
	var apiErr *googleplay.APIError
	if !errors.Is(err, googleplay.ErrNotFound) || !errors.As(err, &apiErr) || apiErr.Status != 404 || !apiErr.Permanent() {
		t.Fatalf("unknown token: %v", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatal("the error names the purchase token")
	}
	g.Put("pack_50", token, googleplay.ProductPurchase{})
	g.FailNext(1)
	_, err = c.GetProductPurchase(context.Background(), "pack_50", token)
	if !errors.Is(err, googleplay.ErrUnavailable) || !errors.As(err, &apiErr) || apiErr.Permanent() {
		t.Fatalf("503: %v", err)
	}
	if _, err := c.GetProductPurchase(context.Background(), "pack_50", token); err != nil {
		t.Fatalf("after the outage: %v", err)
	}
	// A pending purchase cannot be consumed.
	g.Update("pack_50", token, func(p *googleplay.ProductPurchase) { p.PurchaseState = googleplay.StatePending })
	if err := c.Consume(context.Background(), "pack_50", token); !errors.As(err, &apiErr) || !apiErr.Permanent() {
		t.Fatalf("consume pending: %v", err)
	}

	g.Close()
	_, err = c.GetProductPurchase(context.Background(), "pack_50", token)
	if !errors.Is(err, googleplay.ErrUnavailable) || strings.Contains(err.Error(), token) {
		t.Fatalf("closed server: %v", err)
	}
}

func TestVoidedPurchasesArePaged(t *testing.T) {
	g := playtest.New(t, "kz.yerek.aireply")
	g.PageSize(2)
	for _, tok := range []string{"t-1", "t-2", "t-3"} {
		g.Void(googleplay.VoidedPurchase{PurchaseToken: tok, VoidedTimeMillis: "1760950800000", OrderID: "GPA." + tok})
	}
	c := client(t, g)
	since := time.UnixMilli(1760000000000)
	first, err := c.VoidedPurchases(context.Background(), since, "")
	if err != nil || len(first.Purchases) != 2 || first.Next == "" {
		t.Fatalf("first page %+v %v", first, err)
	}
	if g.LastVoidedSince() != "1760000000000" {
		t.Fatalf("startTime %q", g.LastVoidedSince())
	}
	second, err := c.VoidedPurchases(context.Background(), since, first.Next)
	if err != nil || len(second.Purchases) != 1 || second.Next != "" || second.Purchases[0].PurchaseToken != "t-3" {
		t.Fatalf("second page %+v %v", second, err)
	}
	if !second.Purchases[0].VoidedAt().Equal(time.UnixMilli(1760950800000)) {
		t.Fatal("voided time")
	}
}

func TestNewRefusesBadKeysWithoutEchoingThem(t *testing.T) {
	_, err := googleplay.New(googleplay.Config{PackageName: "p", ClientEmail: "a@b",
		PrivateKey: "-----BEGIN PRIVATE KEY-----\nc2VjcmV0\n-----END PRIVATE KEY-----\n"})
	if err == nil || strings.Contains(err.Error(), "c2VjcmV0") {
		t.Fatalf("err = %v", err)
	}
	if _, err := googleplay.New(googleplay.Config{ClientEmail: "a@b"}); err == nil {
		t.Fatal("no package name accepted")
	}
}
