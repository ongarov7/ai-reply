package apptest

import (
	"net/http"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/iap/appstore"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
)

func TestAppReviewSandboxSubscriptionNotificationsFollowAllowedAccount(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t), withEnv("APPLE_IAP_ACCEPT_SANDBOX", "false"),
		withEnv("APPLE_IAP_SANDBOX_ACCOUNTS", "app.review@example.com"))
	h.enableApprovedProducts()
	reviewer := h.signIn("app.review@example.com")
	start := h.clock.Now()
	end := start.Add(time.Hour)
	sandbox := func(tx *appstoretest.Transaction) { tx.Environment = appstore.EnvironmentSandbox }
	first := h.subscriptionTx(reviewer.userID, "review-first", "review-original", "base_monthly", start, end, sandbox)
	mustStatus(t, h.postApple(reviewer.access, first), http.StatusOK, "")

	notify := func(kind, id, transaction string, autoRenew *int) {
		data := appstoretest.NotificationData{BundleID: h.cfg.IAP.AppleBundleID,
			Environment: appstore.EnvironmentSandbox, SignedTransactionInfo: transaction}
		if autoRenew != nil {
			data.SignedRenewalInfo = h.apple.Sign(t, appstore.Renewal{OriginalTransactionID: "review-original",
				AutoRenewStatus: *autoRenew, Environment: appstore.EnvironmentSandbox, SignedDate: h.clock.Now().UnixMilli()})
		}
		payload := appstoretest.Notification{NotificationType: kind, NotificationUUID: id, Version: "2.0",
			SignedDate: h.clock.Now().UnixMilli(), Data: data}
		mustStatus(t, h.do(http.MethodPost, "/api/v1/iap/apple/notifications",
			map[string]any{"signedPayload": h.apple.Sign(t, payload)}, nil), http.StatusOK, "")
	}

	h.clock.Advance(time.Minute)
	renewedEnd := end.Add(time.Hour)
	renewed := h.subscriptionTx(reviewer.userID, "review-renewed", "review-original", "base_monthly", end, renewedEnd, sandbox)
	notify("DID_RENEW", "review-renewal", renewed, nil)
	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(reviewer.access))
	if me.str("subscription", "expires_at") != renewedEnd.Format(time.RFC3339) {
		t.Fatalf("review renewal was ignored: %s", me.raw)
	}

	h.clock.Advance(time.Minute)
	autoRenew := 0
	notify("DID_CHANGE_RENEWAL_STATUS", "review-cancel", renewed, &autoRenew)
	me = h.do(http.MethodGet, "/api/v1/me", nil, h.auth(reviewer.access))
	if me.body["subscription"].(map[string]any)["auto_renews"] != false {
		t.Fatalf("review cancellation was ignored: %s", me.raw)
	}

	h.clock.Advance(time.Minute)
	refunded := h.subscriptionTx(reviewer.userID, "review-renewed", "review-original", "base_monthly", end, renewedEnd,
		func(tx *appstoretest.Transaction) {
			sandbox(tx)
			tx.RevocationDate = h.clock.Now().UnixMilli()
		})
	notify("REFUND", "review-refund", refunded, nil)
	me = h.do(http.MethodGet, "/api/v1/me", nil, h.auth(reviewer.access))
	if me.str("subscription", "plan", "code") != "free" {
		t.Fatalf("review refund was ignored: %s", me.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE user_id=? AND environment='Sandbox'`, reviewer.userID); n != 2 {
		t.Fatalf("review purchases must stay recorded as Sandbox: %d", n)
	}
}

func TestAppReviewSandboxNotificationsCannotGrantToOtherAccounts(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), withIAP(t), withEnv("APPLE_IAP_ACCEPT_SANDBOX", "false"),
		withEnv("APPLE_IAP_SANDBOX_ACCOUNTS", "app.review@example.com"))
	h.enableApprovedProducts()
	stranger := h.signIn("someone@example.com")
	start := h.clock.Now()
	for _, token := range []string{h.iap.AccountToken(stranger.userID), "", "unknown-token"} {
		transaction := h.subscriptionTx(stranger.userID, "stranger-renewed", "stranger-original", "base_monthly", start, start.Add(time.Hour),
			func(tx *appstoretest.Transaction) {
				tx.Environment = appstore.EnvironmentSandbox
				tx.AppAccountToken = token
			})
		payload := appstoretest.Notification{NotificationType: "DID_RENEW", NotificationUUID: "stranger-" + token, Version: "2.0",
			SignedDate: start.UnixMilli(), Data: appstoretest.NotificationData{BundleID: h.cfg.IAP.AppleBundleID,
				Environment: appstore.EnvironmentSandbox, SignedTransactionInfo: transaction}}
		mustStatus(t, h.do(http.MethodPost, "/api/v1/iap/apple/notifications",
			map[string]any{"signedPayload": h.apple.Sign(t, payload)}, nil), http.StatusOK, "")
	}
	if n := h.scalar(`SELECT COUNT(*) FROM iap_purchases`); n != 0 {
		t.Fatalf("unapproved Sandbox notifications granted %d purchases", n)
	}
}
