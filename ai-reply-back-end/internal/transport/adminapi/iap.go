package adminapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// userPurchaseRows — қолданушы картасындағы сатып алулар саны.
const userPurchaseRows = 50

// registerIAP — «Покупки» беті: өнімдер, сатып алулар және Баптаулардағы ауыстырғыш.
func (s *Server) registerIAP(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/admin/iap/products", s.guard(s.handleIAPProducts))
	mux.Handle("POST /api/v1/admin/iap/products", s.guard(s.handleIAPProductCreate))
	mux.Handle("PATCH /api/v1/admin/iap/products/{id}", s.guard(s.handleIAPProductUpdate))
	mux.Handle("POST /api/v1/admin/iap/products/{id}/archive", s.guard(s.handleIAPProductArchive))
	mux.Handle("GET /api/v1/admin/iap/purchases", s.guard(s.handleIAPPurchases))
	mux.Handle("POST /api/v1/admin/settings/iap", s.guard(s.handleSaveIAP))
}

// iapPayload — интеграциялардың күйі, ауыстырғыш және App Store хабарламаларының мекенжайы.
func (s *Server) iapPayload(r *http.Request) map[string]any {
	if s.iap == nil {
		return nil
	}
	st := s.iap.Status()
	return map[string]any{
		"enabled": s.iap.Enabled(r.Context()), "configured": s.iap.AnyConfigured(),
		"apple": st.Apple, "google": st.Google, "apple_sandbox": st.AppleSandbox,
		"bundle_id": st.BundleID, "package_name": st.PackageName, "voided_polling": st.VoidedPolling,
		"available_ios":     s.iap.Available(r.Context(), domain.PlatformIOS),
		"available_android": s.iap.Available(r.Context(), domain.PlatformAndroid),
		// The URL to paste into App Store Connect ▸ App Information ▸ App Store Server Notifications (Version 2).
		"notification_url": s.cfg.App.PublicBaseURL + "/api/v1/iap/apple/notifications",
		"ranges": map[string]iap.Range{
			"units": iap.UnitsRange, "duration_hours": iap.DurationRange, "sort_order": iap.SortRange,
		},
	}
}

