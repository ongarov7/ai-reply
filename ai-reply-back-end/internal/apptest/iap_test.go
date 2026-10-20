package apptest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/iap/appstore/appstoretest"
	"github.com/aireply/ai-reply-back-end/internal/iap/googleplay"
	"github.com/aireply/ai-reply-back-end/internal/iap/googleplay/playtest"
)

// Дүкендердегі өнім идентификаторлары (тест).
const (
	packApple  = "kz.aireply.pack50"
	passApple  = "kz.aireply.pass24"
	packGoogle = "pack_50"
	passGoogle = "pass_24h"
)

// withIAP — App Store (жалған Apple тізбегі) және Google Play (жалған сервер) қосулы.
func withIAP(t *testing.T) harnessOption {
	_, key := playtest.Key(t)
	return func(env map[string]string) {
		env["APPLE_IAP_ENABLED"] = "true"
		env["GOOGLE_PLAY_CLIENT_EMAIL"] = playtest.ClientEmail
		env["GOOGLE_PLAY_PRIVATE_KEY"] = key
	}
}

func packBody() map[string]any {
	return map[string]any{"code": "pack_50", "kind": "pack", "units": 50, "apple_product_id": packApple,
		"google_product_id": packGoogle, "is_active": true, "sort_order": 1,
		"name": map[string]any{"kk": "50 жауап", "ru": "50 ответов", "en": "50 answers", "uz": "50 ta javob"}}
}

func passBody() map[string]any {
	return map[string]any{"code": "pass_24h", "kind": "pass", "units": 40, "duration_hours": 24,
		"apple_product_id": passApple, "google_product_id": passGoogle, "is_active": true, "sort_order": 2,
		"name": map[string]any{"kk": "24 сағат", "ru": "24 часа", "en": "24 hours", "uz": "24 soat"}}
}

func (h *harness) adminCall(admin adminSession, method, path string, body any) response {
	return h.do(method, "/api/v1/admin"+path, body, admin.headers(h.cfg.Admin.CookieName))
}

// createProduct — әкімші панелі арқылы өнім; id қайтарады.
func (h *harness) createProduct(admin adminSession, body map[string]any) string {
	h.t.Helper()
	res := h.adminCall(admin, http.MethodPost, "/iap/products", body)
	mustStatus(h.t, res, http.StatusCreated, "")
	return res.str("product", "id")
}

// openIAP — пакет пен pass жасалып, сатылым қосылады.
func (h *harness) openIAP() adminSession {
	h.t.Helper()
	admin := h.signInAdmin()
	h.createProduct(admin, packBody())
	h.createProduct(admin, passBody())
	mustStatus(h.t, h.adminCall(admin, http.MethodPost, "/settings/iap", map[string]any{"enabled": true}), http.StatusOK, "")
	return admin
}

// appleTx — App Store сияқты қолтаңбалы транзакция (берілген тіркелгінің токенімен).
func (h *harness) appleTx(userID, transactionID, productID string, change func(*appstoretest.Transaction)) string {
	h.t.Helper()
	now := h.clock.Now()
	tx := appstoretest.Transaction{TransactionID: transactionID, OriginalTransactionID: transactionID,
		BundleID: h.cfg.IAP.AppleBundleID, ProductID: productID, PurchaseDate: now.UnixMilli(), Quantity: 1,
		Type: "Consumable", AppAccountToken: h.iap.AccountToken(userID), InAppOwnershipType: "PURCHASED",
		SignedDate: now.UnixMilli(), Environment: "Production", Storefront: "KAZ", Currency: "KZT", Price: 990000}
	if productID == passApple {
		tx.Type = "Non-Renewing Subscription"
	}
	if change != nil {
		change(&tx)
	}
	return h.apple.Sign(h.t, tx)
}

func (h *harness) postApple(access, signed string) response {
	return h.do(http.MethodPost, "/api/v1/iap/apple/transactions", map[string]any{"signed_transaction": signed}, h.auth(access))
}

func (h *harness) postGoogle(access, productID, token string) response {
	return h.do(http.MethodPost, "/api/v1/iap/google/purchases",
		map[string]any{"product_id": productID, "purchase_token": token}, h.auth(access))
}

// appleNotification — App Store Server Notification V2 (қолтаңбалы).
func (h *harness) appleNotification(kind, uuid, signedTx string) response {
	h.t.Helper()
	payload := appstoretest.Notification{NotificationType: kind, NotificationUUID: uuid, Version: "2.0",
		SignedDate: h.clock.Now().UnixMilli(), Data: appstoretest.NotificationData{AppAppleID: 6700000001,
			BundleID: h.cfg.IAP.AppleBundleID, BundleVersion: "12", Environment: "Production", SignedTransactionInfo: signedTx}}
	return h.do(http.MethodPost, "/api/v1/iap/apple/notifications",
		map[string]any{"signedPayload": h.apple.Sign(h.t, payload)}, nil)
}

// playToken — Google purchase token пішіні.
func playToken(seed int) string {
	return fmt.Sprintf("opaque-token-%04d.AO-J1Oz%s", seed, strings.Repeat("Xq9", 30))
}

// playPurchase — Google білетін сатып алу (берілген тіркелгінің токенімен).
func (h *harness) playPurchase(userID, productID, token string, change func(*googleplay.ProductPurchase)) {
	p := googleplay.ProductPurchase{PurchaseTimeMillis: strconv.FormatInt(h.clock.Now().UnixMilli(), 10),
		OrderID: "GPA.3300-" + token[13:17], ObfuscatedExternalAccountID: h.iap.AccountToken(userID), Quantity: 1,
		RegionCode: "KZ"}
	if change != nil {
		change(&p)
	}
	h.play.Put(productID, token, p)
}

