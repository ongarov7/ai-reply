package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

type replyRequest struct {
	SourceText  string           `json:"source_text"`
	Instruction string           `json:"instruction"`
	Language    string           `json:"language"`
	TemplateID  string           `json:"template_id"`
	Template    *templateDTO     `json:"template"`
	Profile     *profileOverride `json:"profile"`
	Business    *workingHoursDTO `json:"business_context"`
	AppVersion  string           `json:"app_version"`
	Platform    string           `json:"platform"`
}

// profileOverride — клиент профильді жергілікті ұстаса, сол мәндер басым болады.
type profileOverride struct {
	Description   string       `json:"description"`
	Role          string       `json:"role"`
	PreferredTone string       `json:"preferred_tone"`
	Business      *businessDTO `json:"business"`
	// ReplyLanguage — "auto" | kk | ru | en | uz. Жаңа клиенттер тек
	// /api/v1/config ішінде features.reply_preferences=true болса жібереді.
	ReplyLanguage string `json:"reply_language"`
}

type replyResponse struct {
	Reply            string   `json:"reply"`
	DetectedLanguage string   `json:"detected_language,omitempty"`
	Usage            usageDTO `json:"usage"`
}

// handleReply — негізгі AI эндпоинті.
//
// Мәтін тек жадта өңделеді: сұраныс денесі журналға да, дерекқорға да жазылмайды.
func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body replyRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}

	profile, err := s.users.Profile(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	promptProfile := ai.Profile{
		Description:   profile.Description,
		Role:          profile.Role,
		PreferredTone: profile.PreferredTone,
		Business: ai.Business{
			Offering: profile.BusinessOffering,
			Summary:  profile.BusinessSummary,
			Rules:    profile.BusinessRules,
		},
	}
	if body.Profile != nil {
		if body.Profile.Description != "" {
			promptProfile.Description = traits.Clamp(body.Profile.Description, 1000)
		}
		if body.Profile.Role != "" {
			promptProfile.Role = traits.Clamp(body.Profile.Role, 120)
		}
		if traits.OneOf(body.Profile.PreferredTone, "natural", "friendly", "professional", "formal", "short") {
			promptProfile.PreferredTone = body.Profile.PreferredTone
		}
		if business := body.Profile.Business.toDomain(); !business.IsEmpty() {
			promptProfile.Business = business
		}
		// Only a known language code gets through; anything else means "follow
		// the incoming message", which is also what an old client gets.
		if _, known := ai.ReplyLanguages[body.Profile.ReplyLanguage]; known {
			promptProfile.ReplyLanguage = body.Profile.ReplyLanguage
		}
	}

	result, err := s.ai.Reply(r.Context(), ai.Request{
		User:        user,
		DeviceID:    DeviceFrom(r.Context()),
		SourceText:  body.SourceText,
		Instruction: body.Instruction,
		Language:    body.Language,
		TemplateID:  body.TemplateID,
		Profile:     promptProfile,
		Template:    body.Template.toDomain(promptProfile.PreferredTone),
		Business:    body.Business.toDomain(),
		Platform:    traits.Clamp(body.Platform, 16),
		AppVersion:  traits.Clamp(body.AppVersion, 32),
	})
	if err != nil {
		status, code, message := httpx.Translate(err)
		details := map[string]any{}
		if code == httpx.CodeDailyLimit || code == httpx.CodeMonthlyLimit {
			details["daily_limit"] = result.DailyLimit
			details["used_today"] = result.UsedToday
			details["resets_at"] = result.ResetsAt.Format(time.RFC3339)
		}
		// Still INVALID_REQUEST for old clients; new ones read the real limit
		// here instead of assuming the number they were built with.
		if errors.Is(err, domain.ErrSourceTooLong) {
			details["field"] = "source_text"
			details["max_characters"] = result.SourceLimit
		}
		if len(details) == 0 {
			details = nil
		}
		httpx.Error(w, status, code, message, details)
		return
	}

	httpx.JSON(w, http.StatusOK, replyResponse{
		Reply:            result.Text,
		DetectedLanguage: result.DetectedLanguage,
		Usage: usageDTO{
			DailyLimit:     result.DailyLimit,
			UsedToday:      result.UsedToday,
			RemainingToday: result.Remaining,
			ResetsAt:       result.ResetsAt.Format(time.RFC3339),
			Timezone:       s.cfg.App.Timezone,
		},
	})
}

