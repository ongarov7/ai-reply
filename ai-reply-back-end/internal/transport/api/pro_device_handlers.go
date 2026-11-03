package api

import (
	"net/http"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

func (s *Server) handleProDevices(w http.ResponseWriter, r *http.Request) {
	if s.proDevices == nil {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	u, _ := UserFrom(r.Context())
	devices, err := s.proDevices.List(r.Context(), u.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	if devices == nil {
		devices = []domain.ProDeviceAuthorization{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"devices": devices})
}
func (s *Server) handleRevokeProDevice(w http.ResponseWriter, r *http.Request) {
	if s.proDevices == nil {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	u, _ := UserFrom(r.Context())
	if err := s.proDevices.Revoke(r.Context(), u.ID, r.PathValue("authorization_id"), DeviceFrom(r.Context()), "user_release"); err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
