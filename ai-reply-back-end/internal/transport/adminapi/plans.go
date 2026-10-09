package adminapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/limits"
	"github.com/aireply/ai-reply-back-end/internal/plans"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

type planPayload struct {
	Code         string            `json:"code"`
	Name         map[string]string `json:"name"`
	Description  map[string]string `json:"description"`
	Price        int64             `json:"price"`
	Currency     string            `json:"currency"`
	DailyLimit   int               `json:"daily_message_limit"`
	MonthlyLimit int               `json:"monthly_message_limit"`
	PeriodDays   int               `json:"period_days"`
	IsFree       bool              `json:"is_free"`
	IsActive     bool              `json:"is_active"`
	// IsVisible — nil keeps the stored value (an admin tab opened before this
	// field existed); on create, nil means visible only for a free plan.
	IsVisible *bool `json:"is_visible"`
	SortOrder int   `json:"sort_order"`

	// Read-only fields of the list row. The Plans page sends the row it was
	// given straight back, so they are accepted here and ignored.
	ID          string `json:"id"`
	PriceText   string `json:"price_text"`
	Subscribers int    `json:"subscribers"`
	Archived    bool   `json:"archived"`
	Listed      bool   `json:"listed"`
	Purchasable bool   `json:"purchasable"`
	IsDefault   bool   `json:"is_default"`
}

func (p planPayload) toDomain(id string, visible bool) domain.Plan {
	if p.IsVisible != nil {
		visible = *p.IsVisible
	}
	plan := domain.Plan{
		ID: id, Code: p.Code, Name: map[string]string{}, Description: map[string]string{},
		Price: p.Price, Currency: p.Currency, DailyLimit: p.DailyLimit,
		MonthlyLimit: p.MonthlyLimit, PeriodDays: p.PeriodDays,
		IsFree: p.IsFree, IsActive: p.IsActive, IsVisible: visible, SortOrder: p.SortOrder,
	}
	for _, locale := range domain.Locales {
		plan.Name[locale] = traits.Clamp(p.Name[locale], 60)
		plan.Description[locale] = traits.Clamp(p.Description[locale], 240)
	}
	return plan
}

func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	list, err := s.admin.Plans(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		count, _ := s.admin.PlanUsage(r.Context(), p.ID)
		isDefault, _ := s.admin.IsDefaultPlan(r.Context(), p.ID)
		out = append(out, map[string]any{
			"id": p.ID, "code": p.Code, "name": p.Name, "description": p.Description,
			"price": p.Price, "price_text": traits.FormatMoney(p.Price, p.Currency),
			"currency": p.Currency, "daily_message_limit": p.DailyLimit,
			"monthly_message_limit": p.MonthlyLimit, "period_days": p.PeriodDays,
			"is_free": p.IsFree, "is_active": p.IsActive, "is_visible": p.IsVisible, "sort_order": p.SortOrder,
			"subscribers": count, "archived": p.ArchivedAt != nil,
			// listed — customers see it; purchasable — it can be bought right now.
			"listed": p.Listed(), "purchasable": s.purchasable(r, p), "is_default": isDefault,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"plans": out, "purchases": s.purchasesPayload(r)})
}

func (s *Server) purchasable(r *http.Request, p domain.Plan) bool {
	return s.payments != nil && s.payments.Purchasable(r.Context(), p)
}

// planFail — әдепкі тарифті өшіру әрекетін анық себеппен қайтарады.
func (s *Server) planFail(w http.ResponseWriter, err error) {
	if errors.Is(err, plans.ErrDefaultPlan) {
		httpx.Error(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "The default free plan must stay enabled.",
			map[string]any{"field": "is_active", "reason": "default_plan"})
		return
	}
	httpx.Fail(w, err)
}

func (s *Server) handlePlanCreate(w http.ResponseWriter, r *http.Request) {
	var body planPayload
	if err := httpx.Decode(w, r, 8192, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	plan, err := s.admin.CreatePlan(r.Context(), adminFrom(r.Context()), s.ip(r), body.toDomain("", body.IsFree))
	if err != nil {
		s.planFail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"id": plan.ID})
}

