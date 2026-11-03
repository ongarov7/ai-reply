package apptest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
)

func TestProDeviceLifecyclePreservesSlotsAndPurchasedCredits(t *testing.T) {
	for _, initial := range []string{"free", "base_monthly"} {
		t.Run(initial, func(t *testing.T) {
			h := newHarness(t, approvedEntitlements(), strictProDevices())
			first, _ := h.signInProDevice("pro-lifecycle@example.com", "lifecycle-device-1", "ios", installID(910))
			admin := h.signInAdmin()
			if initial != "free" {
				h.assignPlan(admin, first.userID, initial, "")
			}
			if n := h.scalar(`SELECT count(*) FROM pro_device_authorizations WHERE user_id=?`, first.userID); n != 0 {
				t.Fatal("non-Pro claimed slot")
			}
			h.assignPlan(admin, first.userID, "pro_monthly", "")
			mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
			original := h.proDeviceAuthorization(first.access, "mobile")
			pack := h.addGrant(first.userID, domain.GrantPack, 5, h.clock.Now(), nil, h.clock.Now())
			second, _ := h.signInProDevice("pro-lifecycle@example.com", "lifecycle-device-2", "android", installID(911))
			mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_LIMIT_REACHED")
			if n := h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID); n != 5 {
				t.Fatal("blocked Pro consumed package")
			}
			svc := subscriptions.New(h.store, plans.New(h.store), h.cfg.App.Location()).WithClock(h.clock)
			sub, err := svc.Current(context.Background(), first.userID)
			if err != nil {
				t.Fatal(err)
			}
			end := h.clock.Now().Add(30 * 24 * time.Hour)
			if renewed, err := svc.Renew(context.Background(), first.userID, sub.PlanID, &end); err != nil || !renewed {
				t.Fatalf("renew: %v %v", renewed, err)
			}
			if h.proDeviceAuthorization(first.access, "mobile") != original {
				t.Fatal("renewal replaced authorization")
			}
			past := h.clock.Now().Add(-time.Minute)
			if err := svc.SetExpiry(context.Background(), sub.ID, first.userID, &past); err != nil {
				t.Fatal(err)
			}
			// Expiry follows existing entitlement accounting, including purchased
			// credits, even on a device which previously could not use Pro.
			mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusOK, "")
			if n := h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID); n != 4 {
				t.Fatal("expired plan did not use exactly one purchased message")
			}
			h.assignPlan(admin, first.userID, "pro_monthly", "")
			mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_LIMIT_REACHED")
			mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
			if h.proDeviceAuthorization(first.access, "mobile") != original {
				t.Fatal("restoration replaced authorization")
			}
		})
	}
}

