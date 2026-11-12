package apptest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
	"github.com/aireply/ai-reply-back-end/internal/legal"
	"github.com/aireply/ai-reply-back-end/internal/payments/bcc"
)

func TestAppleAndWebsiteRefundsKeepTheOtherProvidersPaidAccess(t *testing.T) {
	for _, refundApple := range []bool{true, false} {
		name := "website_cancel_and_refund_preserve_apple"
		if refundApple {
			name = "apple_refund_preserves_website"
		}
		t.Run(name, func(t *testing.T) {
			h := monthlyBaseHarness(t)
			// BCC uses wall time; keep the API's test clock just after its paid
			// period starts, without waiting or changing production clocks.
			h.clock.T = time.Now().UTC().Add(time.Minute)
			s := h.signIn("same-provider-account@example.com")
			ctx := context.Background()
			prices := catalog.New(h.db)
			if err := prices.EnsureInitial(ctx); err != nil {
				t.Fatal(err)
			}
			bankConfig := bcc.CheckoutConfig{
				Enabled: true, RecurringEnabled: true, Environment: bcc.Sandbox, AppEnv: "test",
				PublicBaseURL: "https://billing.example.invalid", Merchant: "00000001", MerchantName: "TEST MERCHANT",
				Terminal: "88888881", MACKeyHex: "6BB0AC02E47BDF73D98FEB777F3B5294",
				NotifyUsername: "fixture-bank", NotifyPassword: strings.Repeat("n", 40), StateSecret: strings.Repeat("s", 40),
			}
			bank, err := bcc.NewCheckout(bankConfig, h.db, prices)
			if err != nil {
				t.Fatal(err)
			}
			product, version, err := prices.ProductForWeb(ctx, "pro_monthly")
			if err != nil || !product.Unlimited {
				t.Fatalf("website Pro offer: %+v %v", product, err)
			}
			order, err := bank.StartExpected(ctx, s.userID, product.Code, "mixed-provider-pro-purchase", bcc.ExpectedQuote{
				CatalogVersion: version, PriceMinor: product.PriceMinor, Units: product.Units, Currency: product.Currency,
				TermsVersion: legal.WebPurchaseTermsVersion, Locale: "en", RecurringConsent: true,
				RecurringTermsVersion: bcc.RecurringTermsVersion, RecurringUntil: time.Now().UTC().AddDate(2, 0, 0).Format("20060102"),
			})
			if err != nil {
				t.Fatal(err)
			}
			subscriptionID := h.text(`SELECT bcc_subscription_id FROM billing_orders WHERE id=?`, order.ID)
			paidBankFields := func(orderID string) url.Values {
				t.Helper()
				raw := h.text(`SELECT request_json FROM billing_orders WHERE id=?`, orderID)
				var values url.Values
				if err := json.Unmarshal([]byte(raw), &values); err != nil {
					t.Fatal(err)
				}
				values.Set("ACTION", "0")
				values.Set("RC", "00")
				values.Set("RRN", "123456789012")
				values.Set("INT_REF", "ABCDEF0123456789")
				return values
			}
			fields := paidBankFields(order.ID)
			bankNotify := func(fields url.Values) {
				t.Helper()
				r := httptest.NewRequest(http.MethodPost, "/api/v1/payments/bcc/notify", strings.NewReader(fields.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.SetBasicAuth(bankConfig.NotifyUsername, bankConfig.NotifyPassword)
				w := httptest.NewRecorder()
				bank.Notify(w, r)
				if w.Code != http.StatusOK {
					t.Fatalf("bank callback: %d %s", w.Code, w.Body.String())
				}
			}
			bankNotify(fields)
			bankPack, err := bank.Start(ctx, s.userID, "pack30", "mixed-provider-pack-purchase")
			if err != nil {
				t.Fatal(err)
			}
			packFields := paidBankFields(bankPack.ID)
			bankNotify(packFields)
			start, end := h.clock.Now(), h.clock.Now().AddDate(0, 1, 0)
			applePro := func(refunded bool) string {
				return h.subscriptionTx(s.userID, "mixed-apple-pro", "mixed-apple-chain", "pro_monthly", start, end,
					func(tx *appstoretest.Transaction) {
						tx.Price = 3990000
						if refunded {
							tx.RevocationDate = h.clock.Now().UnixMilli()
						}
					})
			}
			applePack := func(refunded bool) string {
				return h.appleTx(s.userID, "mixed-apple-pack", "test.pack30", func(tx *appstoretest.Transaction) {
					tx.PurchaseDate, tx.Price = start.UnixMilli(), 130000
					if refunded {
						tx.RevocationDate = h.clock.Now().UnixMilli()
					}
				})
			}
			mustStatus(t, h.postApple(s.access, applePro(false)), http.StatusOK, "")
			mustStatus(t, h.postApple(s.access, applePack(false)), http.StatusOK, "")
			if ent := h.entitlement(s.userID); !ent.Unlimited || ent.Balance.Packs != 60 {
				t.Fatalf("both paid providers did not reach the same account: %+v", ent)
			}
			if refundApple {
				beforeOrder := h.text(`SELECT snapshot_json||'|'||status FROM billing_orders WHERE id=?`, order.ID)
				beforePack := h.text(`SELECT snapshot_json||'|'||status FROM billing_orders WHERE id=?`, bankPack.ID)
				beforePeriod := h.text(`SELECT starts_at||'|'||expires_at||'|'||coalesce(revoked_at,'') FROM bcc_subscription_periods WHERE order_id=?`, order.ID)
				h.clock.Advance(time.Second)
				for i := 0; i < 2; i++ {
					mustStatus(t, h.appleNotification("REFUND", "mixed-pro-refund", applePro(true)), http.StatusOK, "")
					mustStatus(t, h.appleNotification("REFUND", "mixed-pack-refund", applePack(true)), http.StatusOK, "")
				}
				ent := h.entitlement(s.userID)
				if !ent.Unlimited || ent.Plan.Code != product.Code || ent.Subscription.Source != "bcc" || ent.Balance.Packs != 30 ||
					h.scalar(`SELECT units_remaining FROM credit_grants WHERE billing_order_id=? AND status='active'`, bankPack.ID) != 30 ||
					h.text(`SELECT snapshot_json||'|'||status FROM billing_orders WHERE id=?`, order.ID) != beforeOrder ||
					h.text(`SELECT snapshot_json||'|'||status FROM billing_orders WHERE id=?`, bankPack.ID) != beforePack ||
					h.text(`SELECT starts_at||'|'||expires_at||'|'||coalesce(revoked_at,'') FROM bcc_subscription_periods WHERE order_id=?`, order.ID) != beforePeriod {
					t.Fatalf("Apple refund altered the independently paid website bundle/access: %+v", ent)
				}
				return
			}
			appleBefore := h.text(`SELECT transaction_id||'|'||expires_at||'|'||auto_renew||'|'||coalesce(revoked_at,'') FROM apple_subscriptions WHERE user_id=?`, s.userID)
			if err := bank.CancelSubscription(ctx, s.userID, subscriptionID); err != nil {
				t.Fatal(err)
			}
			if ent := h.entitlement(s.userID); !ent.Unlimited || ent.Balance.Packs != 60 {
				t.Fatalf("bank cancellation removed paid access or credits: %+v", ent)
			}
			// Register the refund operation only in the disposable fixture. Sending
			// a money request is outside this regression; its final authenticated
			// callback still runs the real refund handler and transaction.
			for _, refund := range []struct {
				orderID string
				fields  url.Values
			}{{order.ID, fields}, {bankPack.ID, packFields}} {
				refund.fields.Set("TRTYPE", "14")
				refund.fields.Set("NONCE", "00112233445566778899AABBCCDDEEFF")
				if refund.orderID == order.ID {
					refund.fields.Set("RECUR_REF", refund.fields.Get("RRN"))
				}
				refundJSON, err := json.Marshal(refund.fields)
				if err != nil {
					t.Fatal(err)
				}
				h.exec(`INSERT INTO billing_bank_operations(id,order_id,kind,status,request_json,created_by,created_at,updated_at) VALUES(?,?,'refund','pending',?,'fixture-admin',?,?)`,
					"mixed-refund-"+refund.orderID, refund.orderID, string(refundJSON), start.UnixMilli(), start.UnixMilli())
				bankNotify(refund.fields)
				bankNotify(refund.fields)
			}
			ent := h.entitlement(s.userID)
			if !ent.Unlimited || ent.Subscription.Source != domain.ProviderAppStore || ent.Balance.Packs != 30 ||
				h.scalar(`SELECT count(*) FROM iap_purchases WHERE user_id=? AND provider='app_store' AND status='granted'`, s.userID) != 2 ||
				h.scalar(`SELECT sum(units_remaining) FROM credit_grants WHERE user_id=? AND iap_purchase_id IS NOT NULL AND status='active'`, s.userID) != 30 ||
				h.text(`SELECT transaction_id||'|'||expires_at||'|'||auto_renew||'|'||coalesce(revoked_at,'') FROM apple_subscriptions WHERE user_id=?`, s.userID) != appleBefore ||
				h.scalar(`SELECT count(*) FROM billing_orders WHERE user_id=? AND status='refunded'`, s.userID) != 2 {
				t.Fatalf("bank cancellation/refund changed Apple purchases or paid access: %+v", ent)
			}
		})
	}
}
