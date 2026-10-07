package api

import (
	"time"

	"github.com/aireply/ai-reply-back-end/internal/ai"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// DTO-лар әдейі бөлек: дерекқор құрылымы ешқашан сыртқа шықпайды.

type userDTO struct {
	ID         string `json:"id"`
	Phone      string `json:"phone,omitempty"`
	Email      string `json:"email,omitempty"`
	Status     string `json:"status"`
	Locale     string `json:"locale"`
	Platform   string `json:"platform,omitempty"`
	CreatedAt  string `json:"created_at"`
	Onboarding bool   `json:"onboarding_completed"`
	// AuthProviders — кіру тәсілдері: apple | email | google | phone.
	AuthProviders []string `json:"auth_providers"`
	// PreferredLanguage — kk | ru | en | uz, "" — қолданушы әлі таңдамаған.
	PreferredLanguage string `json:"preferred_language"`
}

func toUserDTO(u domain.User, p domain.Profile) userDTO {
	return userDTO{
		ID:         u.ID,
		Phone:      u.Phone,
		Email:      u.Email,
		Status:     u.Status,
		Locale:     u.Locale,
		Platform:   u.Platform,
		CreatedAt:  u.CreatedAt.Format(time.RFC3339),
		Onboarding: p.OnboardingCompleted,

		AuthProviders:     []string{},
		PreferredLanguage: u.PreferredLanguage,
	}
}

type profileDTO struct {
	DisplayName       string   `json:"display_name"`
	Role              string   `json:"role"`
	Description       string   `json:"description"`
	PreferredTone     string   `json:"preferred_tone"`
	BusinessOffering  string   `json:"business_offering"`
	BusinessSummary   string   `json:"business_summary"`
	BusinessRules     []string `json:"business_rules"`
	Completed         bool     `json:"onboarding_completed"`
	GrammaticalGender string   `json:"grammatical_gender"`
	OnboardingVersion int      `json:"onboarding_version"`
	UpdatedAt         string   `json:"updated_at"`
}

type legalConsentDTO struct {
	TermsVersion   string `json:"terms_version"`
	PrivacyVersion string `json:"privacy_version"`
	AcceptedAt     string `json:"accepted_at"`
	Locale         string `json:"locale"`
	Platform       string `json:"platform"`
	AppVersion     string `json:"app_version,omitempty"`
}

func toLegalConsentDTO(consent domain.LegalConsent) legalConsentDTO {
	return legalConsentDTO{
		TermsVersion:   consent.TermsVersion,
		PrivacyVersion: consent.PrivacyVersion,
		AcceptedAt:     consent.AcceptedAt.Format(time.RFC3339),
		Locale:         consent.Locale,
		Platform:       consent.Platform,
		AppVersion:     consent.AppVersion,
	}
}

func toProfileDTO(p domain.Profile) profileDTO {
	rules := p.BusinessRules
	if rules == nil {
		rules = []string{}
	}
	return profileDTO{
		DisplayName:       p.DisplayName,
		Role:              p.Role,
		Description:       p.Description,
		PreferredTone:     p.PreferredTone,
		BusinessOffering:  p.BusinessOffering,
		BusinessSummary:   p.BusinessSummary,
		BusinessRules:     rules,
		Completed:         p.OnboardingCompleted,
		GrammaticalGender: p.GrammaticalGender,
		OnboardingVersion: p.OnboardingVersion,
		UpdatedAt:         p.UpdatedAt.Format(time.RFC3339),
	}
}

type planDTO struct {
	ID           string            `json:"id"`
	Code         string            `json:"code"`
	Name         map[string]string `json:"name"`
	Description  map[string]string `json:"description"`
	Price        int64             `json:"price"`
	PriceText    string            `json:"price_text"`
	Currency     string            `json:"currency"`
	DailyLimit   int               `json:"daily_message_limit"`
	MonthlyLimit int               `json:"monthly_message_limit"`
	PeriodDays   int               `json:"period_days"`
	IsFree       bool              `json:"is_free"`
	SortOrder    int               `json:"sort_order"`
}

func toPlanDTO(p domain.Plan) planDTO {
	return planDTO{
		ID:           p.ID,
		Code:         p.Code,
		Name:         p.Name,
		Description:  p.Description,
		Price:        p.Price,
		PriceText:    traits.FormatMoney(p.Price, p.Currency),
		Currency:     p.Currency,
		DailyLimit:   p.DailyLimit,
		MonthlyLimit: p.MonthlyLimit,
		PeriodDays:   p.PeriodDays,
		IsFree:       p.IsFree,
		SortOrder:    p.SortOrder,
	}
}

type subscriptionDTO struct {
	ID        string  `json:"id,omitempty"`
	Status    string  `json:"status"`
	Plan      planDTO `json:"plan"`
	StartedAt string  `json:"started_at,omitempty"`
	ExpiresAt string  `json:"expires_at,omitempty"`
	Source    string  `json:"source,omitempty"`
}

type usageDTO struct {
	DailyLimit     int    `json:"daily_limit"`
	UsedToday      int    `json:"used_today"`
	RemainingToday int    `json:"remaining_today"`
	MonthlyLimit   int    `json:"monthly_limit"`
	UsedMonth      int    `json:"used_month"`
	ResetsAt       string `json:"resets_at"`
	Timezone       string `json:"timezone"`
}

func toUsageDTO(e domain.Entitlement, tz string) usageDTO {
	return usageDTO{
		DailyLimit:     e.DailyLimit,
		UsedToday:      e.UsedToday,
		RemainingToday: e.Remaining(),
		MonthlyLimit:   e.MonthlyLimit,
		UsedMonth:      e.UsedMonth,
		ResetsAt:       e.ResetsAt.Format(time.RFC3339),
		Timezone:       tz,
	}
}

type deviceDTO struct {
	ID         string `json:"id"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
	OSVersion  string `json:"os_version"`
	Model      string `json:"model"`
	PushOn     bool   `json:"push_enabled"`
	LastSeenAt string `json:"last_seen_at"`
}

func toDeviceDTO(d domain.Device) deviceDTO {
	return deviceDTO{
		ID:         d.ID,
		Platform:   d.Platform,
		AppVersion: d.AppVersion,
		OSVersion:  d.OSVersion,
		Model:      d.Model,
		PushOn:     d.PushOn,
		LastSeenAt: d.LastSeenAt.Format(time.RFC3339),
	}
}

// businessDTO — клиенттен келетін бизнес блогы.
type businessDTO struct {
	Offering string   `json:"offering"`
	Summary  string   `json:"summary"`
	Rules    []string `json:"rules"`
}

func (b *businessDTO) toDomain() ai.Business {
	if b == nil {
		return ai.Business{}
	}
	return ai.Business{
		Offering: traits.Clamp(b.Offering, 120),
		Summary:  traits.Clamp(b.Summary, 400),
		Rules:    b.Rules,
	}
}

type templateDTO struct {
	Name                 string       `json:"name"`
	Relationship         string       `json:"relationship"`
	Tone                 string       `json:"tone"`
	Instructions         string       `json:"instructions"`
	ReplyLength          string       `json:"reply_length"`
	EmojiPolicy          string       `json:"emoji_policy"`
	WorkingHoursBehavior string       `json:"working_hours_behaviour"`
	Business             *businessDTO `json:"business"`
}

func (t *templateDTO) toDomain(fallbackTone string) ai.Template {
	if t == nil {
		return ai.Template{Tone: fallbackTone, ReplyLength: "short", EmojiPolicy: "minimal",
			WorkingHoursBehavior: "mention_when_relevant"}
	}
	tone := t.Tone
	if !traits.OneOf(tone, "natural", "friendly", "professional", "formal", "short") {
		tone = fallbackTone
	}
	length := t.ReplyLength
	if !traits.OneOf(length, "short", "medium") {
		length = "short"
	}
	emoji := t.EmojiPolicy
	if !traits.OneOf(emoji, "allowed", "minimal", "none") {
		emoji = "minimal"
	}
	behaviour := t.WorkingHoursBehavior
	if !traits.OneOf(behaviour, "ignore", "mention_when_relevant", "always_mention") {
		behaviour = "mention_when_relevant"
	}
	return ai.Template{
		Name:                 traits.Clamp(t.Name, 60),
		Relationship:         traits.Clamp(t.Relationship, 40),
		Tone:                 tone,
		Instructions:         traits.Clamp(t.Instructions, 600),
		ReplyLength:          length,
		EmojiPolicy:          emoji,
		WorkingHoursBehavior: behaviour,
		Business:             t.Business.toDomain(),
	}
}

type workingHoursDTO struct {
	Enabled           bool   `json:"enabled"`
	IsWithinHours     bool   `json:"is_within_working_hours"`
	CurrentLocalTime  string `json:"current_local_time"`
	NextWorkingPeriod string `json:"next_working_period"`
	WeeklySchedule    string `json:"weekly_schedule"`
}

func (b *workingHoursDTO) toDomain() ai.WorkingHours {
	if b == nil {
		return ai.WorkingHours{}
	}
	return ai.WorkingHours{
		Enabled:           b.Enabled,
		IsWithinHours:     b.IsWithinHours,
		CurrentLocalTime:  traits.Clamp(b.CurrentLocalTime, 32),
		NextWorkingPeriod: traits.Clamp(b.NextWorkingPeriod, 80),
		WeeklySchedule:    traits.Clamp(b.WeeklySchedule, 200),
	}
}