func TestProClonedDeviceIDCannotDisruptOriginalInstallation(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), strictProDevices())
	first, original := h.signInProDevice("pro-clone@example.com", "cloned-mobile-1", "ios", installID(930))
	h.assignPlan(h.signInAdmin(), first.userID, "pro_monthly", "")
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
	id := h.proDeviceAuthorization(first.access, "mobile")
	clone, _ := h.signInProDevice("pro-clone@example.com", original.str("device_id"), "windows", installID(931))
	mustStatus(t, h.generate(clone.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_IDENTITY_REQUIRED")
	// The old device API accepts metadata, but must not move an occupied
	// mobile authorization into the desktop category or invalidate its binding.
	mustStatus(t, h.do(http.MethodPost, "/api/v1/devices", map[string]any{"device_id": original.str("device_id"), "platform": "windows", "model": "Forged metadata"}, h.auth(clone.access)), http.StatusOK, "")
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
	if h.proDeviceAuthorization(first.access, "mobile") != id {
		t.Fatal("cloned metadata displaced original")
	}
}

func TestProAppleGraceCancellationAndRestoreKeepAuthorization(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), strictProDevices(), withIAP(t))
	h.enableApprovedProducts()
	first, _ := h.signInProDevice("pro-grace@example.com", "grace-device-1", "ios", installID(940))
	start := h.clock.Now()
	end := start.Add(time.Hour)
	tx := h.subscriptionTx(first.userID, "pro-grace-1", "pro-grace-original", "pro_monthly", start, end, nil)
	mustStatus(t, h.postApple(first.access, tx), http.StatusOK, "")
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
	id := h.proDeviceAuthorization(first.access, "mobile")
	second, _ := h.signInProDevice("pro-grace@example.com", "grace-device-2", "android", installID(941))
	notice := func(kind, subtype, uuid string, auto int, grace time.Time) {
		renewal := appstore.Renewal{OriginalTransactionID: "pro-grace-original", AutoRenewStatus: auto, Environment: "Production", SignedDate: h.clock.Now().UnixMilli()}
		if !grace.IsZero() {
			renewal.GracePeriodExpiresDate = grace.UnixMilli()
		}
		payload := appstoretest.Notification{NotificationType: kind, Subtype: subtype, NotificationUUID: uuid, Version: "2.0", SignedDate: h.clock.Now().UnixMilli(), Data: appstoretest.NotificationData{BundleID: h.cfg.IAP.AppleBundleID, Environment: "Production", SignedTransactionInfo: tx, SignedRenewalInfo: h.apple.Sign(t, renewal)}}
		mustStatus(t, h.do(http.MethodPost, "/api/v1/iap/apple/notifications", map[string]any{"signedPayload": h.apple.Sign(t, payload)}, nil), http.StatusOK, "")
	}
	h.clock.Advance(time.Minute)
	notice("DID_CHANGE_RENEWAL_STATUS", "AUTO_RENEW_DISABLED", "pro-grace-cancel", 0, time.Time{})
	mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_LIMIT_REACHED")
	notice("DID_FAIL_TO_RENEW", "GRACE_PERIOD", "pro-grace-start", 1, end.Add(2*time.Hour))
	h.clock.Advance(90 * time.Minute)
	first = h.refresh(first)
	second = h.refresh(second)
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
	mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_LIMIT_REACHED")
	if h.proDeviceAuthorization(first.access, "mobile") != id {
		t.Fatal("grace replaced original")
	}
	notice("GRACE_PERIOD_EXPIRED", "", "pro-grace-end", 1, end.Add(2*time.Hour))
	pack := h.addGrant(first.userID, domain.GrantPack, 2, h.clock.Now(), nil, h.clock.Now())
	mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusOK, "")
	if h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, pack.ID) != 1 {
		t.Fatal("expiry changed purchased balance accounting")
	}
	tx = h.subscriptionTx(first.userID, "pro-grace-restored", "pro-grace-original", "pro_monthly", h.clock.Now(), h.clock.Now().Add(30*24*time.Hour), nil)
	mustStatus(t, h.postApple(first.access, tx), http.StatusOK, "")
	mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_LIMIT_REACHED")
	mustStatus(t, h.generate(first.access, "Сәлем"), http.StatusOK, "")
	if h.proDeviceAuthorization(first.access, "mobile") != id {
		t.Fatal("restore replaced original")
	}
}

func TestProFirstRegistrationWinsBeforeGeneration(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), strictProDevices())
	first, _ := h.signInProDevice("first-registration@example.com", "registration-device-1", "ios", installID(920))
	h.assignPlan(h.signInAdmin(), first.userID, "pro_monthly", "")
	// Refresh is an existing bound session; it reserves the first free slot
	// without sending a prompt or consuming a message.
	first = h.refresh(first)
	original := h.proDeviceAuthorization(first.access, "mobile")
	second, _ := h.signInProDevice("first-registration@example.com", "registration-device-2", "android", installID(921))
	mustStatus(t, h.generate(second.access, "Сәлем"), http.StatusForbidden, "PRO_DEVICE_LIMIT_REACHED")
	if h.proDeviceAuthorization(first.access, "mobile") != original {
		t.Fatal("new registration displaced first")
	}
	if h.scalar(`SELECT count(*) FROM usage_reservations WHERE user_id=?`, first.userID) != 0 {
		t.Fatal("registration/refresh used a message")
	}
}
