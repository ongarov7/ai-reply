package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Дене шектері: қолтаңбалы транзакция ~5 КБ, хабарламада екі JWS бар.
const (
	maxTransactionBody  = 72 << 10
	maxNotificationBody = 256 << 10
	// notificationsPerMinute — App Store хабарламаларының IP бойынша шелегі (қайтару толқынына кең).
	notificationsPerMinute = 600
)

// registerIAP — пакеттер мен pass сатып алу (features.iap_ios, features.iap_android).
//
// Verifying a purchase needs a signed-in person but not the AI consent: no
// text goes anywhere. The two verification routes share a per-user bucket
// (RATE_IAP_PER_MINUTE); App Store Server Notifications are public and
// trusted only after their signature verifies.
func (s *Server) registerIAP(mux *http.ServeMux, perUser middleware.KeyFunc) {
	if s.iap == nil {
		return
	}
	limit := middleware.RateLimit(s.limiter, "iap", s.cfg.Limits.IAPPerMinute, time.Minute, perUser)
	ip := func(r *http.Request) string { return httpx.ClientIP(r, s.cfg.App.TrustProxy) }
	notifications := middleware.RateLimit(s.limiter, "iap_notifications", notificationsPerMinute, time.Minute, ip)
	mux.Handle("GET /api/v1/iap/products", s.requireUser(http.HandlerFunc(s.handleIAPProducts)))
	mux.Handle("POST /api/v1/iap/apple/transactions", s.requireUser(limit(http.HandlerFunc(s.handleAppleTransaction))))
	mux.Handle("POST /api/v1/iap/google/purchases", s.requireUser(limit(http.HandlerFunc(s.handleGooglePurchase))))
	mux.Handle("POST /api/v1/iap/apple/notifications", notifications(http.HandlerFunc(s.handleAppleNotification)))
}

// iapProductDTO — қолданбаға көрінетін өнім (бағасы дүкенде: StoreKit / Play Billing).
type iapProductDTO struct {
	Code           string `json:"code"`
	Kind           string `json:"kind"`
	Units          int    `json:"units"`
	DurationHours  int    `json:"duration_hours"`
	StoreProductID string `json:"store_product_id"`
	Name           string `json:"name"`
}

// handleIAPProducts — GET /api/v1/iap/products?platform=ios|android.
//
// platform falls back to the one in the access token. Only products an
// administrator configured for this store and enabled are listed; while
// sales are off, 403 IAP_UNAVAILABLE.
func (s *Server) handleIAPProducts(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	platform := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("platform")))
	if platform == "" {
		platform = PlatformFrom(r.Context())
	}
	if platform != domain.PlatformIOS && platform != domain.PlatformAndroid {
		httpx.Fail(w, domain.InvalidField("platform", "value"))
		return
	}
	products, err := s.iap.Products(r.Context(), platform)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	locale := domain.ResolveLanguage(user.PreferredLanguage, user.Locale)
	out := make([]iapProductDTO, 0, len(products))
	for _, p := range products {
		out = append(out, iapProductDTO{Code: p.Code, Kind: p.Kind, Units: p.Units, DurationHours: p.DurationHours,
			StoreProductID: p.StoreID(platform), Name: p.LocalizedName(locale)})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"products": out, "account_token": s.iap.AccountToken(user.ID)})
}

type appleTransactionRequest struct {
	SignedTransaction string `json:"signed_transaction"`
}

// handleAppleTransaction — POST /api/v1/iap/apple/transactions: StoreKit 2 jwsRepresentation.
//
// The app calls transaction.finish() only after 200; any other answer leaves
// the transaction unfinished and the app posts it again later.
func (s *Server) handleAppleTransaction(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body appleTransactionRequest
	if err := httpx.Decode(w, r, maxTransactionBody, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if strings.TrimSpace(body.SignedTransaction) == "" {
		httpx.Fail(w, domain.InvalidField("signed_transaction", "required"))
		return
	}
	res, err := s.iap.VerifyApple(r.Context(), user, body.SignedTransaction)
	s.writePurchase(w, r, user, res, err)
}

type googlePurchaseRequest struct {
	ProductID     string `json:"product_id"`
	PurchaseToken string `json:"purchase_token"`
}

// handleGooglePurchase — POST /api/v1/iap/google/purchases: Play Billing purchase token.
//
// The server consumes the purchase after granting it; the app never consumes.
func (s *Server) handleGooglePurchase(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body googlePurchaseRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	switch {
	case strings.TrimSpace(body.ProductID) == "":
		httpx.Fail(w, domain.InvalidField("product_id", "required"))
		return
	case strings.TrimSpace(body.PurchaseToken) == "":
		httpx.Fail(w, domain.InvalidField("purchase_token", "required"))
		return
	}
	res, err := s.iap.VerifyGoogle(r.Context(), user, strings.TrimSpace(body.ProductID), strings.TrimSpace(body.PurchaseToken))
	s.writePurchase(w, r, user, res, err)
}

type purchaseResponse struct {
	Status string    `json:"status"`
	Usage  *usageDTO `json:"usage,omitempty"`
}

// writePurchase — 200 granted | already_granted (жаңа балансымен), 202 pending, не қате.
func (s *Server) writePurchase(w http.ResponseWriter, r *http.Request, user domain.User, res iap.Result, err error) {
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	if res.Status == iap.StatusPending {
		httpx.JSON(w, http.StatusAccepted, purchaseResponse{Status: res.Status})
		return
	}
	entitlement, err := s.subs.Entitlement(r.Context(), user.ID)
	if err != nil {
		// The purchase is granted; the app refreshes /me for the balance.
		s.log.Error("usage after a purchase could not be read", "user_id", user.ID, "error", err.Error())
		httpx.JSON(w, http.StatusOK, purchaseResponse{Status: res.Status})
		return
	}
	usage := s.usage(entitlement)
	httpx.JSON(w, http.StatusOK, purchaseResponse{Status: res.Status, Usage: &usage})
}

// handleAppleNotification — POST /api/v1/iap/apple/notifications (App Store Server Notifications V2).
//
// The body is Apple's {"signedPayload": "..."}, read leniently (Apple may add
// fields). A payload that does not verify gets 400; a verified one always
// 200, processed once per notificationUUID. A database failure answers 500
// so Apple retries.
func (s *Server) handleAppleNotification(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxNotificationBody)
	var body struct {
		SignedPayload string `json:"signedPayload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		httpx.Fail(w, domain.ErrInvalidRequest)
		return
	}
	if strings.TrimSpace(body.SignedPayload) == "" {
		httpx.Fail(w, domain.InvalidField("signedPayload", "required"))
		return
	}
	if _, err := s.iap.HandleAppleNotification(r.Context(), body.SignedPayload); err != nil {
		if status, _, _ := httpx.Translate(err); status == http.StatusInternalServerError {
			s.log.Error("app store notification failed", "error", err.Error())
		}
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
