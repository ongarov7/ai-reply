package bcc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/legal"
)

func currentQuote(t *testing.T, s *CheckoutService) ExpectedQuote {
	t.Helper()
	p, version, err := s.catalog.ProductForWeb(context.Background(), "pack30")
	if err != nil {
		t.Fatal(err)
	}
	return ExpectedQuote{CatalogVersion: version, PriceMinor: p.PriceMinor, Units: p.Units, Currency: p.Currency, TermsVersion: legal.WebPurchaseTermsVersion}
}

func TestExpectedQuoteAndPurchaseConsentCommitOnceAndReplayOriginal(t *testing.T) {
	s, prices := testCheckout(t)
	ctx := context.Background()
	quote := currentQuote(t, s)
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, err := s.StartExpected(ctx, "bank-user", "pack30", "accepted-offer", quote)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- o.ID
		}()
	}
	wg.Wait()
	close(ids)
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("concurrent confirmation created another order")
		}
		id = got
	}
	var count int
	var version, price int64
	var units int
	var terms, source, user string
	if err := s.db.Reader().QueryRow(`SELECT count(*),catalog_version,price_minor,units,terms_version,source,user_id FROM billing_web_purchase_consents WHERE order_id=?`, id).Scan(&count, &version, &price, &units, &terms, &source, &user); err != nil || count != 1 || version != quote.CatalogVersion || price != quote.PriceMinor || units != quote.Units || terms != legal.WebPurchaseTermsVersion || source != "website" || user != "bank-user" {
		t.Fatalf("accepted contract was not stored once: %v", err)
	}
	products := catalog.InitialProducts()
	products[5].PriceMinor += 5000
	draft, err := prices.SaveDraft(ctx, "operator", 0, products)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = prices.Publish(ctx, "operator", draft.ID, draft.Revision); err != nil {
		t.Fatal(err)
	}
	replayed, err := s.StartExpected(ctx, "bank-user", "pack30", "accepted-offer", ExpectedQuote{})
	if err != nil || replayed.ID != id || replayed.AmountMinor != quote.PriceMinor {
		t.Fatal("retry replaced the original accepted contract")
	}
	if _, err = s.StartExpected(ctx, "bank-user", "pack30", "different-attempt", quote); err == nil {
		t.Fatal("stale quote created a new order")
	}
}

func TestInvalidQuoteOrMissingConsentCreatesNoOrder(t *testing.T) {
	for name, change := range map[string]func(*ExpectedQuote){
		"catalog":       func(q *ExpectedQuote) { q.CatalogVersion++ },
		"price":         func(q *ExpectedQuote) { q.PriceMinor++ },
		"units":         func(q *ExpectedQuote) { q.Units++ },
		"currency":      func(q *ExpectedQuote) { q.Currency = "USD" },
		"missing_terms": func(q *ExpectedQuote) { q.TermsVersion = "" },
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := testCheckout(t)
			q := currentQuote(t, s)
			change(&q)
			if _, err := s.StartExpected(context.Background(), "bank-user", "pack30", "invalid-offer", q); err == nil {
				t.Fatal("unaccepted offer created an order")
			}
			var orders, consents int
			if err := s.db.Reader().QueryRow(`SELECT (SELECT count(*) FROM billing_orders),(SELECT count(*) FROM billing_web_purchase_consents)`).Scan(&orders, &consents); err != nil || orders != 0 || consents != 0 {
				t.Fatal("rejected offer left payment or consent records")
			}
		})
	}
	s, _ := testCheckout(t)
	if _, err := s.db.Writer().Exec(`CREATE TRIGGER reject_test_consent BEFORE INSERT ON billing_web_purchase_consents BEGIN SELECT RAISE(ABORT,'fixture consent failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartExpected(context.Background(), "bank-user", "pack30", "rollback-offer", currentQuote(t, s)); err == nil {
		t.Fatal("order committed without its consent")
	}
	var count int
	if err := s.db.Reader().QueryRow(`SELECT count(*) FROM billing_orders`).Scan(&count); err != nil || count != 0 {
		t.Fatal("consent failure did not roll back the bank order")
	}
}

func TestPublicationBeforeCheckoutLockInvalidatesTheOldQuote(t *testing.T) {
	s, prices := testCheckout(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	quote := currentQuote(t, s)
	products := catalog.InitialProducts()
	products[5].PriceMinor += 5000
	draft, err := prices.SaveDraft(ctx, "operator", 0, products)
	if err != nil {
		t.Fatal(err)
	}
	// Hold the same writer lock used by catalog publication. An implementation
	// that reads the offer before this lock would retain the stale price.
	tx, err := s.db.Writer().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	waits := s.db.Writer().Stats().WaitCount
	result := make(chan error, 1)
	go func() {
		_, err := s.StartExpected(ctx, "bank-user", "pack30", "publication-race", quote)
		result <- err
	}()
	deadline := time.Now().Add(time.Second)
	for s.db.Writer().Stats().WaitCount == waits && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.db.Writer().Stats().WaitCount == waits {
		t.Fatal("checkout did not reach the held writer lock")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE billing_catalog_versions SET state='superseded' WHERE state='published'`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE billing_catalog_versions SET state='published' WHERE id=?`, draft.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-result; err == nil {
		t.Fatal("checkout used an offer superseded before its transaction")
	}
	var count int
	if err = s.db.Reader().QueryRow(`SELECT count(*) FROM billing_orders`).Scan(&count); err != nil || count != 0 {
		t.Fatal("stale quote committed a bank order")
	}
}
