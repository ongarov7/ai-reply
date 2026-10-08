package api

import (
	"net/http"

	"github.com/aireply/ai-reply-back-end/internal/reports"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// aiReportRequest — қолданба мен пернетақтадан келетін шағым.
type aiReportRequest struct {
	Mode    string `json:"mode"`
	Reason  string `json:"reason"`
	Comment string `json:"comment"`
	// Text — жасалған мәтін, тек адам «мәтінді жіберу» қосқышын қалдырса.
	Text       string `json:"text"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
}

// handleAIReport — POST /api/v1/ai/reports (features.ai_reports).
//
// The body is never logged; the report keeps only what the person chose to
// send and reaches the administrators' Reports page.
func (s *Server) handleAIReport(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body aiReportRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	report, err := s.reports.Create(r.Context(), user.ID, reports.Input{
		Mode: body.Mode, Reason: body.Reason, Comment: body.Comment, Text: body.Text,
		Platform: body.Platform, AppVersion: body.AppVersion,
	})
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]string{"id": report.ID})
}
