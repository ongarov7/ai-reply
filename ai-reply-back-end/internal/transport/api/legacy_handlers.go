package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Ескі келісімшарт (Node нұсқасымен бірдей). Мақсаты: дүкендегі қолданбалар
// жаңартусыз жұмыс істей берсін. Жаңа клиенттер /api/v1 пайдаланады.

type legacyRegisterRequest struct {
	InstallID string `json:"install_id"`
}

type legacyRegisterResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// handleLegacyRegister — POST /v1/auth/register.
func (s *Server) handleLegacyRegister(w http.ResponseWriter, r *http.Request) {
	var body legacyRegisterRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		legacyError(w, http.StatusBadRequest, "invalid_request", nil)
		return
	}
	token, expires, err := s.auth.LegacyRegister(r.Context(), body.InstallID)
	if err != nil {
		legacyError(w, http.StatusBadRequest, "invalid_request", nil)
		return
	}
	httpx.JSON(w, http.StatusOK, legacyRegisterResponse{Token: token, ExpiresAt: expires.Unix()})
}

type legacyGenerateRequest struct {
	Message         string           `json:"message"`
	TemplateID      string           `json:"template_id"`
	KeyboardLang    string           `json:"keyboard_language"`
	UserInstruction string           `json:"user_instruction"`
	Profile         *legacyProfile   `json:"profile"`
	Template        *templateDTO     `json:"template"`
	BusinessContext *workingHoursDTO `json:"business_context"`
}

type legacyProfile struct {
	Description   string       `json:"description"`
	Role          string       `json:"role"`
	PreferredTone string       `json:"preferred_tone"`
	Business      *businessDTO `json:"business"`
}

type legacyGenerateResponse struct {
	Reply            string `json:"reply"`
	DetectedLanguage string `json:"detected_language,omitempty"`
}

// handleLegacyGenerate — POST /v1/reply/generate.
func (s *Server) handleLegacyGenerate(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())

	var body legacyGenerateRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		legacyError(w, http.StatusBadRequest, "invalid_request", nil)
		return
	}
	message := traits.CollapseSpaces(body.Message)
	if message == "" {
		legacyError(w, http.StatusBadRequest, "message_empty", nil)
		return
	}
	if length, limit := traits.RuneLen(body.Message), s.limits.Current(r.Context()).SourceChars; length > limit {
		legacyError(w, http.StatusRequestEntityTooLarge, "message_too_long", map[string]any{
			"limit": limit, "actual": length,
		})
		return
	}

	profile := ai.Profile{PreferredTone: "natural"}
	if body.Profile != nil {
		profile.Description = traits.Clamp(body.Profile.Description, 1000)
		profile.Role = traits.Clamp(body.Profile.Role, 120)
		if traits.OneOf(body.Profile.PreferredTone, "natural", "friendly", "professional", "formal", "short") {
			profile.PreferredTone = body.Profile.PreferredTone
		}
		profile.Business = body.Profile.Business.toDomain()
	}

	language := body.KeyboardLang
	if !traits.OneOf(language, "kk", "ru", "en", "uz") {
		language = "en"
	}

	result, err := s.ai.Reply(r.Context(), ai.Request{
		User:        user,
		SourceText:  body.Message,
		Instruction: body.UserInstruction,
		Language:    language,
		// keyboard_language — ескі клиенттегі пернетақта: тіл анық болмаса шешеді.
		InputLanguage: ai.InputLanguage(body.KeyboardLang),
		TemplateID:    traits.Clamp(body.TemplateID, 64),
		Profile:       profile,
		Template:      body.Template.toDomain(profile.PreferredTone),
		Business:      body.BusinessContext.toDomain(),
		Platform:      user.Platform,
	})
	if err != nil {
		status, code := legacyCode(err)
		legacyError(w, status, code, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, legacyGenerateResponse{
		Reply:            result.Text,
		DetectedLanguage: result.DetectedLanguage,
	})
}

// legacyCode — домендік қатені ескі код пен мәртебеге салыстырады.
func legacyCode(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrDailyLimit), errors.Is(err, domain.ErrMonthlyLimit),
		errors.Is(err, domain.ErrRateLimited):
		return http.StatusTooManyRequests, "rate_limited"
	case errors.Is(err, domain.ErrProviderTimeout):
		return http.StatusGatewayTimeout, "upstream_timeout"
	case errors.Is(err, domain.ErrEmptyCompletion):
		return http.StatusBadGateway, "empty_completion"
	case errors.Is(err, domain.ErrProviderDown):
		return http.StatusBadGateway, "upstream_unavailable"
	case errors.Is(err, domain.ErrInvalidRequest):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, domain.ErrAccountDisabled):
		return http.StatusForbidden, "unauthorized"
	default:
		return http.StatusInternalServerError, "internal"
	}
}

// legacyError — ескі конверт: {"error":{"code":...}}.
func legacyError(w http.ResponseWriter, status int, code string, extra map[string]any) {
	payload := map[string]any{"code": code}
	for k, v := range extra {
		payload[k] = v
	}
	if status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", strconv.Itoa(60))
		payload["retry_after_seconds"] = 60
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": payload})
}
