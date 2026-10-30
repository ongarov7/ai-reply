package apptest

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
)

const lifecycleTransaction = "apple-pack-lifecycle-transaction"

func applePackLifecycleFixture(t *testing.T, grant bool) (*harness, session, string) {
	t.Helper()
	h := newHarness(t, approvedEntitlements(), withIAP(t))
	h.enableApprovedProducts()
	s := h.signIn("apple-pack-lifecycle@example.com")
	signed := h.appleTx(s.userID, lifecycleTransaction, "test.pack30", nil)
	if grant {
		mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	}
	// Delivery happens after all the signed events below, in either order.
	h.clock.Advance(5 * time.Minute)
	return h, s, signed
}

func (h *harness) applePurchaseEvent(kind, id, transaction string, signedAt time.Time) response {
	h.t.Helper()
	payload := appstoretest.Notification{NotificationType: kind, NotificationUUID: id, Version: "2.0", SignedDate: signedAt.UnixMilli(),
		Data: appstoretest.NotificationData{BundleID: h.cfg.IAP.AppleBundleID, Environment: "Production", SignedTransactionInfo: transaction}}
	return h.do(http.MethodPost, "/api/v1/iap/apple/notifications", map[string]any{"signedPayload": h.apple.Sign(h.t, payload)}, nil)
}

func TestApplePackLifecycleIgnoresDelayedRefundAfterNewerReversal(t *testing.T) {
	h, s, signed := applePackLifecycleFixture(t, true)
	for i := 0; i < 3; i++ {
		mustStatus(t, h.generate(s.access, "Сәлем"), http.StatusOK, "")
	}
	older := h.clock.Now().Add(-4 * time.Minute)
	newer := older.Add(time.Minute)
	// The transaction snapshot can be re-signed later than the event itself.
	// It must not replace the outer notification's signed event ordering.
	refunded := h.appleTx(s.userID, lifecycleTransaction, "test.pack30", func(tx *appstoretest.Transaction) {
		tx.RevocationDate = older.UnixMilli()
		tx.SignedDate = h.clock.Now().Add(-time.Second).UnixMilli()
	})
	mustStatus(t, h.applePurchaseEvent("REFUND_REVERSED", "reversal-newer", signed, newer), http.StatusOK, "")
	mustStatus(t, h.applePurchaseEvent("REFUND", "refund-delayed", refunded, older), http.StatusOK, "")
	mustStatus(t, h.applePurchaseEvent("REFUND", "refund-delayed-duplicate", refunded, older), http.StatusOK, "")
	mustStatus(t, h.applePurchaseEvent("REFUND_REVERSED", "reversal-newer", signed, newer), http.StatusOK, "")
	if h.creditsLeft(s.access) != 27 || h.text(`SELECT status FROM iap_purchases WHERE transaction_id=?`, lifecycleTransaction) != "granted" ||
		h.scalar(`SELECT count(*) FROM credit_ledger WHERE entry='revoke'`) != 0 {
		t.Fatal("a delayed refund overrode the newer signed reversal")
	}
	// A genuinely newer refund still revokes only what remains, and its older
	// reversal cannot restore anything. A later verified reversal restores once.
	latest := newer.Add(time.Minute)
	mustStatus(t, h.applePurchaseEvent("REFUND", "refund-latest", refunded, latest), http.StatusOK, "")
	mustStatus(t, h.applePurchaseEvent("REFUND_REVERSED", "reversal-stale", signed, newer), http.StatusOK, "")
	if h.creditsLeft(s.access) != 0 {
		t.Fatal("a stale reversal restored refunded credits")
	}
	mustStatus(t, h.applePurchaseEvent("REFUND_REVERSED", "reversal-latest", signed, latest.Add(time.Minute)), http.StatusOK, "")
	mustStatus(t, h.applePurchaseEvent("REFUND_REVERSED", "reversal-latest-duplicate", signed, latest.Add(time.Minute)), http.StatusOK, "")
	if h.creditsLeft(s.access) != 27 || h.scalar(`SELECT count(*) FROM credit_ledger WHERE reason='refund_reversed'`) != 1 {
		t.Fatal("the latest reversal did not restore the exact remainder once")
	}
}

