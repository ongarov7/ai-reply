package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/legal"
	"github.com/aireply/ai-reply-back-end/internal/phone"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
	"github.com/aireply/ai-reply-back-end/internal/users"
)

type meResponse struct {
	User         userDTO          `json:"user"`
	Profile      profileDTO       `json:"profile"`
	Subscription subscriptionDTO  `json:"subscription"`
	Usage        usageDTO         `json:"usage"`
	LegalConsent *legalConsentDTO `json:"legal_consent,omitempty"`
}

// handleMe — профиль, тариф және квота бір сұраныста.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	s.writeMe(w, r, user)
}

// writeMe — /me жауабын жинайды (пошта қосылғаннан кейін де осы пішін қайтады).
func (s *Server) writeMe(w http.ResponseWriter, r *http.Request, user domain.User) {
	profile, err := s.users.Profile(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	entitlement, err := s.subs.Entitlement(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	consent, err := s.currentLegalConsent(r, user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, meResponse{
		User:         s.userDTO(r.Context(), user, profile),
		Profile:      toProfileDTO(profile),
		Subscription: s.subscriptionDTO(entitlement),
		Usage:        toUsageDTO(entitlement, s.cfg.App.Timezone),
		LegalConsent: consent,
	})
}

// userDTO — қолданушы және оның кіру тәсілдері (email, google, apple, phone).
func (s *Server) userDTO(ctx context.Context, user domain.User, profile domain.Profile) userDTO {
	dto := toUserDTO(user, profile)
	if methods, err := s.users.SignInMethods(ctx, user.ID); err == nil {
		dto.AuthProviders = methods
	}
	return dto
}

func (s *Server) currentLegalConsent(r *http.Request, userID string) (*legalConsentDTO, error) {
	consent, err := s.users.CurrentLegalConsent(r.Context(), userID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	dto := toLegalConsentDTO(consent)
	return &dto, nil
}

type legalConsentRequest struct {
	TermsVersion   string `json:"terms_version"`
	PrivacyVersion string `json:"privacy_version"`
	Locale         string `json:"locale"`
	Platform       string `json:"platform"`
	AppVersion     string `json:"app_version"`
}

func (s *Server) handleSaveLegalConsent(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body legalConsentRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	consent, err := s.users.SaveLegalConsent(r.Context(), user.ID, users.LegalConsentInput{
		TermsVersion:   body.TermsVersion,
		PrivacyVersion: body.PrivacyVersion,
		Locale:         body.Locale,
		Platform:       body.Platform,
		AppVersion:     body.AppVersion,
	})
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toLegalConsentDTO(consent))
}

type updateMeRequest struct {
	DisplayName      *string   `json:"display_name"`
	Role             *string   `json:"role"`
	Description      *string   `json:"description"`
	PreferredTone    *string   `json:"preferred_tone"`
	BusinessOffering *string   `json:"business_offering"`
	BusinessSummary  *string   `json:"business_summary"`
	BusinessRules    *[]string `json:"business_rules"`
	Locale           *string   `json:"locale"`
	Timezone         *string   `json:"timezone"`
	Completed        *bool     `json:"onboarding_completed"`
}

// handleUpdateMe — тіркеуді аяқтау немесе профильді өңдеу.
func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body updateMeRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	profile, err := s.users.UpdateProfile(r.Context(), user.ID, users.ProfileUpdate(body))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toProfileDTO(profile))
}

// handleUsage — квота күйі (клиенттің есебіне сенбейміз).
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	entitlement, err := s.subs.Entitlement(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toUsageDTO(entitlement, s.cfg.App.Timezone))
}

// handleSubscription — ағымдағы тариф.
func (s *Server) handleSubscription(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	entitlement, err := s.subs.Entitlement(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, s.subscriptionDTO(entitlement))
}

// handlePlans — қолжетімді тарифтер.
func (s *Server) handlePlans(w http.ResponseWriter, r *http.Request) {
	list, err := s.plans.Active(r.Context())
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	out := make([]planDTO, 0, len(list))
	for _, p := range list {
		out = append(out, toPlanDTO(p))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"plans": out})
}

type countryDTO struct {
	ISO     string `json:"iso"`
	Dial    string `json:"dial_code"`
	Name    string `json:"name"`
	Example string `json:"example"`
}

// handleConfig — клиентке қажет сервер параметрлері (құпиясыз).
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	countries := make([]countryDTO, 0, len(phone.Countries()))
	for _, c := range phone.Countries() {
		countries = append(countries, countryDTO{ISO: c.ISO, Dial: "+" + c.Dial, Name: c.NameEN, Example: c.Example})
	}
	current := s.limits.Current(r.Context())
	httpx.JSON(w, http.StatusOK, map[string]any{
		"locales":        domain.Locales,
		"default_locale": "en",
		"timezone":       s.cfg.App.Timezone,
		// Character limits the apps validate against before sending anything.
		// The server enforces the same values; the admin panel changes them.
		"max_source_characters":  current.SourceChars,
		"max_instruction_length": current.InstructionChars,
		// What this server understands, so a newer app never sends a field an
		// older server would reject (request bodies disallow unknown fields).
		"features": map[string]bool{
			"reply_preferences": true,
			// POST /api/v1/ai/compose — writing a new message from an instruction.
			"compose": true,
			// Sign-in methods this server accepts right now.
			"email_otp":      s.auth.EmailDelivery() != "off",
			"google_sign_in": s.auth.GoogleEnabled(),
			"apple_sign_in":  s.auth.AppleEnabled(),
		},
		"demo_mode":    s.cfg.Auth.DemoMode,
		"payment_mode": s.payments.Mode(),
		"countries":    countries,
		"legal": map[string]string{
			"terms_version":   legal.TermsVersion,
			"privacy_version": legal.PrivacyVersion,
			"terms_url":       s.cfg.App.PublicBaseURL + "/offer",
			"privacy_url":     s.cfg.App.PublicBaseURL + "/privacy",
		},
	})
}

type registerDeviceRequest struct {
	DeviceID   string `json:"device_id"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
	OSVersion  string `json:"os_version"`
	Model      string `json:"model"`
	Locale     string `json:"locale"`
	PushToken  string `json:"push_token"`
	PushOn     bool   `json:"push_enabled"`
}

// handleRegisterDevice — құрылғыны тіркеу (push дайындығы).
func (s *Server) handleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body registerDeviceRequest
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	device, err := s.users.RegisterDevice(r.Context(), domain.Device{
		ID:         body.DeviceID,
		UserID:     user.ID,
		Platform:   body.Platform,
		AppVersion: body.AppVersion,
		OSVersion:  body.OSVersion,
		Model:      body.Model,
		Locale:     body.Locale,
		PushToken:  body.PushToken,
		PushOn:     body.PushOn,
	})
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toDeviceDTO(device))
}

// handleListDevices — тіркелген құрылғылар.
func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	list, err := s.users.Devices(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	out := make([]deviceDTO, 0, len(list))
	for _, d := range list {
		out = append(out, toDeviceDTO(d))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"devices": out})
}

// handleDeleteDevice — құрылғыны өшіру.
func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	if err := s.users.RemoveDevice(r.Context(), user.ID, r.PathValue("id")); err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
