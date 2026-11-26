package api

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	_ "image/jpeg" // screenshot uploads: JPEG
	_ "image/png"  // and PNG
	"net/http"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/aicontext"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/legal"
	"github.com/aireply/ai-reply-back-end/internal/screenshot"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// Screenshot replies (features.screenshot_reply, docs/SCREENSHOT_REPLIES.md).
//
// POST /api/v1/ai/screenshot/estimate — the price before anything is sent to
// the AI provider. POST /api/v1/ai/screenshot/reply — the reply, charged by the
// tokens it really used, never above max_units. Neither stores the image, the
// recognised conversation, the instruction or the reply.

// Body limits: a conversation is at most a few dozen kilobytes; a reply in
// image mode carries one screenshot (base64 of at most maxScreenshotBytes).
const (
	screenshotEstimateBodyBytes = 128 << 10
	screenshotReplyBodyBytes    = 6 << 20
	maxScreenshotBytes          = 3 << 20
	maxScreenshotSide           = 4096
	minScreenshotSide           = 64
)

type screenshotRequest struct {
	Mode         string                   `json:"mode"`
	Conversation *screenshot.Conversation `json:"conversation"`
	Image        *screenshotImageDTO      `json:"image"`
	// ImageConsent — legal.ScreenshotConsentVersion, which the person accepted
	// in the app before the screenshot itself may be sent (image mode).
	ImageConsent      string           `json:"image_consent"`
	Instruction       string           `json:"instruction"`
	InstructionSource string           `json:"instruction_source"`
	Language          string           `json:"language"`
	InputLanguage     string           `json:"input_language"`
	TemplateID        string           `json:"template_id"`
	Template          *templateDTO     `json:"template"`
	Profile           *profileOverride `json:"profile"`
	// MaxUnits — the most the person approved (absent: one reply).
	MaxUnits   int    `json:"max_units"`
	AppVersion string `json:"app_version"`
	Platform   string `json:"platform"`
}

// screenshotImageDTO — the screenshot: its size for an estimate, its JPEG or
// PNG bytes (base64) for a reply in image mode.
type screenshotImageDTO struct {
	Data   string `json:"data"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type screenshotQuoteDTO struct {
	Mode            string `json:"mode"`
	EstimatedUnits  int    `json:"estimated_units"`
	MaxUnits        int    `json:"max_units"`
	Unlimited       bool   `json:"unlimited"`
	AvailableUnits  *int   `json:"available_units,omitempty"`
	ImagesLeftToday *int   `json:"images_left_today,omitempty"`
	InputTokens     int    `json:"input_tokens"`
	ImageTokens     int    `json:"image_tokens"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	TokensCounted   bool   `json:"tokens_counted"`
	// ReferenceTokens — one standard reply in weighted tokens; OutputWeight —
	// what one output token counts as (input tokens).
	ReferenceTokens int     `json:"reference_tokens"`
	OutputWeight    float64 `json:"output_weight"`
	PolicyVersion   string  `json:"policy_version"`
}

func toQuoteDTO(q ai.ScreenshotQuote) screenshotQuoteDTO {
	dto := screenshotQuoteDTO{
		Mode: q.Mode, EstimatedUnits: q.EstimatedUnits, MaxUnits: q.MaxUnits, Unlimited: q.Unlimited,
		InputTokens: q.InputTokens, ImageTokens: q.ImageTokens, MaxOutputTokens: q.MaxOutputTokens,
		TokensCounted: q.Counted, ReferenceTokens: int(q.Reference + 0.5),
		OutputWeight: float64(int(q.Weight*100+0.5)) / 100, PolicyVersion: q.PolicyVersion,
	}
	if q.AvailableUnits >= 0 {
		available := q.AvailableUnits
		dto.AvailableUnits = &available
	}
	if q.ImagesLeftToday >= 0 {
		left := q.ImagesLeftToday
		dto.ImagesLeftToday = &left
	}
	return dto
}

type screenshotReplyResponse struct {
	Reply            string             `json:"reply"`
	DetectedLanguage string             `json:"detected_language,omitempty"`
	Usage            usageDTO           `json:"usage"`
	Units            int                `json:"units"`
	RefundedUnits    int                `json:"refunded_units"`
	Quote            screenshotQuoteDTO `json:"quote"`
	ContextApplied   bool               `json:"context_applied"`
}

// screenshotInput — the request in the AI layer's terms; withImage decodes
// and checks the uploaded screenshot (a reply in image mode).
func (s *Server) screenshotInput(r *http.Request, body screenshotRequest, withImage bool) (ai.ScreenshotRequest, error) {
	user, _ := UserFrom(r.Context())
	profile, err := s.replyProfile(r.Context(), user, body.Profile, false)
	if err != nil {
		return ai.ScreenshotRequest{}, err
	}
	req := ai.ScreenshotRequest{
		User:            user,
		DeviceID:        DeviceFrom(r.Context()),
		SessionPlatform: PlatformFrom(r.Context()),
		Mode:            strings.ToLower(strings.TrimSpace(body.Mode)),
		Instruction:     body.Instruction,
		Voice:           body.InstructionSource == instructionSourceVoice,
		Language:        traits.Clamp(body.Language, 16),
		InputLanguage:   ai.InputLanguage(body.InputLanguage),
		TemplateID:      body.TemplateID,
		Profile:         profile,
		Template:        body.Template.toDomain(profile.PreferredTone),
		Platform:        traits.Clamp(body.Platform, 16),
		AppVersion:      traits.Clamp(body.AppVersion, 32),
		ApprovedUnits:   body.MaxUnits,
	}
	if body.Conversation != nil {
		req.Conversation = *body.Conversation
	}
	if req.Mode == screenshot.ModeImage {
		if body.Image != nil {
			req.ImageWidth, req.ImageHeight = body.Image.Width, body.Image.Height
		}
		if withImage {
			if body.ImageConsent != legal.ScreenshotConsentVersion {
				return ai.ScreenshotRequest{}, domain.ErrScreenshotConsent
			}
			if body.Image == nil || body.Image.Data == "" {
				return ai.ScreenshotRequest{}, domain.InvalidField("image", "required")
			}
			img, err := decodeScreenshot(body.Image.Data)
			if err != nil {
				return ai.ScreenshotRequest{}, err
			}
			req.Image = &img.prompt
			// The real size, not the declared one, prices the image.
			req.ImageWidth, req.ImageHeight = img.width, img.height
		}
	}
	recipient := aicontext.RecipientFor(body.TemplateID, templateRelationship(body.Template))
	req.Account, _ = s.accountContext(r.Context(), user, recipient)
	return req, nil
}

