// Package users — профиль және құрылғылар.
package users

import (
	"context"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/legal"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Service — қолданушы деректері.
type Service struct{ repo *repository.Store }

// New — қызмет.
func New(repo *repository.Store) *Service { return &Service{repo: repo} }

// SignInMethods — қолданушының кіру тәсілдері.
func (s *Service) SignInMethods(ctx context.Context, userID string) ([]string, error) {
	return s.repo.SignInMethods(ctx, userID)
}

// Profile — профильді оқу.
func (s *Service) Profile(ctx context.Context, userID string) (domain.Profile, error) {
	return s.repo.Profile(ctx, userID)
}

// ProfileUpdate — тіркеуді аяқтау/өңдеу сұранысы.
//
// The API converts its request type into this one directly, so the two keep
// identical fields in identical order (transport/api updateMeRequest).
type ProfileUpdate struct {
	DisplayName      *string
	Role             *string
	Description      *string
	PreferredTone    *string
	BusinessOffering *string
	BusinessSummary  *string
	BusinessRules    *[]string
	Locale           *string
	Timezone         *string
	Completed        *bool
	// GrammaticalGender — male | female | unspecified; басқа мән — қате.
	GrammaticalGender *string
	// OnboardingVersion — 0…1000; сақталатыны ескі мән мен жаңасының үлкені.
	OnboardingVersion *int
}

// UpdateProfile — тек берілген өрістер өзгереді.
func (s *Service) UpdateProfile(ctx context.Context, userID string, in ProfileUpdate) (domain.Profile, error) {
	profile, err := s.repo.Profile(ctx, userID)
	if err != nil {
		return domain.Profile{}, err
	}
	profile.UserID = userID

	if in.DisplayName != nil {
		profile.DisplayName = traits.Clamp(*in.DisplayName, 80)
	}
	if in.Role != nil {
		profile.Role = traits.Clamp(*in.Role, 120)
	}
	if in.Description != nil {
		profile.Description = traits.Clamp(*in.Description, 1000)
	}
	if in.PreferredTone != nil {
		tone := strings.ToLower(strings.TrimSpace(*in.PreferredTone))
		if !traits.OneOf(tone, "natural", "friendly", "professional", "formal", "short") {
			return domain.Profile{}, domain.ErrInvalidRequest
		}
		profile.PreferredTone = tone
	}
	if in.BusinessOffering != nil {
		profile.BusinessOffering = traits.Clamp(*in.BusinessOffering, 120)
	}
	if in.BusinessSummary != nil {
		profile.BusinessSummary = traits.Clamp(*in.BusinessSummary, 400)
	}
	if in.BusinessRules != nil {
		rules := make([]string, 0, 8)
		for _, r := range *in.BusinessRules {
			if r = traits.Clamp(r, 200); r != "" {
				rules = append(rules, r)
			}
			if len(rules) == 8 {
				break
			}
		}
		profile.BusinessRules = rules
	}
	if in.Completed != nil {
		profile.OnboardingCompleted = *in.Completed
	}
	if in.GrammaticalGender != nil {
		gender := strings.ToLower(strings.TrimSpace(*in.GrammaticalGender))
		if !domain.IsGrammaticalGender(gender) {
			return domain.Profile{}, domain.ErrInvalidRequest
		}
		profile.GrammaticalGender = gender
	}
	if in.OnboardingVersion != nil {
		version := *in.OnboardingVersion
		if version < 0 || version > domain.MaxOnboardingVersion {
			return domain.Profile{}, domain.ErrInvalidRequest
		}
		// Ескі құрылғы нұсқаны кері түсіре алмайды.
		if version > profile.OnboardingVersion {
			profile.OnboardingVersion = version
		}
	}
	if err := s.repo.SaveProfile(ctx, profile); err != nil {
		return domain.Profile{}, err
	}

	if in.Locale != nil || in.Timezone != nil {
		locale, timezone := "", ""
		if in.Locale != nil {
			locale = domain.NormalizeLocale(*in.Locale)
		}
		if in.Timezone != nil {
			timezone = traits.Clamp(*in.Timezone, 64)
		}
		if err := s.repo.UpdateUserMeta(ctx, userID, "", "", "", locale, timezone); err != nil {
			return domain.Profile{}, err
		}
	}
	return profile, nil
}

// RegisterDevice — құрылғыны тіркеу (push негізі).
func (s *Service) RegisterDevice(ctx context.Context, d domain.Device) (domain.Device, error) {
	d.Platform = strings.ToLower(strings.TrimSpace(d.Platform))
	if d.Platform != "" && !traits.OneOf(d.Platform, domain.PlatformIOS, domain.PlatformAndroid, domain.PlatformWeb) {
		return domain.Device{}, domain.ErrInvalidRequest
	}
	d.AppVersion = traits.Clamp(d.AppVersion, 32)
	d.OSVersion = traits.Clamp(d.OSVersion, 32)
	d.Model = traits.Clamp(d.Model, 64)
	d.PushToken = traits.Clamp(d.PushToken, 512)
	return s.repo.UpsertDevice(ctx, d)
}

// Devices — қолданушының құрылғылары.
func (s *Service) Devices(ctx context.Context, userID string) ([]domain.Device, error) {
	return s.repo.DevicesByUser(ctx, userID)
}

// RemoveDevice — құрылғыны өшіру.
func (s *Service) RemoveDevice(ctx context.Context, userID, deviceID string) error {
	return s.repo.RevokeDevice(ctx, userID, deviceID)
}

// LegalConsentInput identifies the public documents accepted in a client.
type LegalConsentInput struct {
	TermsVersion   string
	PrivacyVersion string
	Locale         string
	Platform       string
	AppVersion     string
}

// SaveLegalConsent validates and stores acceptance for the current documents.
func (s *Service) SaveLegalConsent(ctx context.Context, userID string, in LegalConsentInput) (domain.LegalConsent, error) {
	if strings.TrimSpace(in.TermsVersion) != legal.TermsVersion ||
		strings.TrimSpace(in.PrivacyVersion) != legal.PrivacyVersion {
		return domain.LegalConsent{}, domain.ErrInvalidRequest
	}
	platform := strings.ToLower(strings.TrimSpace(in.Platform))
	if !traits.OneOf(platform, domain.PlatformIOS, domain.PlatformAndroid, domain.PlatformWeb) {
		return domain.LegalConsent{}, domain.ErrInvalidRequest
	}
	now := time.Now().UTC()
	return s.repo.SaveLegalConsent(ctx, domain.LegalConsent{
		UserID:         userID,
		TermsVersion:   legal.TermsVersion,
		PrivacyVersion: legal.PrivacyVersion,
		AcceptedAt:     now,
		Locale:         domain.NormalizeLocale(strings.ToLower(strings.TrimSpace(in.Locale))),
		Platform:       platform,
		AppVersion:     traits.Clamp(in.AppVersion, 32),
		CreatedAt:      now,
	})
}

// CurrentLegalConsent returns acceptance for the public versions served now.
func (s *Service) CurrentLegalConsent(ctx context.Context, userID string) (domain.LegalConsent, error) {
	return s.repo.LegalConsent(ctx, userID, legal.TermsVersion, legal.PrivacyVersion)
}