func sha256Hex(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

func (h *harness) feature(name string) bool {
	h.t.Helper()
	config := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	features, _ := config.body["features"].(map[string]any)
	on, ok := features[name].(bool)
	if !ok {
		h.t.Fatalf("features.%s missing: %s", name, config.raw)
	}
	return on
}

func (h *harness) products(access, platform string) response {
	path := "/api/v1/iap/products"
	if platform != "" {
		path += "?platform=" + platform
	}
	return h.do(http.MethodGet, path, nil, h.auth(access))
}

// limitDetails — күндік лимит біткендегі қатенің details-і.
func (h *harness) limitDetails(access string) map[string]any {
	h.t.Helper()
	res := h.generate(access, "Тағы бір сұрақ бар еді")
	mustStatus(h.t, res, http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")
	details, _ := res.body["error"].(map[string]any)["details"].(map[string]any)
	return details
}

func (h *harness) creditsLeft(access string) float64 {
	h.t.Helper()
	return h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(access)).num("credits")
}

// Дүкен бапталмаған серверде сатып алу мүлде жоқ: флагтар өшірулі, маршруттар 403, ауыстырғыш қосылмайды.
func TestIAPStaysOffWithoutAStore(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("iap-off@example.com")
	if h.feature("iap_ios") || h.feature("iap_android") {
		t.Fatal("purchases announced without a store")
	}
	mustStatus(t, h.products(s.access, "ios"), http.StatusForbidden, "IAP_UNAVAILABLE")
	mustStatus(t, h.postApple(s.access, "a.b.c"), http.StatusForbidden, "IAP_UNAVAILABLE")
	mustStatus(t, h.postGoogle(s.access, packGoogle, playToken(1)), http.StatusForbidden, "IAP_UNAVAILABLE")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/iap/apple/notifications", map[string]any{"signedPayload": "a.b.c"}, nil),
		http.StatusForbidden, "IAP_UNAVAILABLE")

	admin := h.signInAdmin()
	h.createProduct(admin, packBody())
	on := h.adminCall(admin, http.MethodPost, "/settings/iap", map[string]any{"enabled": true})
	mustStatus(t, on, http.StatusConflict, "IAP_UNAVAILABLE")
	if on.str("error", "details", "reason") != "store_not_configured" {
		t.Fatalf("refusal: %s", on.raw)
	}
	if h.auditMeta("settings.iap.update") != nil {
		t.Fatal("a refused switch was audited as a change")
	}
	settings := h.adminCall(admin, http.MethodGet, "/settings", nil)
	if iap, _ := settings.body["iap"].(map[string]any); iap == nil || iap["configured"] != false || iap["enabled"] != false {
		t.Fatalf("settings.iap = %s", settings.raw)
	}
	h.useQuota(s.userID, 7)
	if details := h.limitDetails(s.access); details["can_buy"] != false {
		t.Fatalf("can_buy = %v", details["can_buy"])
	}
}

// Бапталған дүкен де: өнім, белсенділік және ауыстырғыш болмайынша ештеңе көрінбейді.
func TestIAPOpensOnlyWithProductsAndTheSwitch(t *testing.T) {
	h := newHarness(t, withIAP(t))
	s := h.signIn("iap-open@example.com")
	admin := h.signInAdmin()
	h.useQuota(s.userID, 7)

	if h.feature("iap_ios") || h.feature("iap_android") {
		t.Fatal("open before an admin did anything")
	}
	mustStatus(t, h.products(s.access, "ios"), http.StatusForbidden, "IAP_UNAVAILABLE")
	mustStatus(t, h.adminCall(admin, http.MethodPost, "/settings/iap", map[string]any{"enabled": true}), http.StatusOK, "")
	if h.feature("iap_ios") || h.feature("iap_android") {
		t.Fatal("open without products")
	}

	pack := packBody()
	delete(pack, "google_product_id")
	packID := h.createProduct(admin, pack)
	inactive := passBody()
	inactive["is_active"] = false
	h.createProduct(admin, inactive)
	if !h.feature("iap_ios") || h.feature("iap_android") {
		t.Fatal("an iOS-only product must open iOS only")
	}
	if details := h.limitDetails(s.access); details["can_buy"] != true {
		t.Fatalf("can_buy on iOS = %v", details["can_buy"])
	}

	list := h.products(s.access, "ios")
	mustStatus(t, list, http.StatusOK, "")
	items, _ := list.body["products"].([]any)
	if len(items) != 1 {
		t.Fatalf("products = %s", list.raw)
	}
	item := items[0].(map[string]any)
	if item["code"] != "pack_50" || item["kind"] != "pack" || item["units"] != float64(50) ||
		item["duration_hours"] != float64(0) || item["store_product_id"] != packApple || item["name"] != "50 жауап" {
		t.Fatalf("product = %v", item)
	}
	token := list.str("account_token")
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(token) ||
		token != h.iap.AccountToken(s.userID) {
		t.Fatalf("account_token %q", token)
	}
	other := h.signIn("iap-open-other@example.com")
	if h.products(other.access, "ios").str("account_token") == token {
		t.Fatal("two accounts share an account token")
	}
	if fromToken := h.products(s.access, ""); len(fromToken.body["products"].([]any)) != 1 {
		t.Fatalf("platform from the access token: %s", fromToken.raw)
	}
	mustStatus(t, h.products(s.access, "android"), http.StatusForbidden, "IAP_UNAVAILABLE")
	if bad := h.products(s.access, "web"); bad.status != http.StatusBadRequest || bad.str("error", "details", "field") != "platform" {
		t.Fatalf("platform=web: %s", bad.raw)
	}

	// The Google id opens Android.
	pack["google_product_id"] = packGoogle
	mustStatus(t, h.adminCall(admin, http.MethodPatch, "/iap/products/"+packID, pack), http.StatusOK, "")
	if !h.feature("iap_android") {
		t.Fatal("android stays closed with a Google id")
	}
	android := h.products(s.access, "android")
	if items, _ := android.body["products"].([]any); len(items) != 1 || items[0].(map[string]any)["store_product_id"] != packGoogle {
		t.Fatalf("android products = %s", android.raw)
	}

	// Switching sales off closes everything again, and so does archiving the only product.
	mustStatus(t, h.adminCall(admin, http.MethodPost, "/settings/iap", map[string]any{"enabled": false}), http.StatusOK, "")
	if h.feature("iap_ios") || h.feature("iap_android") {
		t.Fatal("open after the switch went off")
	}
	if details := h.limitDetails(s.access); details["can_buy"] != false {
		t.Fatal("can_buy with sales off")
	}
	mustStatus(t, h.adminCall(admin, http.MethodPost, "/settings/iap", map[string]any{"enabled": true}), http.StatusOK, "")
	mustStatus(t, h.adminCall(admin, http.MethodPost, "/iap/products/"+packID+"/archive", map[string]any{}), http.StatusOK, "")
	if h.feature("iap_ios") {
		t.Fatal("an archived product keeps sales open")
	}
	mustStatus(t, h.products(s.access, "ios"), http.StatusForbidden, "IAP_UNAVAILABLE")
}

// ---------------------------------------------------------------- App Store