type decodedScreenshot struct {
	prompt        ai.PromptImage
	width, height int
}

// decodeScreenshot — base64 JPEG or PNG within the limits. The bytes live in
// memory for this request only.
func decodeScreenshot(data string) (decodedScreenshot, error) {
	if base64.StdEncoding.DecodedLen(len(data)) > maxScreenshotBytes+3 {
		return decodedScreenshot{}, screenshotImageError("too_large")
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return decodedScreenshot{}, screenshotImageError("format")
	}
	if len(raw) > maxScreenshotBytes {
		return decodedScreenshot{}, screenshotImageError("too_large")
	}
	mime := http.DetectContentType(raw)
	if mime != "image/jpeg" && mime != "image/png" {
		return decodedScreenshot{}, screenshotImageError("format")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return decodedScreenshot{}, screenshotImageError("unreadable")
	}
	if config.Width < minScreenshotSide || config.Height < minScreenshotSide ||
		config.Width > maxScreenshotSide || config.Height > maxScreenshotSide {
		return decodedScreenshot{}, screenshotImageError("size")
	}
	return decodedScreenshot{prompt: ai.PromptImage{MIME: mime, Data: raw, Detail: "high"},
		width: config.Width, height: config.Height}, nil
}

// screenshotImageError — 400 INVALID_REQUEST with the image limits.
func screenshotImageError(reason string) error {
	return &screenshotImageInvalid{reason: reason}
}

type screenshotImageInvalid struct{ reason string }

func (e *screenshotImageInvalid) Error() string { return "invalid screenshot: " + e.reason }
func (e *screenshotImageInvalid) Unwrap() error { return domain.ErrInvalidRequest }
func (e *screenshotImageInvalid) Details() map[string]any {
	return map[string]any{"field": "image", "reason": e.reason, "max_bytes": maxScreenshotBytes,
		"max_side": maxScreenshotSide, "formats": []string{"jpeg", "png"}}
}

// handleScreenshotEstimate — the price of a screenshot reply. Nothing leaves
// the server: the prompt is counted here and an image is priced by its size.
func (s *Server) handleScreenshotEstimate(w http.ResponseWriter, r *http.Request) {
	var body screenshotRequest
	if err := httpx.Decode(w, r, screenshotEstimateBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	req, err := s.screenshotInput(r, body, false)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	quote, err := s.ai.ScreenshotEstimate(r.Context(), req)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toQuoteDTO(quote))
}

// handleScreenshotReply — the reply to the conversation in a screenshot.
func (s *Server) handleScreenshotReply(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body screenshotRequest
	if err := httpx.Decode(w, r, screenshotReplyBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	key, err := idempotencyKey(r)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	req, err := s.screenshotInput(r, body, true)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	req.IdempotencyKey = key

	result, err := s.ai.ScreenshotReply(r.Context(), req)
	if err != nil {
		status, code, message := httpx.Translate(err)
		details := errorDetails(err)
		s.limitDetails(r.Context(), details, code, user, requestPlatform(r, body.Platform), result.Result)
		if errors.Is(err, domain.ErrInsufficientCredits) {
			details["can_buy"] = s.canBuy(r.Context(), user, requestPlatform(r, body.Platform))
		}
		if len(details) == 0 {
			details = nil
		}
		httpx.Error(w, status, code, message, details)
		return
	}
	httpx.JSON(w, http.StatusOK, screenshotReplyResponse{
		Reply:            result.Text,
		DetectedLanguage: result.DetectedLanguage,
		Usage:            s.resultUsage(result.Result),
		Units:            result.Units,
		RefundedUnits:    result.Refunded,
		Quote:            toQuoteDTO(result.Quote),
		ContextApplied:   result.ContextApplied,
	})
}

// screenshotConfig — what the apps need before offering screenshot replies.
func (s *Server) screenshotConfig(r *http.Request) map[string]any {
	if s.screenshots == nil {
		return nil
	}
	policy := s.screenshots.Policy(r.Context())
	return map[string]any{
		"image_enabled":            policy.ImageEnabled,
		"image_consent_version":    legal.ScreenshotConsentVersion,
		"policy_version":           screenshot.PolicyVersion,
		"max_messages":             screenshot.MaxMessages,
		"max_message_characters":   screenshot.MaxMessageChars,
		"max_total_characters":     screenshot.MaxTotalChars,
		"max_image_bytes":          maxScreenshotBytes,
		"max_image_side":           maxScreenshotSide,
		"reference_input_tokens":   policy.RefInputTokens,
		"reference_output_tokens":  policy.RefOutputTokens,
		"max_units":                policy.MaxUnits,
		"unlimited_images_per_day": policy.UnlimitedImagesPerDay,
		"shortcut_url":             policy.ShortcutURL,
	}
}
