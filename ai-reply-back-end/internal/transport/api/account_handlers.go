package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
	"github.com/aireply/ai-reply-back-end/internal/users"
)

// deleteAccountRequest — денесі міндетті емес (DELETE денесіз де келеді).
type deleteAccountRequest struct {
	// AppleAuthorizationCode — iOS қолданбасы жою алдында Apple-ден алған жаңа код:
	// сервер сол арқылы Sign in with Apple токенін кері қайтарады.
	AppleAuthorizationCode string `json:"apple_authorization_code"`
}

// handleDeleteAccount — DELETE /api/v1/me және POST /api/v1/me/delete (Баптаулар ▸ Тіркелгі ▸ Тіркелгіні жою).
//
// The account and its server data are gone when this answers; every token of
// the account answers 401 from then on. The keyboard keeps typing without one.
func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body deleteAccountRequest
	if err := decodeOptional(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	result, err := s.users.DeleteAccount(r.Context(), user.ID, body.AppleAuthorizationCode, users.DeletedInApp)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"deleted": true, "apple_token_revoked": result.AppleTokenRevoked})
}

type deletionRequest struct {
	Email  string `json:"email"`
	Locale string `json:"locale"`
}

// handleAccountDeletionRequest — /account/delete бетінің бірінші қадамы: поштаға код.
// Жауап әрқашан {"ok": true}: тіркелгі бар-жоғы айтылмайды.
func (s *Server) handleAccountDeletionRequest(w http.ResponseWriter, r *http.Request) {
	var body deletionRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if err := s.auth.RequestAccountDeletion(r.Context(), body.Email, body.Locale); err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type deletionConfirm struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

// handleAccountDeletionConfirm — екінші қадам: код дұрыс болса, тіркелгі бірден жойылады.
func (s *Server) handleAccountDeletionConfirm(w http.ResponseWriter, r *http.Request) {
	var body deletionConfirm
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	user, err := s.auth.ConfirmAccountDeletion(r.Context(), body.Email, body.Code)
	if errors.Is(err, domain.ErrNotFound) {
		// The code was valid, the account went away meanwhile (deleted in the app).
		httpx.JSON(w, http.StatusOK, map[string]bool{"deleted": true})
		return
	}
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	if _, err := s.users.DeleteAccount(r.Context(), user.ID, "", users.DeletedOnWeb); err != nil &&
		!errors.Is(err, domain.ErrNotFound) {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// decodeOptional — бос дене рұқсат; әйтпесе httpx.Decode сияқты қатаң оқылады.
func decodeOptional(w http.ResponseWriter, r *http.Request, maxBytes int64, target any) error {
	if maxBytes <= 0 {
		maxBytes = 32 * 1024
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
	if err != nil {
		return domain.ErrInvalidRequest
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return httpx.Decode(w, r, maxBytes, target)
}
