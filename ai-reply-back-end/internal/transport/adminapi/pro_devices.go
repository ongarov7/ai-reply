package adminapi

import (
	"errors"
	"net/http"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

func (s *Server) registerProDevices(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/admin/users/{id}/pro-devices", s.guard(s.handleProDevices))
	mux.Handle("POST /api/v1/admin/users/{id}/pro-devices/{authorization_id}/revoke", s.guard(s.handleRevokeProDevice))
}

func (s *Server) handleProDevices(w http.ResponseWriter, r *http.Request) {
	devices, events, err := s.admin.ProDevices(r.Context(), adminFrom(r.Context()), r.PathValue("id"))
	if s.proDeviceFailure(w, err) {
		return
	}
	if devices == nil {
		devices = []domain.ProDeviceAuthorization{}
	}
	if events == nil {
		events = []domain.ProDeviceEvent{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"devices": devices, "events": events})
}

func (s *Server) handleRevokeProDevice(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(w, r, 2048, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	err := s.admin.RevokeProDevice(r.Context(), adminFrom(r.Context()), r.PathValue("id"), r.PathValue("authorization_id"), body.Reason)
	if s.proDeviceFailure(w, err) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) proDeviceFailure(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, domain.ErrProDeviceManagementForbidden) {
		httpx.Error(w, http.StatusForbidden, "FORBIDDEN", "Administrator permission required.", nil)
	} else {
		httpx.Fail(w, err)
	}
	return true
}
