package api

import (
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/payments/bcc"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
	"net/http"
	"time"
)

// RegisterBCC keeps bank checkout out of the native digital-purchase flow.
// These routes exist only on an explicitly enabled non-production sandbox.
func (s *Server) RegisterBCC(mux *http.ServeMux, bank *bcc.CheckoutService) {
	if !bank.Enabled() {
		return
	}
	userKey := func(r *http.Request) string { u, _ := UserFrom(r.Context()); return u.ID }
	limit := middleware.RateLimit(s.limiter, "bcc_checkout", 10, time.Minute, userKey)
	mux.Handle("POST /api/v1/payments/bcc/checkout", s.requireUser(limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _ := UserFrom(r.Context())
		var body struct {
			ProductCode string `json:"product_code"`
		}
		if err := httpx.Decode(w, r, 2048, &body); err != nil {
			httpx.Fail(w, err)
			return
		}
		o, err := bank.Start(r.Context(), user.ID, body.ProductCode, r.Header.Get("Idempotency-Key"))
		if err != nil {
			httpx.Fail(w, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, o)
	}))))
	mux.Handle("GET /api/v1/payments/bcc/orders/{id}/status", s.requireUser(limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := UserFrom(r.Context())
		o, err := bank.Status(r.Context(), u.ID, r.PathValue("id"))
		if err != nil {
			httpx.Fail(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, o)
	}))))
	mux.Handle("POST /api/v1/payments/bcc/orders/{id}/reconcile", s.requireUser(limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := UserFrom(r.Context())
		o, err := bank.Reconcile(r.Context(), u.ID, r.PathValue("id"))
		if err != nil {
			httpx.Fail(w, err)
			return
		}
		httpx.JSON(w, http.StatusAccepted, o)
	}))))
	mux.HandleFunc("GET /api/v1/payments/bcc/orders/{id}/checkout", func(w http.ResponseWriter, r *http.Request) {
		bank.CheckoutPageWithClientIP(w, r, httpx.ClientIP(r, s.cfg.App.TrustProxy))
	})
	mux.HandleFunc("POST /api/v1/payments/bcc/orders/{id}/return", bank.Return)
	mux.HandleFunc("POST /api/v1/payments/bcc/notify", bank.Notify)
}