func (s *Server) handlePlanUpdate(w http.ResponseWriter, r *http.Request) {
	var body planPayload
	if err := httpx.Decode(w, r, 8192, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	current, err := s.admin.Plan(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	if err := s.admin.UpdatePlan(r.Context(), adminFrom(r.Context()), s.ip(r),
		body.toDomain(current.ID, current.IsVisible)); err != nil {
		s.planFail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePlanArchive(w http.ResponseWriter, r *http.Request) {
	if err := s.admin.ArchivePlan(r.Context(), adminFrom(r.Context()), s.ip(r), r.PathValue("id")); err != nil {
		s.planFail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- audit

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	page := 1
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 1 {
		page = v
	}
	limit := 50
	entries, total, err := s.admin.AuditLog(r.Context(), traits.NewPage(limit, (page-1)*limit))
	if err != nil {
		s.fail(w, err)
		return
	}
	loc := s.cfg.App.Location()
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"at": e.CreatedAt.In(loc).Format("2006-01-02 15:04"), "admin": e.AdminEmail,
			"action": e.Action, "entity_type": e.EntityType, "entity_id": e.EntityID,
			"ip": e.IP, "metadata": e.Metadata,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"entries": out, "total": total, "page": page, "limit": limit,
	})
}

// ---------------------------------------------------------------- settings

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	pricing, err := s.admin.Pricing(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	loc := s.cfg.App.Location()
	rows := make([]map[string]any, 0, len(pricing))
	for _, p := range pricing {
		rows = append(rows, map[string]any{
			"id": p.ID, "model": p.Model, "input_per_1m": p.InputPer1M,
			"output_per_1m": p.OutputPer1M, "currency": p.Currency,
			"effective_from": p.EffectiveFrom.In(loc).Format("2006-01-02"),
		})
	}
	aiLimits, err := s.aiLimitsPayload(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"env": s.cfg.App.Env, "timezone": s.cfg.App.Timezone,
		"demo_mode": s.cfg.Auth.DemoMode, "payment_mode": s.cfg.Payments.Mode,
		"model": s.cfg.OpenAI.Model, "legacy_api": s.cfg.Auth.LegacyEnabled,
		"access_ttl": s.cfg.Auth.AccessTTL.String(), "refresh_ttl": s.cfg.Auth.RefreshTTL.String(),
		// Kept for any older admin bundle still open in a browser tab.
		"source_limit": aiLimits.Current.SourceChars,
		"ai_limits":    aiLimits,
		"pricing":      rows,
		"purchases":    s.purchasesPayload(r),
	})
}

// purchasesPayload — сатып алу күйі: интеграция бар ма және әкімші қосқан ба.
//
// checkout_available is false until a live provider (StoreKit, Play Billing)
// is wired in; while it is false the switch cannot be turned on.
func (s *Server) purchasesPayload(r *http.Request) map[string]any {
	if s.payments == nil {
		return map[string]any{"enabled": false, "checkout_available": false, "provider": "", "live": false}
	}
	return map[string]any{
		"enabled":            s.payments.PurchasesEnabled(r.Context()),
		"checkout_available": s.payments.CheckoutAvailable(),
		"provider":           s.payments.Provider(),
		"live":               s.payments.Live(),
	}
}

// handleSavePurchases — «Сатып алу» ауыстырғышы (аудитпен).
func (s *Server) handleSavePurchases(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := httpx.Decode(w, r, 256, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if s.payments == nil {
		httpx.Fail(w, domain.ErrPurchasesDisabled)
		return
	}
	before := s.payments.PurchasesEnabled(r.Context())
	if err := s.payments.SetPurchasesEnabled(r.Context(), body.Enabled); err != nil {
		if errors.Is(err, domain.ErrPurchasesDisabled) {
			httpx.Error(w, http.StatusConflict, httpx.CodePurchasesDisabled,
				"No verified billing integration is configured on this server.",
				map[string]any{"reason": "checkout_unavailable"})
			return
		}
		s.fail(w, err)
		return
	}
	s.admin.Audit(r.Context(), adminFrom(r.Context()), s.ip(r), "settings.purchases.update", "system_settings",
		"purchases_enabled", map[string]any{"before": before, "after": body.Enabled})
	httpx.JSON(w, http.StatusOK, s.purchasesPayload(r))
}

// aiLimitsDTO — ағымдағы мәндер, әдепкілер, қайсысы әкімшіден және рұқсат ауқымы.
type aiLimitsDTO struct {
	Current    limits.Limits           `json:"current"`
	Defaults   limits.Limits           `json:"defaults"`
	Overridden map[string]bool         `json:"overridden"`
	Ranges     map[string]limits.Range `json:"ranges"`
}

func (s *Server) aiLimitsPayload(r *http.Request) (aiLimitsDTO, error) {
	overridden, err := s.limits.Overridden(r.Context())
	if err != nil {
		return aiLimitsDTO{}, err
	}
	return aiLimitsDTO{
		Current:    s.limits.Current(r.Context()),
		Defaults:   s.limits.Defaults(),
		Overridden: overridden,
		Ranges: map[string]limits.Range{
			limits.KeySourceChars:      limits.SourceRange,
			limits.KeyInstructionChars: limits.InstructionRange,
			limits.KeyMaxOutputTokens:  limits.OutputTokenRange,
		},
	}, nil
}

// handleSaveLimits — AI шектеулерін өзгерту (аудитпен, ескі/жаңа мәндерімен).
func (s *Server) handleSaveLimits(w http.ResponseWriter, r *http.Request) {
	var body limits.Limits
	if err := httpx.Decode(w, r, 1024, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	before := s.limits.Current(r.Context())
	saved, err := s.limits.Update(r.Context(), body)
	if err != nil {
		var fieldErr *limits.FieldError
		if errors.As(err, &fieldErr) {
			httpx.Error(w, http.StatusBadRequest, httpx.CodeInvalidRequest, fieldErr.Error(), map[string]any{
				"field": fieldErr.Field, "min": fieldErr.Range.Min, "max": fieldErr.Range.Max,
			})
			return
		}
		s.fail(w, err)
		return
	}
	s.admin.Audit(r.Context(), adminFrom(r.Context()), s.ip(r), "settings.ai_limits.update", "system_settings", "ai_limits",
		map[string]any{"before": before, "after": saved})
	payload, err := s.aiLimitsPayload(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, payload)
}

func (s *Server) handleSavePricing(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model         string  `json:"model"`
		InputPer1M    float64 `json:"input_per_1m"`
		OutputPer1M   float64 `json:"output_per_1m"`
		EffectiveFrom string  `json:"effective_from"`
	}
	if err := httpx.Decode(w, r, 2048, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if strings.TrimSpace(body.Model) == "" || body.InputPer1M < 0 || body.OutputPer1M < 0 {
		httpx.Fail(w, domain.ErrInvalidRequest)
		return
	}
	effective := time.Now().UTC()
	if raw := strings.TrimSpace(body.EffectiveFrom); raw != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", raw, s.cfg.App.Location()); err == nil {
			effective = parsed.UTC()
		}
	}
	if err := s.admin.SavePricing(r.Context(), adminFrom(r.Context()), s.ip(r), repository.Pricing{
		Model: strings.TrimSpace(body.Model), InputPer1M: body.InputPer1M,
		OutputPer1M: body.OutputPer1M, Currency: "USD", EffectiveFrom: effective,
	}); err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
