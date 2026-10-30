package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

type replyRequest struct {
	SourceText  string `json:"source_text"`
	Instruction string `json:"instruction"`
	Language    string `json:"language"`
	// InputLanguage — Reply басылғандағы пернетақта (kk | ru | en). Жаңа
	// клиенттер тек features.sender_profile=true болса жібереді; белгісіз мән
	// елеусіз қалады.
	InputLanguage string           `json:"input_language"`
	TemplateID    string           `json:"template_id"`
	Template      *templateDTO     `json:"template"`
	Profile       *profileOverride `json:"profile"`
	Business      *workingHoursDTO `json:"business_context"`
	AppVersion    string           `json:"app_version"`
	Platform      string           `json:"platform"`
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
	// GrammaticalGender — male | female | unspecified (features.sender_profile).
	// Белгісіз мән елеусіз қалады, сақталған профиль мәні қолданылады.
	GrammaticalGender string `json:"grammatical_gender"`
}

type replyResponse struct {
	Reply            string   `json:"reply"`
	DetectedLanguage string   `json:"detected_language,omitempty"`
	Usage            usageDTO `json:"usage"`
}

// idempotencyKeyPattern — Idempotency-Key пішіні (transcribe request_id-мен бірдей).
var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// idempotencyKey — Idempotency-Key тақырыбы: "" — жоқ, қате пішін — 400 (idempotency_key, format).
//
// A header, not a body field: bodies are decoded strictly, and an older
// server simply ignores an unknown header (features.idempotent_generation).
func idempotencyKey(r *http.Request) (string, error) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		return "", nil
	}
	if !idempotencyKeyPattern.MatchString(key) {
		return "", domain.InvalidField("idempotency_key", "format")
	}
	return key, nil
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
	key, err := idempotencyKey(r)
	if err != nil {
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
		GrammaticalGender: profile.GrammaticalGender,
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
		if domain.IsGrammaticalGender(body.Profile.GrammaticalGender) {
			promptProfile.GrammaticalGender = body.Profile.GrammaticalGender
		}
	}

	result, err := s.ai.Reply(r.Context(), ai.Request{
		User:           user,
		DeviceID:       DeviceFrom(r.Context()),
		SourceText:     body.SourceText,
		Instruction:    body.Instruction,
		Language:       body.Language,
		InputLanguage:  ai.InputLanguage(body.InputLanguage),
		TemplateID:     body.TemplateID,
		Profile:        promptProfile,
		Template:       body.Template.toDomain(promptProfile.PreferredTone),
		Business:       body.Business.toDomain(),
		Platform:       traits.Clamp(body.Platform, 16),
		AppVersion:     traits.Clamp(body.AppVersion, 32),
		IdempotencyKey: key,
	})
	if err != nil {
		status, code, message := httpx.Translate(err)
		details := errorDetails(err)
		s.limitDetails(r.Context(), details, code, user, requestPlatform(r, body.Platform), result)
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
		Usage:            s.resultUsage(result),
	})
}

// composeRequest — «Create» режимі: пайдаланушы не жазу керегін сипаттайды.
// source_text жоқ: бұл ешкімге жауап емес.
type composeRequest struct {
	Instruction string `json:"instruction"`
	// Language — қолданбаның интерфейс тілі (метадерек және тілі анық емес
	// нұсқауға арналған шешім). Хабарлама тілі нұсқаудың өз тілінен анықталады.
	Language string `json:"language"`
	// InputLanguage және Profile — тек features.sender_profile=true болса.
	InputLanguage string          `json:"input_language"`
	Profile       *composeProfile `json:"profile"`
	// Regenerate — сол нұсқау бойынша басқа нұсқа сұралды.
	Regenerate bool   `json:"regenerate"`
	AppVersion string `json:"app_version"`
	Platform   string `json:"platform"`
}

// composeProfile — compose-қа тек жіберушінің жынысы керек.
type composeProfile struct {
	GrammaticalGender string `json:"grammatical_gender"`
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
	key, err := idempotencyKey(r)
	if err != nil {
		httpx.Fail(w, err)
		return
	}

	// Жыныс: сұраныстағы жарамды мән → сақталған профиль → unspecified.
	profile, err := s.users.Profile(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	gender := profile.GrammaticalGender
	if body.Profile != nil && domain.IsGrammaticalGender(body.Profile.GrammaticalGender) {
		gender = body.Profile.GrammaticalGender
	}

	result, err := s.ai.Compose(r.Context(), ai.ComposeRequest{
		User:              user,
		DeviceID:          DeviceFrom(r.Context()),
		Instruction:       body.Instruction,
		Language:          traits.Clamp(body.Language, 16),
		InputLanguage:     ai.InputLanguage(body.InputLanguage),
		GrammaticalGender: gender,
		Regenerate:        body.Regenerate,
		Platform:          traits.Clamp(body.Platform, 16),
		AppVersion:        traits.Clamp(body.AppVersion, 32),
		IdempotencyKey:    key,
	})
	if err != nil {
		status, code, message := httpx.Translate(err)
		details := errorDetails(err)
		s.limitDetails(r.Context(), details, code, user, requestPlatform(r, body.Platform), result)
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
		Usage:            s.resultUsage(result),
	})
}