func TestApplePackLifecycleRetainsUnknownReversalBeforeVerification(t *testing.T) {
	for _, order := range []string{"reversal_first", "refund_first"} {
		t.Run(order, func(t *testing.T) {
			h, s, signed := applePackLifecycleFixture(t, false)
			older := h.clock.Now().Add(-3 * time.Minute)
			newer := older.Add(time.Minute)
			if order == "refund_first" {
				mustStatus(t, h.applePurchaseEvent("REFUND", "unknown-refund", signed, older), http.StatusOK, "")
			}
			mustStatus(t, h.applePurchaseEvent("REFUND_REVERSED", "unknown-reversal", signed, newer), http.StatusOK, "")
			mustStatus(t, h.applePurchaseEvent("REFUND", "unknown-delayed-refund", signed, older), http.StatusOK, "")
			if h.scalar(`SELECT count(*) FROM iap_purchases`) != 0 || h.scalar(`SELECT count(*) FROM credit_grants`) != 0 ||
				h.text(`SELECT state FROM apple_purchase_lifecycle WHERE transaction_id=?`, lifecycleTransaction) != "restored" {
				t.Fatal("unknown reversal was lost or granted units before verification")
			}
			other := h.signIn("apple-pack-lifecycle-other@example.com")
			mustStatus(t, h.postApple(other.access, signed), http.StatusBadRequest, "PURCHASE_INVALID")
			mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
			mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
			if h.creditsLeft(s.access) != 30 || h.creditsLeft(other.access) != 0 || h.scalar(`SELECT count(*) FROM credit_grants WHERE kind='pack'`) != 1 {
				t.Fatal("event ordering lost account binding or granted the pack twice")
			}
		})
	}
}

func TestApplePackLifecycleRevokedSnapshotBeforeVerificationBlocksOldPaidJWS(t *testing.T) {
	h, s, signed := applePackLifecycleFixture(t, false)
	at := h.clock.Now().Add(-2 * time.Minute)
	revoked := h.appleTx(s.userID, lifecycleTransaction, "test.pack30", func(tx *appstoretest.Transaction) {
		tx.RevocationDate, tx.SignedDate = at.UnixMilli(), at.UnixMilli()
	})
	mustStatus(t, h.postApple(s.access, revoked), http.StatusBadRequest, "PURCHASE_INVALID")
	mustStatus(t, h.postApple(s.access, signed), http.StatusBadRequest, "PURCHASE_INVALID")
	if h.creditsLeft(s.access) != 0 || h.scalar(`SELECT count(*) FROM iap_purchases`) != 0 {
		t.Fatal("an old paid JWS bypassed the newer verified revoked snapshot")
	}
	mustStatus(t, h.applePurchaseEvent("REFUND_REVERSED", "snapshot-reversal", signed, at.Add(time.Minute)), http.StatusOK, "")
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	mustStatus(t, h.postApple(s.access, revoked), http.StatusBadRequest, "PURCHASE_INVALID")
	if h.creditsLeft(s.access) != 30 {
		t.Fatal("an older revoked snapshot undid the verified reversal")
	}
}

func TestApplePackLifecycleEqualTimeCannotRestoreRevocation(t *testing.T) {
	for _, reversalFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("reversal_first_%t", reversalFirst), func(t *testing.T) {
			h, s, signed := applePackLifecycleFixture(t, true)
			at := h.clock.Now().Add(-time.Minute)
			if reversalFirst {
				mustStatus(t, h.applePurchaseEvent("REFUND_REVERSED", "equal-reversal-before", signed, at), http.StatusOK, "")
			}
			mustStatus(t, h.applePurchaseEvent("REFUND", "equal-refund", signed, at), http.StatusOK, "")
			mustStatus(t, h.applePurchaseEvent("REFUND_REVERSED", "equal-reversal-after", signed, at), http.StatusOK, "")
			if h.creditsLeft(s.access) != 0 || h.text(`SELECT state FROM apple_purchase_lifecycle WHERE transaction_id=?`, lifecycleTransaction) != "refunded" {
				t.Fatal("an equal-time reversal overrode a revocation")
			}
		})
	}
}