// Бір транзакция бір рет беріледі; басқа тіркелгі оны ала алмайды.
func TestAppleTransactionIsGrantedOnce(t *testing.T) {
	h := newHarness(t, withIAP(t))
	h.openIAP()
	s := h.signIn("iap-apple@example.com")
	signed := h.appleTx(s.userID, "2000000000000001", packApple, nil)

	first := h.postApple(s.access, signed)
	mustStatus(t, first, http.StatusOK, "")
	if first.str("status") != "granted" || first.num("usage", "credits") != 50 || first.num("usage", "remaining_total") != 57 {
		t.Fatalf("first post: %s", first.raw)
	}
	again := h.postApple(s.access, signed)
	if again.status != http.StatusOK || again.str("status") != "already_granted" || again.num("usage", "credits") != 50 {
		t.Fatalf("re-post: %d %s", again.status, again.raw)
	}
	purchaseID := h.text(`SELECT id FROM iap_purchases WHERE provider = 'app_store' AND transaction_id = '2000000000000001'
		AND user_id = ? AND status = 'granted' AND units = 50 AND environment = 'Production' AND product_code = 'pack_50'`, s.userID)
	if purchaseID == "" || h.scalar(`SELECT COUNT(*) FROM iap_purchases`) != 1 {
		t.Fatal("purchase row")
	}
	if h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id = ? AND kind = 'pack' AND units_total = 50
		AND iap_purchase_id = ? AND expires_at IS NULL AND product_code = 'pack_50'`, s.userID, purchaseID) != 1 {
		t.Fatal("pack grant: one, never expiring, pointing at the purchase")
	}
	if h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id = ? AND entry = 'grant' AND kind = 'pack' AND delta = 50
		AND reason = 'app_store'`, s.userID) != 1 {
		t.Fatal("ledger grant")
	}

	// quantity 2 gives twice the units.
	double := h.postApple(s.access, h.appleTx(s.userID, "2000000000000002", packApple,
		func(tx *appstoretest.Transaction) { tx.Quantity = 2 }))
	if double.str("status") != "granted" || double.num("usage", "credits") != 150 {
		t.Fatalf("quantity 2: %s", double.raw)
	}

	// Another account cannot redeem it: its token does not match, and a
	// transaction bound to an account is never moved to another one.
	other := h.signIn("iap-apple-other@example.com")
	stolen := h.postApple(other.access, signed)
	mustStatus(t, stolen, http.StatusBadRequest, "PURCHASE_INVALID")
	if stolen.str("error", "details", "reason") != "account_mismatch" {
		t.Fatalf("stolen: %s", stolen.raw)
	}
	mustStatus(t, h.postApple(other.access, h.appleTx(other.userID, "2000000000000001", packApple, nil)),
		http.StatusConflict, "PURCHASE_BELONGS_TO_ANOTHER_ACCOUNT")
	if h.creditsLeft(other.access) != 0 || h.creditsLeft(s.access) != 150 {
		t.Fatal("balances moved")
	}
	if strings.Contains(h.logs.String(), strings.Split(signed, ".")[2]) {
		t.Fatal("logs contain the signed transaction")
	}
}

