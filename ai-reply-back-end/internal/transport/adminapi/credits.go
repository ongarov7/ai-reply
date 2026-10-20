package adminapi

import (
	"net/http"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/credits"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// grantsPerHour — бір әкімшінің сағатына қолмен беретін кредиттері (қате басудан қорғаныс).
const grantsPerHour = 60

// registerCredits — сәлемдесу бонусының баптауы, қолмен берілетін кредит және оны тоқтату.
func (s *Server) registerCredits(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/admin/settings/welcome-bonus", s.guard(s.handleSaveWelcomeBonus))
	mux.Handle("POST /api/v1/admin/users/{id}/credits", s.guard(s.handleGrantCredits))
	mux.Handle("POST /api/v1/admin/users/{id}/credits/{grant_id}/revoke", s.guard(s.handleRevokeCredits))
}

// welcomeBonusPayload — бонус баптауы: ағымдағы мән, әдепкісі, ауқымы және қай сәттен бастап.
func (s *Server) welcomeBonusPayload(r *http.Request) map[string]any {
	if s.credits == nil {
		return nil
	}
	w := s.credits.WelcomeSettings(r.Context())
	since := ""
	if !w.Since.IsZero() {
		since = w.Since.In(s.cfg.App.Location()).Format("2006-01-02 15:04")
	}
	return map[string]any{
		"units": w.Units, "default": credits.DefaultWelcomeUnits, "since": since,
		"range": credits.WelcomeRange,
	}
}

// handleSaveWelcomeBonus — жаңа тіркелгілердің бонусы (0 — өшірулі). Берілген бонустар өзгермейді.
func (s *Server) handleSaveWelcomeBonus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Units int `json:"units"`
	}
	if err := httpx.Decode(w, r, 256, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if s.credits == nil {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	before := s.credits.WelcomeSettings(r.Context()).Units
	saved, err := s.credits.SetWelcomeUnits(r.Context(), body.Units)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.admin.Audit(r.Context(), adminFrom(r.Context()), s.ip(r), "settings.welcome_bonus.update", "system_settings",
		credits.SettingWelcomeUnits, map[string]any{"before": before, "after": saved.Units})
	httpx.JSON(w, http.StatusOK, s.welcomeBonusPayload(r))
}

// handleGrantCredits — әкімші қолмен береді: units 1…10000, түсініктеме міндетті (≤ 200 таңба).
func (s *Server) handleGrantCredits(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Units  int    `json:"units"`
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(w, r, 2048, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if s.credits == nil {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	adminUser := adminFrom(r.Context())
	if !s.allow(w, "admin_credit_grant:"+adminUser.ID, grantsPerHour, time.Hour,
		"Too many credit grants. Try again later.") {
		return
	}
	userID := r.PathValue("id")
	grant, err := s.credits.AdminGrant(r.Context(), userID, body.Units, body.Reason, adminUser.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.admin.Audit(r.Context(), adminUser, s.ip(r), "credits.grant", "user", userID, map[string]any{
		"grant_id": grant.ID, "kind": grant.Kind, "units": grant.UnitsTotal, "reason": grant.Reason,
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"grant": s.grantDTO(grant)})
}

// handleRevokeCredits — кредитті тоқтату: қалдығы нөлге түседі, жұмсалғаны қалады.
func (s *Server) handleRevokeCredits(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(w, r, 2048, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if s.credits == nil {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	adminUser := adminFrom(r.Context())
	userID := r.PathValue("id")
	grant, revoked, err := s.credits.Revoke(r.Context(), userID, r.PathValue("grant_id"), body.Reason, adminUser.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.admin.Audit(r.Context(), adminUser, s.ip(r), "credits.revoke", "user", userID, map[string]any{
		"grant_id": grant.ID, "kind": grant.Kind, "units_revoked": revoked, "reason": grant.RevokeReason,
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"grant": s.grantDTO(grant), "units_revoked": revoked})
}

// actorLabel — кім берді: "system" не "admin" (әкімшінің идентификаторы аудитте).
func actorLabel(actor string) string {
	if actor == "" || actor == "system" {
		return "system"
	}
	return "admin"
}

// grantDTO — кредит жолы (әкімші картасы).
func (s *Server) grantDTO(g domain.CreditGrant) map[string]any {
	loc := s.cfg.App.Location()
	return map[string]any{
		"id": g.ID, "kind": g.Kind, "units_total": g.UnitsTotal, "units_remaining": g.UnitsRemaining,
		"status": g.Status, "created_at": g.CreatedAt.In(loc).Format("2006-01-02 15:04"),
		"expires_at": optionalTime(g.ExpiresAt, loc), "reason": g.Reason, "by": actorLabel(g.GrantedBy),
		"revoked_at": optionalTime(g.RevokedAt, loc), "revoke_reason": g.RevokeReason,
	}
}

// balancesPayload — тарифтен тыс қалдық (бонус, кредиттер, pass) және барлығы.
func (s *Server) balancesPayload(e domain.Entitlement) map[string]any {
	b := e.Balance
	out := map[string]any{
		"welcome": nil, "credits": b.Credits(), "admin": b.Admin, "packs": b.Packs, "pass": nil,
		"remaining_total": e.RemainingTotal(),
	}
	if b.HasWelcome {
		out["welcome"] = map[string]any{"total": b.WelcomeTotal, "remaining": b.WelcomeRemaining}
	}
	if b.PassActive && b.PassExpiresAt != nil {
		out["pass"] = map[string]any{
			"expires_at": b.PassExpiresAt.In(s.cfg.App.Location()).Format("2006-01-02 15:04"),
			"remaining":  b.PassRemaining,
		}
	}
	return out
}

// ledgerPayload — журналдың соңғы жазбалары (брон идентификаторынсыз).
func (s *Server) ledgerPayload(entries []domain.LedgerEntry) []map[string]any {
	loc := s.cfg.App.Location()
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"at": e.CreatedAt.In(loc).Format("2006-01-02 15:04"), "entry": e.Entry, "kind": e.Kind,
			"delta": e.Delta, "by": actorLabel(e.Actor), "reason": e.Reason,
		})
	}
	return out
}