// resultUsage — генерациядан кейінгі санағыштар (GET /me/usage-пен бірдей өрістер және bucket).
func (s *Server) resultUsage(result ai.Result) usageDTO {
	dto := usageDTO{
		DailyLimit:     result.DailyLimit,
		Unlimited:      result.Unlimited,
		UsedToday:      result.UsedToday,
		RemainingToday: result.Remaining,
		MonthlyLimit:   result.MonthlyLimit,
		UsedMonth:      result.UsedMonth,
		ResetsAt:       result.ResetsAt.Format(time.RFC3339),
		Timezone:       s.cfg.App.Timezone,
		Bucket:         result.Bucket,
	}
	dto.withBalance(result.Balance, s.cfg.App.Location())
	return dto
}

// errorDetails — қатенің өз details-і (мысалы, Idempotency-Key қайшылығының field/reason).
func errorDetails(err error) map[string]any {
	details := map[string]any{}
	var d interface{ Details() map[string]any }
	if errors.As(err, &d) {
		for k, v := range d.Details() {
			details[k] = v
		}
	}
	return details
}

// limitDetails — DAILY_LIMIT_REACHED / MONTHLY_LIMIT_REACHED қатесіндегі санағыштар.
// monthly_limit пен used_month тек айлық шек бар тарифте (0 емес) қосылады.
//
// welcome_remaining, credits_remaining and pass_active describe the extra
// buckets (empty by definition when the limit is hit, except a pass that is
// still running); can_buy says the app may offer to buy more replies.
func (s *Server) limitDetails(ctx context.Context, details map[string]any, code string, user domain.User,
	platform string, result ai.Result) {
	if code != httpx.CodeDailyLimit && code != httpx.CodeMonthlyLimit {
		return
	}
	details["daily_limit"] = result.DailyLimit
	details["used_today"] = result.UsedToday
	details["resets_at"] = result.ResetsAt.Format(time.RFC3339)
	if result.MonthlyLimit > 0 {
		details["monthly_limit"] = result.MonthlyLimit
		details["used_month"] = result.UsedMonth
	}
	details["welcome_remaining"] = result.Balance.WelcomeRemaining
	details["credits_remaining"] = result.Balance.Credits()
	details["pass_active"] = result.Balance.PassActive
	details["can_buy"] = s.canBuy(ctx, user, platform)
}

// requestPlatform — сұраныстағы платформа, болмаса токендегісі.
func requestPlatform(r *http.Request, declared string) string {
	if p := strings.ToLower(strings.TrimSpace(declared)); p != "" {
		return traits.Clamp(p, 16)
	}
	return PlatformFrom(r.Context())
}

// canBuy — қолданба осы платформада жауап сатып алуды ұсына ала ма (PurchaseOptions → iap.Available).
func (s *Server) canBuy(ctx context.Context, user domain.User, platform string) bool {
	if s.purchases == nil {
		return false
	}
	return s.purchases.CanBuy(ctx, user, platform)
}

// polishRequest — нұсқау өрісіндегі мәтін (features.instruction_polish).
type polishRequest struct {
	Text          string `json:"text"`
	InputLanguage string `json:"input_language"`
	Platform      string `json:"platform"`
	AppVersion    string `json:"app_version"`
}

type polishResponse struct {
	Text    string `json:"text"`
	Changed bool   `json:"changed"`
}

// handlePolish — пайдаланушы жазған нұсқаудың түзетілген нұсқасы (ұсыныс).
// Квота жұмсалмайды; мәтін жадта ғана өңделеді.
func (s *Server) handlePolish(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AI.PolishEnabled {
		httpx.Fail(w, domain.ErrNotFound)
		return
	}
	user, _ := UserFrom(r.Context())
	var body polishRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}

	result, err := s.ai.Polish(r.Context(), ai.PolishRequest{
		User:          user,
		DeviceID:      DeviceFrom(r.Context()),
		Text:          body.Text,
		InputLanguage: ai.InputLanguage(body.InputLanguage),
		Platform:      traits.Clamp(body.Platform, 16),
		AppVersion:    traits.Clamp(body.AppVersion, 32),
	})
	if err != nil {
		status, code, message := httpx.Translate(err)
		var details map[string]any
		switch {
		case errors.Is(err, domain.ErrInstructionMissing):
			details = map[string]any{"field": "text"}
		case errors.Is(err, domain.ErrInstructionTooLong):
			details = map[string]any{"field": "text", "max_characters": result.TextLimit}
		}
		httpx.Error(w, status, code, message, details)
		return
	}
	httpx.JSON(w, http.StatusOK, polishResponse{Text: result.Text, Changed: result.Changed})
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
		"usage":        s.usage(entitlement),
	})
}