func TestApplePackLifecycleRollsBackStateWithFailedGrantMutation(t *testing.T) {
	h, s, signed := applePackLifecycleFixture(t, true)
	h.exec(`CREATE TRIGGER lifecycle_reject_revoke BEFORE INSERT ON credit_ledger WHEN NEW.entry='revoke' BEGIN SELECT RAISE(ABORT,'fixture rejection'); END`)
	at := h.clock.Now().Add(-time.Minute)
	mustStatus(t, h.applePurchaseEvent("REFUND", "atomic-refund", signed, at), http.StatusInternalServerError, "INTERNAL_ERROR")
	if h.scalar(`SELECT count(*) FROM apple_purchase_lifecycle`) != 0 || h.scalar(`SELECT count(*) FROM iap_notifications`) != 0 || h.creditsLeft(s.access) != 30 {
		t.Fatal("failed revocation committed its lifecycle state or changed credits")
	}
	h.exec(`DROP TRIGGER lifecycle_reject_revoke`)
	mustStatus(t, h.applePurchaseEvent("REFUND", "atomic-refund", signed, at), http.StatusOK, "")
	if h.creditsLeft(s.access) != 0 || h.text(`SELECT state FROM apple_purchase_lifecycle WHERE transaction_id=?`, lifecycleTransaction) != "refunded" {
		t.Fatal("the retried event was skipped after its failed transaction")
	}
	h.exec(`CREATE TRIGGER lifecycle_reject_restore BEFORE INSERT ON credit_ledger WHEN NEW.reason='refund_reversed' BEGIN SELECT RAISE(ABORT,'fixture rejection'); END`)
	mustStatus(t, h.applePurchaseEvent("REFUND_REVERSED", "atomic-reversal", signed, at.Add(time.Second)), http.StatusInternalServerError, "INTERNAL_ERROR")
	if h.text(`SELECT state FROM apple_purchase_lifecycle WHERE transaction_id=?`, lifecycleTransaction) != "refunded" || h.creditsLeft(s.access) != 0 {
		t.Fatal("failed restoration committed its lifecycle or granted credits")
	}
	h.exec(`DROP TRIGGER lifecycle_reject_restore`)
	mustStatus(t, h.applePurchaseEvent("REFUND_REVERSED", "atomic-reversal", signed, at.Add(time.Second)), http.StatusOK, "")
	if h.creditsLeft(s.access) != 30 {
		t.Fatal("restoration could not retry after its rolled-back transaction")
	}
}

func TestApplePackLifecycleConcurrentEventsConvergeBeforeAndAfterVerification(t *testing.T) {
	for _, granted := range []bool{false, true} {
		t.Run(fmt.Sprintf("already_granted_%t", granted), func(t *testing.T) {
			h, s, signed := applePackLifecycleFixture(t, granted)
			older := h.clock.Now().Add(-3 * time.Minute)
			var payloads []string
			for index, kind := range []string{"REFUND", "REFUND_REVERSED"} {
				payloads = append(payloads, h.apple.Sign(t, appstoretest.Notification{NotificationType: kind, NotificationUUID: fmt.Sprintf("concurrent-%d", index),
					Version: "2.0", SignedDate: older.Add(time.Duration(index) * time.Minute).UnixMilli(),
					Data: appstoretest.NotificationData{BundleID: h.cfg.IAP.AppleBundleID, Environment: "Production", SignedTransactionInfo: signed}}))
			}
			start, results := make(chan struct{}), make(chan response, len(payloads))
			for _, payload := range payloads {
				go func(payload string) {
					<-start
					results <- h.do(http.MethodPost, "/api/v1/iap/apple/notifications", map[string]any{"signedPayload": payload}, nil)
				}(payload)
			}
			close(start)
			for range payloads {
				mustStatus(t, <-results, http.StatusOK, "")
			}
			mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
			if h.creditsLeft(s.access) != 30 || h.scalar(`SELECT count(*) FROM credit_grants WHERE kind='pack'`) != 1 ||
				h.text(`SELECT state FROM apple_purchase_lifecycle WHERE transaction_id=?`, lifecycleTransaction) != "restored" {
				t.Fatal("concurrent delivery lost the newest state or duplicated the purchase")
			}
		})
	}
}