// Тексеруден өтпеген транзакция ештеңе бермейді және ештеңе жазбайды.
func TestAppleTransactionsThatDoNotVerifyGrantNothing(t *testing.T) {
	h := newHarness(t, withIAP(t), withEnv("APPLE_IAP_ACCEPT_SANDBOX", "false"))
	h.openIAP()
	s := h.signIn("iap-apple-refused@example.com")
	other := h.signIn("iap-apple-refused-other@example.com")
	stranger := appstoretest.NewChain(t, appstoretest.Options{})
	base := func(change func(*appstoretest.Transaction)) string {
		return h.appleTx(s.userID, "2000000000000100", packApple, change)
	}
	valid := base(nil)
	parts := strings.Split(valid, ".")
	tampered := strings.Split(base(func(tx *appstoretest.Transaction) { tx.ProductID = passApple }), ".")[1]
	payload := appstoretest.Transaction{TransactionID: "2000000000000100", BundleID: h.cfg.IAP.AppleBundleID,
		ProductID: packApple, Type: "Consumable", Quantity: 1, AppAccountToken: h.iap.AccountToken(s.userID),
		SignedDate: h.clock.Now().UnixMilli(), Environment: "Production"}

	for _, tc := range []struct {
		name, signed, reason string
	}{
		{"another app", base(func(tx *appstoretest.Transaction) { tx.BundleID = "kz.other.app" }), "bundle_mismatch"},
		{"sandbox", base(func(tx *appstoretest.Transaction) { tx.Environment = "Sandbox" }), "environment"},
		{"xcode", base(func(tx *appstoretest.Transaction) { tx.Environment = "Xcode" }), "environment"},
		{"another account", base(func(tx *appstoretest.Transaction) { tx.AppAccountToken = h.iap.AccountToken(other.userID) }), "account_mismatch"},
		{"no account token", base(func(tx *appstoretest.Transaction) { tx.AppAccountToken = "" }), "account_mismatch"},
		{"revoked", base(func(tx *appstoretest.Transaction) { tx.RevocationDate = h.clock.Now().UnixMilli() }), "revoked"},
		{"subscription", base(func(tx *appstoretest.Transaction) { tx.Type = "Auto-Renewable Subscription" }), "product_type"},
		{"non-consumable", base(func(tx *appstoretest.Transaction) { tx.Type = "Non-Consumable" }), "product_type"},
		{"unknown product", base(func(tx *appstoretest.Transaction) { tx.ProductID = "kz.aireply.pack999" }), "product_unknown"},
		{"another chain", stranger.Sign(t, payload), "signature"},
		{"alg none", h.apple.SignWithHeader(t, map[string]any{"alg": "none", "x5c": h.apple.X5C()}, payload), "signature"},
		{"HS256", h.apple.SignWithHeader(t, map[string]any{"alg": "HS256", "x5c": h.apple.X5C()}, payload), "signature"},
		{"tampered", parts[0] + "." + tampered + "." + parts[2], "signature"},
		{"garbage", "a.b.c", "malformed"},
	} {
		res := h.postApple(s.access, tc.signed)
		if res.status != http.StatusBadRequest || res.errorCode() != "PURCHASE_INVALID" ||
			res.str("error", "details", "reason") != tc.reason {
			t.Errorf("%s: %d %s", tc.name, res.status, res.raw)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM iap_purchases`) + h.scalar(`SELECT COUNT(*) FROM credit_grants`); n != 0 {
		t.Fatalf("refused transactions left %d row(s)", n)
	}
	if h.creditsLeft(s.access) != 0 {
		t.Fatal("credits from a refused transaction")
	}
	empty := h.do(http.MethodPost, "/api/v1/iap/apple/transactions", map[string]any{"signed_transaction": ""}, h.auth(s.access))
	if empty.status != http.StatusBadRequest || empty.str("error", "details", "field") != "signed_transaction" {
		t.Fatalf("empty: %s", empty.raw)
	}
	unknown := h.do(http.MethodPost, "/api/v1/iap/apple/transactions",
		map[string]any{"signed_transaction": valid, "receipt": "x"}, h.auth(s.access))
	mustStatus(t, unknown, http.StatusBadRequest, "INVALID_REQUEST")
	mustStatus(t, h.postApple(s.access, valid), http.StatusOK, "")
}

// Sandbox (TestFlight, App Review) қабылданатын серверде — ортасымен жазылады, нақты сатылымнан бөлек.
func TestSandboxPurchasesAreRecordedApart(t *testing.T) {
	h := newHarness(t, withIAP(t))
	admin := h.openIAP()
	s := h.signIn("iap-sandbox@example.com")
	res := h.postApple(s.access, h.appleTx(s.userID, "2000000000000200", packApple,
		func(tx *appstoretest.Transaction) { tx.Environment = "Sandbox" }))
	if res.status != http.StatusOK || res.str("status") != "granted" {
		t.Fatalf("sandbox: %s", res.raw)
	}
	mustStatus(t, h.postApple(s.access, h.appleTx(s.userID, "2000000000000201", packApple, nil)), http.StatusOK, "")

	sandbox := h.adminCall(admin, http.MethodGet, "/iap/purchases?environment=Sandbox", nil)
	production := h.adminCall(admin, http.MethodGet, "/iap/purchases?environment=Production", nil)
	if sandbox.num("total") != 1 || production.num("total") != 1 {
		t.Fatalf("sandbox %s production %s", sandbox.raw, production.raw)
	}
	stats, _ := sandbox.body["stats"].([]any)
	seen := map[string]float64{}
	for _, g := range stats {
		row := g.(map[string]any)
		seen[row["environment"].(string)+"/"+row["status"].(string)] = row["count"].(float64)
	}
	if seen["Sandbox/granted"] != 1 || seen["Production/granted"] != 1 {
		t.Fatalf("stats %v", seen)
	}
}

// Қайтару хабарламасы қалғанын алады (теріс емес), қайталанса ештеңе өзгермейді; REFUND_REVERSED қайтарады.
func TestRefundNotificationRevokesWhatIsLeft(t *testing.T) {
	h := newHarness(t, withIAP(t))
	h.openIAP()
	s := h.signIn("iap-refund@example.com")
	signed := h.appleTx(s.userID, "2000000000000010", packApple, nil)
	mustStatus(t, h.postApple(s.access, signed), http.StatusOK, "")
	h.useQuota(s.userID, 7)
	for i := 0; i < 3; i++ {
		if res := h.generate(s.access, "Пакеттен жауап"); res.str("usage", "bucket") != "pack" {
			t.Fatalf("generation %d: %s", i, res.raw)
		}
	}
	if h.creditsLeft(s.access) != 47 {
		t.Fatal("pack after three generations")
	}

	refunded := h.appleTx(s.userID, "2000000000000010", packApple, func(tx *appstoretest.Transaction) {
		tx.RevocationDate, tx.RevocationReason = h.clock.Now().UnixMilli(), new(int)
	})
	mustStatus(t, h.appleNotification("REFUND", "6b8f2d3e-0000-4000-8000-000000000001", refunded), http.StatusOK, "")
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.num("credits") != 0 || usage.num("remaining_total") != 0 {
		t.Fatalf("usage after the refund: %s", usage.raw)
	}
	if h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id = ? AND kind = 'pack' AND status = 'revoked'
		AND units_remaining = 0 AND revoked_by = 'system' AND revoke_reason = 'refund:0'`, s.userID) != 1 {
		t.Fatal("grant not revoked")
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id = '2000000000000010' AND status = 'refunded'
		AND revoke_reason = 'refund:0' AND revoked_at IS NOT NULL`) != 1 {
		t.Fatal("purchase not refunded")
	}
	if h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id = ? AND entry = 'revoke' AND delta = -47`, s.userID) != 1 {
		t.Fatal("ledger revoke of what was left")
	}
	mustStatus(t, h.generate(s.access, "Тағы"), http.StatusTooManyRequests, "DAILY_LIMIT_REACHED")

	// The same notification again, and another one for the same transaction: nothing moves.
	mustStatus(t, h.appleNotification("REFUND", "6b8f2d3e-0000-4000-8000-000000000001", refunded), http.StatusOK, "")
	mustStatus(t, h.appleNotification("REFUND", "6b8f2d3e-0000-4000-8000-000000000002", refunded), http.StatusOK, "")
	if h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id = ? AND entry = 'revoke'`, s.userID) != 1 ||
		h.scalar(`SELECT COUNT(*) FROM iap_notifications`) != 2 ||
		h.text(`SELECT outcome FROM iap_notifications WHERE notification_id = '6b8f2d3e-0000-4000-8000-000000000002'`) != "unchanged" {
		t.Fatal("a repeated refund changed something")
	}
	stale := h.postApple(s.access, signed)
	mustStatus(t, stale, http.StatusBadRequest, "PURCHASE_INVALID")
	if stale.str("error", "details", "reason") != "revoked" {
		t.Fatalf("re-post after the refund: %s", stale.raw)
	}

	// Apple reverses the refund: what the refund took comes back.
	mustStatus(t, h.appleNotification("REFUND_REVERSED", "6b8f2d3e-0000-4000-8000-000000000003", signed), http.StatusOK, "")
	if h.creditsLeft(s.access) != 47 || h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id = ? AND entry = 'grant'
		AND delta = 47 AND reason = 'refund_reversed'`, s.userID) != 1 {
		t.Fatal("refund not reversed")
	}
	if again := h.postApple(s.access, signed); again.str("status") != "already_granted" {
		t.Fatalf("re-post after the reversal: %s", again.raw)
	}

	// A refund for a transaction never posted: recorded, and never granted later.
	never := h.appleTx(s.userID, "2000000000000099", packApple, nil)
	mustStatus(t, h.appleNotification("REFUND", "6b8f2d3e-0000-4000-8000-000000000004", never), http.StatusOK, "")
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id = '2000000000000099' AND user_id IS NULL
		AND status = 'refunded' AND product_code = 'pack_50'`) != 1 {
		t.Fatal("no tombstone for the unknown refund")
	}
	late := h.postApple(s.access, never)
	if late.status != http.StatusBadRequest || late.str("error", "details", "reason") != "revoked" {
		t.Fatalf("post after an early refund: %s", late.raw)
	}

	for i, kind := range []string{"TEST", "CONSUMPTION_REQUEST", "DID_CHANGE_RENEWAL_STATUS"} {
		mustStatus(t, h.appleNotification(kind, fmt.Sprintf("6b8f2d3e-0000-4000-8000-00000000010%d", i), signed), http.StatusOK, "")
	}
	forged := appstoretest.NewChain(t, appstoretest.Options{}).Sign(t, appstoretest.Notification{NotificationType: "REFUND",
		NotificationUUID: "6b8f2d3e-0000-4000-8000-000000000200", SignedDate: h.clock.Now().UnixMilli(),
		Data: appstoretest.NotificationData{BundleID: h.cfg.IAP.AppleBundleID, SignedTransactionInfo: signed}})
	mustStatus(t, h.do(http.MethodPost, "/api/v1/iap/apple/notifications", map[string]any{"signedPayload": forged}, nil),
		http.StatusBadRequest, "PURCHASE_INVALID")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/iap/apple/notifications", map[string]any{}, nil), http.StatusBadRequest, "INVALID_REQUEST")
	if h.creditsLeft(s.access) != 47 || h.scalar(`SELECT COUNT(*) FROM iap_notifications`) != 7 {
		t.Fatalf("after the other notifications: credits %v, %d notifications", h.creditsLeft(s.access),
			h.scalar(`SELECT COUNT(*) FROM iap_notifications`))
	}
}

// Pass: екіншісі біріншісі біткенде басталады, әрқайсысы өз мерзімінде бітеді.
func TestPassesStackAndExpire(t *testing.T) {
	h := newHarness(t, withIAP(t))
	h.openIAP()
	s := h.signIn("iap-pass@example.com")
	start := h.clock.Now()
	loc := h.cfg.App.Location()
	first := h.postApple(s.access, h.appleTx(s.userID, "2000000000000300", passApple, nil))
	if first.str("usage", "pass", "expires_at") != start.Add(24*time.Hour).In(loc).Format(time.RFC3339) ||
		first.num("usage", "pass", "remaining") != 40 {
		t.Fatalf("first pass: %s", first.raw)
	}
	second := h.postApple(s.access, h.appleTx(s.userID, "2000000000000301", passApple, nil))
	if second.str("status") != "granted" || second.str("usage", "pass", "expires_at") != first.str("usage", "pass", "expires_at") ||
		second.num("usage", "pass", "remaining") != 40 {
		t.Fatalf("second pass shows before it starts: %s", second.raw)
	}
	if h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id = ? AND kind = 'pass' AND starts_at = ? AND expires_at = ?`,
		s.userID, start.Add(24*time.Hour).UnixMilli(), start.Add(48*time.Hour).UnixMilli()) != 1 {
		t.Fatal("the second pass does not start when the first ends")
	}
	h.useQuota(s.userID, 7)
	if res := h.generate(s.access, "Pass арқылы"); res.str("usage", "bucket") != "pass" || res.num("usage", "pass", "remaining") != 39 {
		t.Fatalf("generation: %s", res.raw)
	}

	h.clock.Advance(25 * time.Hour)
	s = h.refresh(s)
	usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.str("pass", "expires_at") != start.Add(48*time.Hour).In(loc).Format(time.RFC3339) || usage.num("pass", "remaining") != 40 {
		t.Fatalf("after the first pass: %s", usage.raw)
	}
	h.clock.Advance(24 * time.Hour)
	s = h.refresh(s)
	usage = h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(s.access))
	if usage.body["pass"] != nil {
		t.Fatalf("a pass outlived its time: %s", usage.raw)
	}
}

