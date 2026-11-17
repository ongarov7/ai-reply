package apptest

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

type sharedPackDevice struct {
	platform string
	session  session
}

func sharedPackSignIns(t *testing.T, h *harness, address string) []sharedPackDevice {
	t.Helper()
	devices := make([]sharedPackDevice, 0, 4)
	for _, platform := range []string{"ios", "android", "macos", "windows"} {
		s, _ := h.signInProDevice(address, "shared-pack-"+platform, platform, "")
		if len(devices) != 0 && s.userID != devices[0].session.userID {
			t.Fatal("device sign-ins did not resolve to one account")
		}
		devices = append(devices, sharedPackDevice{platform: platform, session: s})
	}
	return devices
}

func assertSharedPackBalance(t *testing.T, h *harness, devices []sharedPackDevice, remaining int) {
	t.Helper()
	for _, device := range devices {
		usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(device.session.access))
		mustStatus(t, usage, http.StatusOK, "")
		if usage.num("purchased_credits") != float64(remaining) || usage.num("credits") != float64(remaining) || usage.num("remaining_total") != float64(remaining) {
			t.Fatalf("%s sees a separate/stale pack balance, want %d: %s", device.platform, remaining, usage.raw)
		}
		if usage.body["unlimited"] != false || usage.num("daily_limit") != 0 || usage.num("welcome", "remaining") != 0 || usage.num("admin_credits") != 0 {
			t.Fatalf("%s fixture has access outside the shared purchased pack: %s", device.platform, usage.raw)
		}
	}
	userID := devices[0].session.userID
	if h.scalar(`SELECT COUNT(*) FROM pro_device_authorizations WHERE user_id=?`, userID) != 0 ||
		h.scalar(`SELECT COUNT(*) FROM pro_device_events WHERE user_id=?`, userID) != 0 {
		t.Fatal("purchased pack access entered the Pro device restriction")
	}
}

func sharedPackGenerate(h *harness, device sharedPackDevice, key string) response {
	headers := h.auth(device.session.access)
	headers["Idempotency-Key"] = key
	return h.do(http.MethodPost, "/api/v1/ai/reply", map[string]any{
		"source_text": "Сәлем", "instruction": "Сыпайы жауап бер", "language": "kk",
		"template_id": "client", "platform": device.platform, "app_version": "1.0.0",
	}, headers)
}

func TestSharedPurchasedPackBalanceAcrossPhoneAndDesktop(t *testing.T) {
	for _, units := range []int{30, 500} {
		t.Run(fmt.Sprintf("pack%d", units), func(t *testing.T) {
			h := newHarness(t, approvedEntitlements(), legacyProDevices(), withWelcomeBonus(0))
			devices := sharedPackSignIns(t, h, "shared-pack@example.com")
			userID := devices[0].session.userID
			h.addGrant(userID, domain.GrantPack, units, h.clock.Now(), nil, h.clock.Now())
			assertSharedPackBalance(t, h, devices, units)

			// Spend first on a phone, then a laptop, then the other platforms.
			// Every fresh status request must see the same account balance.
			for index, device := range []sharedPackDevice{devices[0], devices[2], devices[1], devices[3]} {
				result := sharedPackGenerate(h, device, fmt.Sprintf("shared-pack-spend-%d", index))
				mustStatus(t, result, http.StatusOK, "")
				if result.str("usage", "bucket") != domain.GrantPack || result.num("usage", "purchased_credits") != float64(units-index-1) {
					t.Fatalf("%s did not consume exactly one shared pack reply: %s", device.platform, result.raw)
				}
				assertSharedPackBalance(t, h, devices, units-index-1)
			}
			if h.provider.calls != len(devices) || h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id=? AND entry='consume' AND kind='pack' AND delta=-1`, userID) != len(devices) {
				t.Fatal("shared pack requests and consumed units disagree")
			}
		})
	}
}

func TestSharedPurchasedPackLastReplyIsAtomicAcrossDevices(t *testing.T) {
	h := newHarness(t, approvedEntitlements(), legacyProDevices(), withWelcomeBonus(0))
	devices := sharedPackSignIns(t, h, "shared-last-reply@example.com")
	userID := devices[0].session.userID
	grant := h.addGrant(userID, domain.GrantPack, 1, h.clock.Now(), nil, h.clock.Now())
	assertSharedPackBalance(t, h, devices, 1)

	const requests = 8
	start := make(chan struct{})
	var ready, workers sync.WaitGroup
	ready.Add(requests)
	workers.Add(requests)
	results := make([]response, requests)
	for index := range results {
		go func(index int) {
			defer workers.Done()
			ready.Done()
			<-start
			results[index] = sharedPackGenerate(h, devices[index%len(devices)], fmt.Sprintf("shared-pack-last-%d", index))
		}(index)
	}
	ready.Wait()
	close(start)
	workers.Wait()

	success, exhausted := 0, 0
	for _, result := range results {
		if result.status == http.StatusOK {
			success++
			if result.str("usage", "bucket") != domain.GrantPack {
				t.Fatalf("last reply used a different allowance: %s", result.raw)
			}
		} else {
			mustStatus(t, result, http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")
			exhausted++
		}
	}
	if success != 1 || exhausted != requests-1 || h.provider.calls != 1 {
		t.Fatalf("last pack reply was shared incorrectly: success=%d exhausted=%d provider_calls=%d", success, exhausted, h.provider.calls)
	}
	if h.scalar(`SELECT units_remaining FROM credit_grants WHERE id=?`, grant.ID) != 0 ||
		h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id=? AND grant_id=? AND entry='consume' AND kind='pack' AND delta=-1`, userID, grant.ID) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM usage_reservations WHERE user_id=? AND status='committed' AND bucket='pack'`, userID) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM usage_reservations WHERE user_id=?`, userID) != 1 {
		t.Fatal("concurrent devices double-spent or left an extra pack reservation")
	}
	assertSharedPackBalance(t, h, devices, 0)
}
