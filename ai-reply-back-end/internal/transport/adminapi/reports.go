package adminapi

import (
	"net/http"
	"strconv"

	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// handleReports — AI жауаптарына шағымдар, жаңасы алдымен (?status=open|resolved, бос — бәрі).
//
// The text is shown: the person sent it to be read. The account appears only
// as a masked identifier and its id, for opening the user page.
func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page := 1
	if v, err := strconv.Atoi(query.Get("page")); err == nil && v > 1 {
		page = v
	}
	limit := 25
	rows, total, err := s.reports.List(r.Context(), query.Get("status"), traits.NewPage(limit, (page-1)*limit))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	loc := s.cfg.App.Location()
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		report := row.Report
		out = append(out, map[string]any{
			"id": report.ID, "user_id": report.UserID, "identifier": maskIdentifier(row.User),
			"mode": report.Mode, "reason": report.Reason, "comment": report.Comment, "text": report.Text,
			"platform": report.Platform, "app_version": report.AppVersion, "status": report.Status,
			"created_at":  report.CreatedAt.In(loc).Format("2006-01-02 15:04"),
			"resolved_at": optionalTime(report.ResolvedAt, loc), "resolved_by": row.ResolvedByEmail,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"reports": out, "total": total, "page": page, "limit": limit})
}

// handleResolveReport — шағымды шешілді деп белгілеу (аудитке жазылады).
func (s *Server) handleResolveReport(w http.ResponseWriter, r *http.Request) {
	adminUser := adminFrom(r.Context())
	id := r.PathValue("id")
	changed, err := s.reports.Resolve(r.Context(), id, adminUser.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	if changed {
		s.admin.Audit(r.Context(), adminUser, s.ip(r), "report.resolve", "ai_report", id, nil)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "changed": changed})
}