// Сатып алынған бірліктер бар ретке кіреді: тариф → pass → бонус → әкімші → пакет.
func TestPurchasedUnitsJoinTheConsumptionOrder(t *testing.T) {
	h := newHarness(t, withIAP(t), withWelcomeBonus(2))
	admin := h.signInAdmin()
	h.createProduct(admin, map[string]any{"code": "pack_3", "kind": "pack", "units": 3, "apple_product_id": "kz.aireply.pack3",
		"is_active": true})
	h.createProduct(admin, map[string]any{"code": "pass_2", "kind": "pass", "units": 2, "duration_hours": 24,
		"apple_product_id": "kz.aireply.pass2", "is_active": true})
	mustStatus(t, h.adminCall(admin, http.MethodPost, "/settings/iap", map[string]any{"enabled": true}), http.StatusOK, "")
	s := h.signIn("iap-order@example.com")
	mustStatus(t, h.postApple(s.access, h.appleTx(s.userID, "2000000000000400", "kz.aireply.pack3", nil)), http.StatusOK, "")
	mustStatus(t, h.giveCredits(admin, s.userID, 1, "goodwill"), http.StatusOK, "")
	mustStatus(t, h.postApple(s.access, h.appleTx(s.userID, "2000000000000401", "kz.aireply.pass2",
		func(tx *appstoretest.Transaction) { tx.Type = "Non-Renewing Subscription" })), http.StatusOK, "")

	h.useQuota(s.userID, 7)
	var got []string
	for i := 0; i < 8; i++ {
		res := h.generate(s.access, "Кезекпен жауап "+strconv.Itoa(i))
		mustStatus(t, res, http.StatusOK, "")
		got = append(got, res.str("usage", "bucket"))
	}
	if want := "pass pass welcome welcome admin pack pack pack"; strings.Join(got, " ") != want {
		t.Fatalf("buckets %v, want %s", got, want)
	}
	details := h.limitDetails(s.access)
	if details["credits_remaining"] != float64(0) || details["welcome_remaining"] != float64(0) ||
		details["pass_active"] != true || details["can_buy"] != true {
		t.Fatalf("limit details %v", details)
	}
}

// ---------------------------------------------------------------- Google Play