// productPayload — әкімші жіберетін өнім. Тізім жолының оқу өрістері де қабылданады (еленбейді).
type productPayload struct {
	Code            string            `json:"code"`
	Kind            string            `json:"kind"`
	Units           int               `json:"units"`
	DurationHours   int               `json:"duration_hours"`
	AppleProductID  string            `json:"apple_product_id"`
	GoogleProductID string            `json:"google_product_id"`
	Name            map[string]string `json:"name"`
	IsActive        bool              `json:"is_active"`
	SortOrder       int               `json:"sort_order"`

	ID             string `json:"id"`
	Archived       bool   `json:"archived"`
	ArchivedAt     string `json:"archived_at"`
	Sales          int    `json:"sales"`
	OfferedIOS     bool   `json:"offered_ios"`
	OfferedAndroid bool   `json:"offered_android"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func (p productPayload) input() iap.ProductInput {
	return iap.ProductInput{
		Code: p.Code, Kind: p.Kind, Units: p.Units, DurationHours: p.DurationHours,
		AppleProductID: p.AppleProductID, GoogleProductID: p.GoogleProductID, Name: p.Name,
		IsActive: p.IsActive, SortOrder: p.SortOrder,
	}
}

func (s *Server) productDTO(p domain.IAPProduct, sales int) map[string]any {
	loc := s.cfg.App.Location()
	name := map[string]string{}
	for _, l := range domain.Locales {
		name[l] = p.Name[l]
	}
	return map[string]any{
		"id": p.ID, "code": p.Code, "kind": p.Kind, "units": p.Units, "duration_hours": p.DurationHours,
		"apple_product_id": p.AppleProductID, "google_product_id": p.GoogleProductID, "name": name,
		"is_active": p.IsActive, "sort_order": p.SortOrder, "archived": p.ArchivedAt != nil,
		"archived_at": optionalTime(p.ArchivedAt, loc), "sales": sales,
		"offered_ios": p.Offered(domain.PlatformIOS), "offered_android": p.Offered(domain.PlatformAndroid),
		"created_at": p.CreatedAt.In(loc).Format("2006-01-02 15:04"),
		"updated_at": p.UpdatedAt.In(loc).Format("2006-01-02 15:04"),
	}
}

// productAudit — аудиттегі өнім (атаулар мен уақытсыз).
func productAudit(p domain.IAPProduct) map[string]any {
	return map[string]any{
		"code": p.Code, "kind": p.Kind, "units": p.Units, "duration_hours": p.DurationHours,
		"apple_product_id": p.AppleProductID, "google_product_id": p.GoogleProductID,
		"is_active": p.IsActive, "sort_order": p.SortOrder,
	}
}

func (s *Server) handleIAPProducts(w http.ResponseWriter, r *http.Request) {
	if s.iap == nil {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	products, sales, err := s.iap.AdminProducts(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]map[string]any, 0, len(products))
	for _, p := range products {
		out = append(out, s.productDTO(p, sales[p.Code]))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"products": out, "iap": s.iapPayload(r)})
}

func (s *Server) handleIAPProductCreate(w http.ResponseWriter, r *http.Request) {
	var body productPayload
	if err := httpx.Decode(w, r, 8192, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if s.iap == nil {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	p, err := s.iap.CreateProduct(r.Context(), body.input())
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.admin.Audit(r.Context(), adminFrom(r.Context()), s.ip(r), "iap_product.create", "iap_product", p.ID, productAudit(p))
	httpx.JSON(w, http.StatusCreated, map[string]any{"product": s.productDTO(p, 0)})
}

func (s *Server) handleIAPProductUpdate(w http.ResponseWriter, r *http.Request) {
	var body productPayload
	if err := httpx.Decode(w, r, 8192, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if s.iap == nil {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	before, after, err := s.iap.UpdateProduct(r.Context(), r.PathValue("id"), body.input())
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.admin.Audit(r.Context(), adminFrom(r.Context()), s.ip(r), "iap_product.update", "iap_product", after.ID,
		map[string]any{"before": productAudit(before), "after": productAudit(after)})
	httpx.JSON(w, http.StatusOK, map[string]any{"product": s.productDTO(after, 0)})
}

func (s *Server) handleIAPProductArchive(w http.ResponseWriter, r *http.Request) {
	if s.iap == nil {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	p, err := s.iap.ArchiveProduct(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.admin.Audit(r.Context(), adminFrom(r.Context()), s.ip(r), "iap_product.archive", "iap_product", p.ID,
		map[string]any{"code": p.Code, "was_active": p.IsActive})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// userPurchaseDTO — қолданушы картасындағы сатып алу: өнім коды, күй, уақыт.
//
// Store product ids and transaction ids stay on the Purchases page: the card
// is checked by the privacy guard, and a store id such as
// "kz.ai-reply.pack50" reads like content to it.
func (s *Server) userPurchaseDTO(p domain.IAPPurchase) map[string]any {
	loc := s.cfg.App.Location()
	return map[string]any{
		"id": p.ID, "provider": p.Provider, "environment": p.Environment, "product_code": p.ProductCode,
		"quantity": p.Quantity, "units": p.Units, "status": p.Status, "consumed": p.ConsumedAt != nil,
		"purchased_at": optionalTime(p.PurchasedAt, loc), "created_at": p.CreatedAt.In(loc).Format("2006-01-02 15:04"),
		"revoked_at": optionalTime(p.RevokedAt, loc), "revoke_reason": p.RevokeReason,
	}
}

// purchaseDTO — «Покупки» бетінің жолы: картадағы өрістер және дүкен идентификаторлары.
func (s *Server) purchaseDTO(p domain.IAPPurchase) map[string]any {
	out := s.userPurchaseDTO(p)
	out["store_product_id"], out["transaction_id"], out["order_id"] = p.StoreProductID, p.TransactionID, p.OrderID
	return out
}

func (s *Server) handleIAPPurchases(w http.ResponseWriter, r *http.Request) {
	if s.iap == nil {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	query := r.URL.Query()
	filter := repository.PurchaseFilter{
		Provider: query.Get("provider"), Status: query.Get("status"), Environment: query.Get("environment"),
		UserID: traits.Clamp(query.Get("user_id"), 64),
	}
	for _, check := range []struct {
		field, value string
		allowed      []string
	}{
		{"provider", filter.Provider, domain.IAPProviders},
		{"status", filter.Status, domain.IAPPurchaseStatuses},
		{"environment", filter.Environment, domain.IAPEnvironments},
	} {
		if check.value != "" && !traits.OneOf(check.value, check.allowed...) {
			httpx.Fail(w, domain.InvalidField(check.field, "value"))
			return
		}
	}
	limit := 50
	if v, err := strconv.Atoi(query.Get("limit")); err == nil && v > 0 && v <= 100 {
		limit = v
	}
	page := 1
	if v, err := strconv.Atoi(query.Get("page")); err == nil && v > 1 {
		page = v
	}
	filter.Page = traits.NewPage(limit, (page-1)*limit)
	rows, total, err := s.iap.Purchases(r.Context(), filter)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := s.purchaseDTO(row.Purchase)
		item["user_id"], item["identifier"] = row.User.ID, ""
		if row.User.ID != "" {
			item["identifier"] = maskIdentifier(row.User)
		}
		out = append(out, item)
	}
	stats, err := s.iap.Stats(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	groups := make([]map[string]any, 0, len(stats))
	for _, st := range stats {
		groups = append(groups, map[string]any{"provider": st.Provider, "environment": st.Environment,
			"status": st.Status, "count": st.Count, "units": st.Units})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"purchases": out, "total": total, "page": page, "limit": limit, "stats": groups,
	})
}

// handleSaveIAP — «Покупки в приложении» ауыстырғышы (аудитпен). Дүкен бапталмаса қосылмайды (409).
func (s *Server) handleSaveIAP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := httpx.Decode(w, r, 256, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if s.iap == nil {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	before, err := s.iap.SetEnabled(r.Context(), body.Enabled)
	if err != nil {
		if errors.Is(err, domain.ErrIAPUnavailable) {
			httpx.Error(w, http.StatusConflict, httpx.CodeIAPUnavailable,
				"No App Store or Google Play integration is configured on this server.",
				map[string]any{"reason": "store_not_configured"})
			return
		}
		s.fail(w, err)
		return
	}
	s.admin.Audit(r.Context(), adminFrom(r.Context()), s.ip(r), "settings.iap.update", "system_settings",
		iap.SettingEnabled, map[string]any{"before": before, "after": body.Enabled})
	httpx.JSON(w, http.StatusOK, s.iapPayload(r))
}
