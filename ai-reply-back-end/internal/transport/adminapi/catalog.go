package adminapi

import (
	"errors"
	"net/http"

	"github.com/aireply/ai-reply-back-end/internal/catalog"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

func (s *Server) registerCatalog(mux *http.ServeMux) {
	if s.catalog == nil {
		return
	}
	mux.Handle("GET /api/v1/admin/billing/catalog", s.guard(s.handleBillingCatalog))
	mux.Handle("PUT /api/v1/admin/billing/catalog/draft", s.guard(s.handleBillingDraft))
	mux.Handle("POST /api/v1/admin/billing/catalog/publish", s.guard(s.handleBillingPublish))
	if s.bank.Enabled() {
		mux.Handle("GET /api/v1/admin/billing/orders", s.guard(func(w http.ResponseWriter, r *http.Request) {
			orders, err := s.bank.Orders(r.Context())
			if err != nil {
				httpx.Fail(w, err)
				return
			}
			httpx.JSON(w, http.StatusOK, map[string]any{"orders": orders})
		}))
		mux.Handle("POST /api/v1/admin/billing/orders/{id}/refund", s.guard(func(w http.ResponseWriter, r *http.Request) {
			order, err := s.bank.Refund(r.Context(), adminFrom(r.Context()).Email, r.PathValue("id"))
			if err != nil {
				httpx.Fail(w, err)
				return
			}
			s.admin.Audit(r.Context(), adminFrom(r.Context()), s.ip(r), "bcc.sandbox.refund", "billing_order", order.ID, map[string]any{"status": order.Status})
			httpx.JSON(w, http.StatusAccepted, order)
		}))
	}

}
func (s *Server) handleBillingCatalog(w http.ResponseWriter, r *http.Request) {
	published, err := s.catalog.Published(r.Context())
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	draft, err := s.catalog.Draft(r.Context())
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		httpx.Fail(w, err)
		return
	}
	history, err := s.catalog.History(r.Context())
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	audit, err := s.catalog.AuditHistory(r.Context())
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	var pending any
	if draft.ID != 0 {
		pending = draft
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"published": published, "draft": pending, "history": history, "audit": audit, "store_price_source": "StoreKit", "store_sync": "manual_app_store_connect", "store_checkout_prices_verified": false, "bank_enabled": s.bank.Enabled()})
}
func (s *Server) handleBillingDraft(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Revision int               `json:"revision"`
		Products []catalog.Product `json:"products"`
	}
	if err := httpx.Decode(w, r, 64<<10, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	admin := adminFrom(r.Context())
	v, err := s.catalog.SaveDraft(r.Context(), admin.Email, body.Revision, body.Products)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"draft": v})
}
func (s *Server) handleBillingPublish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       int64 `json:"id"`
		Revision int   `json:"revision"`
	}
	if err := httpx.Decode(w, r, 1024, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	admin := adminFrom(r.Context())
	v, err := s.catalog.Publish(r.Context(), admin.Email, body.ID, body.Revision)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.admin.Audit(r.Context(), admin, s.ip(r), "billing_catalog.publish", "billing_catalog", "", map[string]any{"version": v.ID})
	httpx.JSON(w, http.StatusOK, map[string]any{"published": v})
}
func (s *Server) catalogManaged(code string) bool {
	if s.catalog == nil {
		return false
	}
	switch code {
	case "free", "base_monthly", "base_yearly", "pro_monthly", "pro_yearly", "pack30":
		return true
	}
	return false
}