// composeRequest — «Create» режимі: пайдаланушы не жазу керегін сипаттайды.
// source_text жоқ: бұл ешкімге жауап емес.
type composeRequest struct {
	Instruction string `json:"instruction"`
	// Language — қолданбаның интерфейс тілі (метадерек және тілі анық емес
	// нұсқауға арналған шешім). Хабарлама тілі нұсқаудың өз тілінен анықталады.
	Language string `json:"language"`
	// Regenerate — сол нұсқау бойынша басқа нұсқа сұралды.
	Regenerate bool   `json:"regenerate"`
	AppVersion string `json:"app_version"`
	Platform   string `json:"platform"`
}

type composeResponse struct {
	Text             string   `json:"text"`
	DetectedLanguage string   `json:"detected_language,omitempty"`
	Usage            usageDTO `json:"usage"`
}

// handleCompose — нұсқау бойынша жаңа хабарлама. Квота, токен есебі және
// қате кодтары /ai/reply-мен бірдей.
func (s *Server) handleCompose(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body composeRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}

	result, err := s.ai.Compose(r.Context(), ai.ComposeRequest{
		User:        user,
		DeviceID:    DeviceFrom(r.Context()),
		Instruction: body.Instruction,
		Language:    traits.Clamp(body.Language, 16),
		Regenerate:  body.Regenerate,
		Platform:    traits.Clamp(body.Platform, 16),
		AppVersion:  traits.Clamp(body.AppVersion, 32),
	})
	if err != nil {
		status, code, message := httpx.Translate(err)
		details := map[string]any{}
		if code == httpx.CodeDailyLimit || code == httpx.CodeMonthlyLimit {
			details["daily_limit"] = result.DailyLimit
			details["used_today"] = result.UsedToday
			details["resets_at"] = result.ResetsAt.Format(time.RFC3339)
		}
		switch {
		case errors.Is(err, domain.ErrInstructionMissing):
			details["field"] = "instruction"
		case errors.Is(err, domain.ErrInstructionTooLong):
			details["field"] = "instruction"
			details["max_characters"] = result.InstructionLimit
		}
		if len(details) == 0 {
			details = nil
		}
		httpx.Error(w, status, code, message, details)
		return
	}

	httpx.JSON(w, http.StatusOK, composeResponse{
		Text:             result.Text,
		DetectedLanguage: result.DetectedLanguage,
		Usage: usageDTO{
			DailyLimit:     result.DailyLimit,
			UsedToday:      result.UsedToday,
			RemainingToday: result.Remaining,
			ResetsAt:       result.ResetsAt.Format(time.RFC3339),
			Timezone:       s.cfg.App.Timezone,
		},
	})
}

type checkoutRequest struct {
	PlanID string `json:"plan_id"`
}

// handleCheckout — төлем бастау (demo режимінде де нақты жазба қалады).
func (s *Server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body checkoutRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	intent, err := s.payments.Start(r.Context(), user.ID, body.PlanID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"payment_id":   intent.PaymentID,
		"provider":     intent.Provider,
		"status":       intent.Status,
		"amount":       intent.Amount,
		"currency":     intent.Currency,
		"redirect_url": intent.RedirectURL,
		"demo":         intent.Demo,
	})
}

// handleConfirmPayment — төлемді растау және тарифті қосу.
func (s *Server) handleConfirmPayment(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	if _, err := s.payments.Confirm(r.Context(), user.ID, r.PathValue("id")); err != nil {
		httpx.Fail(w, err)
		return
	}
	entitlement, err := s.subs.Entitlement(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"subscription": s.subscriptionDTO(entitlement),
		"usage":        toUsageDTO(entitlement, s.cfg.App.Timezone),
	})
}