// Google: алдымен беріледі, содан кейін сервер тұтынады; токен ашық түрде сақталмайды.
func TestGooglePurchaseIsGrantedThenConsumed(t *testing.T) {
	h := newHarness(t, withIAP(t))
	h.openIAP()
	s := h.signIn("iap-google@example.com")
	token := playToken(1)
	h.playPurchase(s.userID, packGoogle, token, nil)

	res := h.postGoogle(s.access, packGoogle, token)
	if res.status != http.StatusOK || res.str("status") != "granted" || res.num("usage", "credits") != 50 {
		t.Fatalf("post: %d %s", res.status, res.raw)
	}
	if h.play.Purchase(packGoogle, token).ConsumptionState != googleplay.Consumed || h.play.ConsumeCalls.Load() != 1 {
		t.Fatal("the server did not consume")
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE provider = 'google_play' AND transaction_id = ? AND user_id = ?
		AND status = 'granted' AND consumed_at IS NOT NULL AND purchase_token_sealed IS NULL AND environment = 'Production'
		AND order_id = ?`, sha256Hex(token), s.userID, "GPA.3300-"+token[13:17]) != 1 {
		t.Fatal("purchase row")
	}
	again := h.postGoogle(s.access, packGoogle, token)
	if again.status != http.StatusOK || again.str("status") != "already_granted" || again.num("usage", "credits") != 50 ||
		h.play.ConsumeCalls.Load() != 1 {
		t.Fatalf("re-post: %s", again.raw)
	}

	test := playToken(2)
	h.playPurchase(s.userID, packGoogle, test, func(p *googleplay.ProductPurchase) { p.PurchaseType = new(int) })
	mustStatus(t, h.postGoogle(s.access, packGoogle, test), http.StatusOK, "")
	if h.text(`SELECT environment FROM iap_purchases WHERE transaction_id = ?`, sha256Hex(test)) != "Test" {
		t.Fatal("a license-test purchase is not marked Test")
	}
	if h.dbContains(token) || h.dbContains(test) || strings.Contains(h.logs.String(), token) {
		t.Fatal("the raw purchase token was stored or logged")
	}
}

// Pending: 202 және байланған жол, ештеңе берілмейді; төленгенде беріледі; болдырылмаса rejected.
func TestGooglePendingPurchase(t *testing.T) {
	h := newHarness(t, withIAP(t))
	h.openIAP()
	s := h.signIn("iap-pending@example.com")
	token := playToken(3)
	h.playPurchase(s.userID, packGoogle, token, func(p *googleplay.ProductPurchase) { p.PurchaseState = googleplay.StatePending })

	for i := 0; i < 2; i++ {
		res := h.postGoogle(s.access, packGoogle, token)
		if res.status != http.StatusAccepted || res.str("status") != "pending" || res.body["usage"] != nil {
			t.Fatalf("pending %d: %d %s", i, res.status, res.raw)
		}
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id = ? AND status = 'pending' AND units = 0 AND user_id = ?`,
		sha256Hex(token), s.userID) != 1 || h.scalar(`SELECT COUNT(*) FROM credit_grants WHERE user_id = ?`, s.userID) != 0 ||
		h.play.ConsumeCalls.Load() != 0 {
		t.Fatal("a pending purchase granted something")
	}
	other := h.signIn("iap-pending-other@example.com")
	mustStatus(t, h.postGoogle(other.access, packGoogle, token), http.StatusBadRequest, "PURCHASE_INVALID")

	h.play.Update(packGoogle, token, func(p *googleplay.ProductPurchase) { p.PurchaseState = googleplay.StatePurchased })
	paid := h.postGoogle(s.access, packGoogle, token)
	if paid.status != http.StatusOK || paid.str("status") != "granted" || paid.num("usage", "credits") != 50 {
		t.Fatalf("paid: %s", paid.raw)
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id = ? AND status = 'granted' AND units = 50
		AND consumed_at IS NOT NULL`, sha256Hex(token)) != 1 {
		t.Fatal("the pending row was not completed")
	}

	cancelled := playToken(4)
	h.playPurchase(s.userID, packGoogle, cancelled, func(p *googleplay.ProductPurchase) { p.PurchaseState = googleplay.StatePending })
	mustStatus(t, h.postGoogle(s.access, packGoogle, cancelled), http.StatusAccepted, "")
	h.play.Update(packGoogle, cancelled, func(p *googleplay.ProductPurchase) { p.PurchaseState = googleplay.StateCanceled })
	res := h.postGoogle(s.access, packGoogle, cancelled)
	if res.status != http.StatusBadRequest || res.str("error", "details", "reason") != "canceled" ||
		h.text(`SELECT status FROM iap_purchases WHERE transaction_id = ?`, sha256Hex(cancelled)) != "rejected" {
		t.Fatalf("cancelled: %s", res.raw)
	}
}

func TestGooglePurchasesThatDoNotVerifyGrantNothing(t *testing.T) {
	h := newHarness(t, withIAP(t))
	h.openIAP()
	s := h.signIn("iap-google-refused@example.com")
	other := h.signIn("iap-google-refused-other@example.com")
	put := func(seed int, change func(*googleplay.ProductPurchase)) string {
		token := playToken(seed)
		h.playPurchase(s.userID, packGoogle, token, change)
		return token
	}
	rewarded := 2
	for _, tc := range []struct {
		name, product, token, reason string
	}{
		{"another account", packGoogle, put(10, func(p *googleplay.ProductPurchase) {
			p.ObfuscatedExternalAccountID = h.iap.AccountToken(other.userID)
		}), "account_mismatch"},
		{"no account id", packGoogle, put(11, func(p *googleplay.ProductPurchase) { p.ObfuscatedExternalAccountID = "" }), "account_mismatch"},
		{"consumed elsewhere", packGoogle, put(12, func(p *googleplay.ProductPurchase) { p.ConsumptionState = googleplay.Consumed }), "already_consumed"},
		{"rewarded", packGoogle, put(13, func(p *googleplay.ProductPurchase) { p.PurchaseType = &rewarded }), "unsupported_type"},
		{"unknown token", packGoogle, playToken(14), "not_found"},
		{"unknown product", "pack_999", put(15, nil), "product_unknown"},
		{"malformed token", packGoogle, "bad token!", "malformed"},
	} {
		res := h.postGoogle(s.access, tc.product, tc.token)
		if res.status != http.StatusBadRequest || res.errorCode() != "PURCHASE_INVALID" ||
			res.str("error", "details", "reason") != tc.reason {
			t.Errorf("%s: %d %s", tc.name, res.status, res.raw)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM iap_purchases`) + h.scalar(`SELECT COUNT(*) FROM credit_grants`); n != 0 {
		t.Fatalf("refused purchases left %d row(s)", n)
	}
	if res := h.postGoogle(s.access, "", playToken(16)); res.status != http.StatusBadRequest || res.str("error", "details", "field") != "product_id" {
		t.Fatalf("no product id: %s", res.raw)
	}

	// Google is down: 503, nothing written, the app posts again later.
	token := put(17, nil)
	h.play.FailNext(1)
	mustStatus(t, h.postGoogle(s.access, packGoogle, token), http.StatusServiceUnavailable, "STORE_UNAVAILABLE")
	if h.scalar(`SELECT COUNT(*) FROM iap_purchases`) != 0 {
		t.Fatal("an unverified purchase was written")
	}
	mustStatus(t, h.postGoogle(s.access, packGoogle, token), http.StatusOK, "")
}

// Тұтыну сәтсіз болса, сатып алу бәрібір беріледі; жұмысшы кейін тұтынады.
func TestGoogleConsumeIsRetriedByTheWorker(t *testing.T) {
	h := newHarness(t, withIAP(t))
	h.openIAP()
	s := h.signIn("iap-consume@example.com")
	token := playToken(20)
	h.playPurchase(s.userID, packGoogle, token, nil)
	h.play.FailConsumes(1)

	res := h.postGoogle(s.access, packGoogle, token)
	if res.status != http.StatusOK || res.str("status") != "granted" || res.num("usage", "credits") != 50 {
		t.Fatalf("post: %s", res.raw)
	}
	sealed := h.text(`SELECT purchase_token_sealed FROM iap_purchases WHERE transaction_id = ? AND consumed_at IS NULL`, sha256Hex(token))
	if !strings.HasPrefix(sealed, "v1:") || strings.Contains(sealed, token) {
		t.Fatalf("the token waiting for the retry is not sealed: %q", sealed)
	}

	h.iap.Tick(context.Background()) // too fresh: the request itself just tried
	if h.play.ConsumeCalls.Load() != 1 {
		t.Fatalf("consume calls %d", h.play.ConsumeCalls.Load())
	}
	h.clock.Advance(2 * time.Minute)
	h.iap.Tick(context.Background())
	if h.play.Purchase(packGoogle, token).ConsumptionState != googleplay.Consumed ||
		h.scalar(`SELECT COUNT(*) FROM iap_purchases WHERE transaction_id = ? AND consumed_at IS NOT NULL
			AND purchase_token_sealed IS NULL`, sha256Hex(token)) != 1 {
		t.Fatal("the worker did not consume")
	}
	h.clock.Advance(2 * time.Minute)
	h.iap.Tick(context.Background())
	if h.play.ConsumeCalls.Load() != 2 {
		t.Fatalf("consumed again: %d calls", h.play.ConsumeCalls.Load())
	}
	if h.dbContains(token) {
		t.Fatal("the raw token reached the database")
	}
}

// Google қайтарған сатып алу: қалдығы алынады, қайталанған тексеру ештеңе өзгертпейді.
func TestGoogleVoidedPurchaseIsRevoked(t *testing.T) {
	h := newHarness(t, withIAP(t))
	h.openIAP()
	s := h.signIn("iap-voided@example.com")
	token := playToken(30)
	h.playPurchase(s.userID, packGoogle, token, nil)
	mustStatus(t, h.postGoogle(s.access, packGoogle, token), http.StatusOK, "")
	h.useQuota(s.userID, 7)
	if res := h.generate(s.access, "Пакеттен"); res.str("usage", "bucket") != "pack" {
		t.Fatalf("generation: %s", res.raw)
	}

	voidedAt := strconv.FormatInt(h.clock.Now().UnixMilli(), 10)
	h.play.Void(googleplay.VoidedPurchase{PurchaseToken: token, VoidedTimeMillis: voidedAt, OrderID: "GPA.v1", VoidedReason: 1})
	unknown := playToken(31)
	h.play.Void(googleplay.VoidedPurchase{PurchaseToken: unknown, VoidedTimeMillis: voidedAt, OrderID: "GPA.v2", VoidedReason: 7})
	revoked, err := h.iap.PollVoided(context.Background())
	if err != nil || revoked != 1 {
		t.Fatalf("poll: %d %v", revoked, err)
	}
	if since, _ := strconv.ParseInt(h.play.LastVoidedSince(), 10, 64); since > h.clock.Now().Add(-29*24*time.Hour).UnixMilli() {
		t.Fatalf("the first poll reads 30 days back, startTime %d", since)
	}
	if h.creditsLeft(s.access) != 0 || h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id = ? AND entry = 'revoke' AND delta = -49`, s.userID) != 1 ||
		h.text(`SELECT revoke_reason FROM iap_purchases WHERE transaction_id = ?`, sha256Hex(token)) != "voided:1" {
		t.Fatal("voided purchase not revoked")
	}
	if again, err := h.iap.PollVoided(context.Background()); err != nil || again != 0 {
		t.Fatalf("second poll: %d %v", again, err)
	}
	if h.scalar(`SELECT COUNT(*) FROM credit_ledger WHERE user_id = ? AND entry = 'revoke'`, s.userID) != 1 {
		t.Fatal("revoked twice")
	}
	h.playPurchase(s.userID, packGoogle, unknown, nil)
	late := h.postGoogle(s.access, packGoogle, unknown)
	if late.status != http.StatusBadRequest || late.str("error", "details", "reason") != "revoked" {
		t.Fatalf("a voided token was granted: %s", late.raw)
	}
}

// ---------------------------------------------------------------- admin

// Өнімдер: ережелер, бірегейлік, өзгермейтін өрістер, мұрағат және әр өзгерістің аудиті.
func TestAdminManagesIAPProductsWithAudit(t *testing.T) {
	h := newHarness(t, withIAP(t))
	admin := h.signInAdmin()
	with := func(base map[string]any, changes map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range changes {
			if v == nil {
				delete(out, k)
				continue
			}
			out[k] = v
		}
		return out
	}
	for _, tc := range []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"no units", with(packBody(), map[string]any{"units": 0}), "units"},
		{"too many units", with(packBody(), map[string]any{"units": 100001}), "units"},
		{"pass without hours", with(passBody(), map[string]any{"duration_hours": 0}), "duration_hours"},
		{"pass over 30 days", with(passBody(), map[string]any{"duration_hours": 721}), "duration_hours"},
		{"pack with hours", with(packBody(), map[string]any{"duration_hours": 24}), "duration_hours"},
		{"kind", with(packBody(), map[string]any{"kind": "subscription"}), "kind"},
		{"code", with(packBody(), map[string]any{"code": "Pack 50"}), "code"},
		{"apple id", with(packBody(), map[string]any{"apple_product_id": "bad id!"}), "apple_product_id"},
		{"google id", with(packBody(), map[string]any{"google_product_id": "Pack50"}), "google_product_id"},
		{"active without a store id", with(packBody(), map[string]any{"apple_product_id": nil, "google_product_id": nil}), "is_active"},
		{"name too long", with(packBody(), map[string]any{"name": map[string]any{"ru": strings.Repeat("я", 61)}}), "name.ru"},
	} {
		res := h.adminCall(admin, http.MethodPost, "/iap/products", tc.body)
		if res.status != http.StatusBadRequest || res.str("error", "details", "field") != tc.field {
			t.Errorf("%s: %d %s", tc.name, res.status, res.raw)
		}
	}
	if h.scalar(`SELECT COUNT(*) FROM iap_products`) != 0 {
		t.Fatal("an invalid product was saved")
	}

	noCSRF := h.do(http.MethodPost, "/api/v1/admin/iap/products", packBody(),
		map[string]string{"Cookie": h.cfg.Admin.CookieName + "=" + admin.cookie})
	mustStatus(t, noCSRF, http.StatusForbidden, "CSRF_MISMATCH")

	id := h.createProduct(admin, packBody())
	if meta := h.auditMeta("iap_product.create"); meta == nil || meta["code"] != "pack_50" || meta["units"] != float64(50) ||
		meta["apple_product_id"] != packApple {
		t.Fatalf("create audit %v", meta)
	}
	dupCode := h.adminCall(admin, http.MethodPost, "/iap/products", with(packBody(), map[string]any{
		"apple_product_id": "kz.aireply.other", "google_product_id": "other"}))
	if dupCode.status != http.StatusConflict || dupCode.str("error", "details", "field") != "code" {
		t.Fatalf("duplicate code: %s", dupCode.raw)
	}
	dupApple := h.adminCall(admin, http.MethodPost, "/iap/products", with(packBody(), map[string]any{
		"code": "pack_other", "google_product_id": "other"}))
	if dupApple.status != http.StatusConflict || dupApple.str("error", "details", "field") != "apple_product_id" {
		t.Fatalf("duplicate apple id: %s", dupApple.raw)
	}

	list := h.adminCall(admin, http.MethodGet, "/iap/products", nil)
	rows, _ := list.body["products"].([]any)
	if len(rows) != 1 {
		t.Fatalf("products %s", list.raw)
	}
	row := rows[0].(map[string]any)
	if row["sales"] != float64(0) || row["offered_ios"] != true || row["archived"] != false {
		t.Fatalf("row %v", row)
	}
	if iap, _ := list.body["iap"].(map[string]any); iap["apple"] != true || iap["google"] != true ||
		iap["notification_url"] != h.cfg.App.PublicBaseURL+"/api/v1/iap/apple/notifications" {
		t.Fatalf("iap status %v", list.body["iap"])
	}

	// The row as the page got it goes straight back, with one change.
	row["units"] = 60
	mustStatus(t, h.adminCall(admin, http.MethodPatch, "/iap/products/"+id, row), http.StatusOK, "")
	meta := h.auditMeta("iap_product.update")
	before, _ := meta["before"].(map[string]any)
	after, _ := meta["after"].(map[string]any)
	if before["units"] != float64(50) || after["units"] != float64(60) {
		t.Fatalf("update audit %v", meta)
	}
	for field, value := range map[string]any{"code": "pack_60", "kind": "pass"} {
		res := h.adminCall(admin, http.MethodPatch, "/iap/products/"+id, with(row, map[string]any{field: value, "duration_hours": 24}))
		if res.status != http.StatusBadRequest || res.str("error", "details", "field") != field ||
			res.str("error", "details", "reason") != "immutable" {
			t.Errorf("%s change: %s", field, res.raw)
		}
	}

	mustStatus(t, h.adminCall(admin, http.MethodPost, "/iap/products/"+id+"/archive", map[string]any{}), http.StatusOK, "")
	if meta := h.auditMeta("iap_product.archive"); meta == nil || meta["code"] != "pack_50" {
		t.Fatalf("archive audit %v", meta)
	}
	mustStatus(t, h.adminCall(admin, http.MethodPost, "/iap/products/"+id+"/archive", map[string]any{}), http.StatusConflict, "CONFLICT")
	mustStatus(t, h.adminCall(admin, http.MethodPatch, "/iap/products/"+id, row), http.StatusConflict, "CONFLICT")
	mustStatus(t, h.adminCall(admin, http.MethodPatch, "/iap/products/unknown", row), http.StatusNotFound, "NOT_FOUND")
	// An archived product keeps its store id: a new product cannot take it.
	reuse := h.adminCall(admin, http.MethodPost, "/iap/products", with(packBody(), map[string]any{"code": "pack_new"}))
	if reuse.status != http.StatusConflict {
		t.Fatalf("store id of an archived product reused: %s", reuse.raw)
	}

	mustStatus(t, h.adminCall(admin, http.MethodPost, "/settings/iap", map[string]any{"enabled": true}), http.StatusOK, "")
	if meta := h.auditMeta("settings.iap.update"); meta["before"] != false || meta["after"] != true {
		t.Fatalf("switch audit %v", meta)
	}
	settings := h.adminCall(admin, http.MethodGet, "/settings", nil)
	if iap, _ := settings.body["iap"].(map[string]any); iap["enabled"] != true || iap["configured"] != true {
		t.Fatalf("settings %s", settings.raw)
	}
}

// Сатып алулар тізімі: сүзгілер; қолданушы картасында тек метадерек.
func TestAdminSeesPurchasesWithoutContent(t *testing.T) {
	h := newHarness(t, withIAP(t))
	admin := h.openIAP()
	s := h.signIn("iap-admin-list@example.com")
	other := h.signIn("iap-admin-list-other@example.com")
	h.generate(s.access, secretMessage)
	mustStatus(t, h.postApple(s.access, h.appleTx(s.userID, "2000000000000500", packApple, nil)), http.StatusOK, "")
	token := playToken(40)
	h.playPurchase(s.userID, packGoogle, token, nil)
	mustStatus(t, h.postGoogle(s.access, packGoogle, token), http.StatusOK, "")
	mustStatus(t, h.postApple(other.access, h.appleTx(other.userID, "2000000000000501", passApple,
		func(tx *appstoretest.Transaction) { tx.Environment = "Sandbox" })), http.StatusOK, "")

	count := func(query string) float64 {
		res := h.adminCall(admin, http.MethodGet, "/iap/purchases"+query, nil)
		mustStatus(t, res, http.StatusOK, "")
		return res.num("total")
	}
	for query, want := range map[string]float64{
		"": 3, "?provider=google_play": 1, "?provider=app_store": 2, "?status=granted": 3, "?status=refunded": 0,
		"?user_id=" + s.userID: 2, "?environment=Sandbox": 1, "?provider=app_store&environment=Production": 1,
	} {
		if got := count(query); got != want {
			t.Errorf("%q: %v, want %v", query, got, want)
		}
	}
	if bad := h.adminCall(admin, http.MethodGet, "/iap/purchases?provider=paypal", nil); bad.status != http.StatusBadRequest ||
		bad.str("error", "details", "field") != "provider" {
		t.Fatalf("unknown provider: %s", bad.raw)
	}
	list := h.adminCall(admin, http.MethodGet, "/iap/purchases?user_id="+s.userID, nil)
	if strings.Contains(string(list.raw), "iap-admin-list@example.com") || strings.Contains(string(list.raw), token) {
		t.Fatalf("the list shows the address or the token: %s", list.raw)
	}

	detail := h.adminCall(admin, http.MethodGet, "/users/"+s.userID, nil)
	purchases, _ := detail.body["iap_purchases"].([]any)
	if len(purchases) != 2 {
		t.Fatalf("user detail purchases: %s", detail.raw)
	}
	first := purchases[0].(map[string]any)
	if first["status"] != "granted" || first["units"] != float64(50) || first["product_code"] != "pack_50" {
		t.Fatalf("purchase %v", first)
	}
	payload := string(detail.raw)
	for _, forbidden := range []string{secretMessage, "source_text", "reply", "message", "prompt", "conversation", token} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("user detail exposes %q", forbidden)
		}
	}
}
