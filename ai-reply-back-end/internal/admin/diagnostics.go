package admin

import (
	"context"
	"regexp"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/auth"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/phone"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// SubjectHasher — кіру оқиғаларын пошта/телефон бойынша іздеу үшін (telemetry.Service).
type SubjectHasher interface {
	SubjectHash(kind, value string) string
}

// WithSubjectHasher — кіру оқиғаларын пошта бойынша табу үшін.
func (s *Service) WithSubjectHasher(h SubjectHasher) *Service { s.hasher = h; return s }

// UserQuery — әкімшінің еркін іздеуі қолданушы идентификаторларына айналған түрі.
//
// IDs nil means "no user filter"; an empty, non-nil slice means the query
// matched nobody (the list must then be empty, not unfiltered). SubjectHash
// also finds sign-in attempts made with an address that has no account.
type UserQuery struct {
	IDs         []string
	SubjectHash string
}

var userIDPrefix = regexp.MustCompile(`^[0-9a-fA-F-]{4,36}$`)

// ResolveUser — пошта, телефон не идентификатор → тіркелгілер.
func (s *Service) ResolveUser(ctx context.Context, query string) (UserQuery, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return UserQuery{}, nil
	}
	var (
		ids []string
		err error
		out UserQuery
	)
	switch {
	case strings.Contains(query, "@"):
		email, normErr := auth.NormalizeEmail(query)
		if normErr != nil {
			email = strings.ToLower(query)
		}
		ids, err = s.repo.UserIDsMatching(ctx, email, "", "")
		if s.hasher != nil {
			out.SubjectHash = s.hasher.SubjectHash("email", email)
		}
	case strings.HasPrefix(query, "+") || digitsOnly(query) >= 7:
		number, parseErr := phone.Parse(query)
		if parseErr != nil {
			return UserQuery{IDs: []string{}}, nil
		}
		ids, err = s.repo.UserIDsMatching(ctx, "", number.E164, "")
		if s.hasher != nil {
			out.SubjectHash = s.hasher.SubjectHash("phone", number.E164)
		}
	case userIDPrefix.MatchString(query):
		ids, err = s.repo.UserIDsMatching(ctx, "", "", strings.ToLower(query))
	default:
		return UserQuery{IDs: []string{}}, nil
	}
	if err != nil {
		return UserQuery{}, err
	}
	if ids == nil {
		ids = []string{}
	}
	out.IDs = ids
	return out, nil
}

func digitsOnly(s string) int {
	n := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			n++
		case r == ' ' || r == '-' || r == '(' || r == ')':
		default:
			return 0
		}
	}
	return n
}

// Diagnostics — бір қолданушы бойынша қолдау/тергеу көрінісі (құпиясыз).
type Diagnostics struct {
	User          domain.User
	SignInMethods []string
	Entitlement   domain.Entitlement
	Installations []domain.Installation
	Sessions      []domain.AppSession
	AuthEvents    []domain.AuthEvent
	APIErrors     []domain.APIError
	AppErrors     []domain.AppEvent
	Deliveries    []repository.DeliveryRow
}

// UserDiagnostics — құрылғылар, сессиялар, кіру оқиғалары, қателер, хабарламалар.
func (s *Service) UserDiagnostics(ctx context.Context, userID string) (Diagnostics, error) {
	var d Diagnostics
	var err error
	if d.User, err = s.repo.UserByID(ctx, userID); err != nil {
		return d, err
	}
	if d.SignInMethods, err = s.repo.SignInMethods(ctx, userID); err != nil {
		return d, err
	}
	if d.Entitlement, err = s.subs.Entitlement(ctx, userID); err != nil {
		return d, err
	}
	if d.Installations, err = s.repo.InstallationsByUser(ctx, userID); err != nil {
		return d, err
	}
	if d.Sessions, err = s.repo.UserAppSessions(ctx, userID, 20); err != nil {
		return d, err
	}
	page := traits.NewPage(30, 0)
	ids := []string{userID}
	if d.AuthEvents, _, err = s.repo.ListAuthEvents(ctx, repository.AuthEventFilter{UserIDs: ids, Page: page}); err != nil {
		return d, err
	}
	if d.APIErrors, _, err = s.repo.ListAPIErrors(ctx, repository.APIErrorFilter{UserIDs: ids, Page: page}); err != nil {
		return d, err
	}
	if d.AppErrors, _, err = s.repo.ListAppEvents(ctx, repository.EventFilter{
		UserIDs: ids, Outcome: domain.OutcomeFailure, Page: page}); err != nil {
		return d, err
	}
	d.Deliveries, _, err = s.repo.ListDeliveries(ctx, repository.DeliveryFilter{UserID: userID, Page: page})
	return d, err
}

// AppEvents — қосымша оқиғалары.
func (s *Service) AppEvents(ctx context.Context, f repository.EventFilter) ([]domain.AppEvent, int, error) {
	return s.repo.ListAppEvents(ctx, f)
}

// AuthEvents — кіру оқиғалары.
func (s *Service) AuthEvents(ctx context.Context, f repository.AuthEventFilter) ([]domain.AuthEvent, int, error) {
	return s.repo.ListAuthEvents(ctx, f)
}

// APIErrors — API қателері.
func (s *Service) APIErrors(ctx context.Context, f repository.APIErrorFilter) ([]domain.APIError, int, error) {
	return s.repo.ListAPIErrors(ctx, f)
}

// Versions — қосымша нұсқаларының нақты қолданысы және қателері.
func (s *Service) Versions(ctx context.Context) ([]repository.VersionRow, error) {
	return s.repo.VersionReport(ctx, s.clock.Now())
}

// Devices — орнатулар тізімі.
func (s *Service) Devices(ctx context.Context, f repository.InstallationFilter) ([]repository.InstallationRow, int, error) {
	return s.repo.ListInstallations(ctx, f)
}

// OpsView — операциялық тақта.
type OpsView struct {
	Summary      repository.OpsSummary
	AppVersions  []repository.Point
	OSVersions   []repository.Point
	RecentErrors []domain.APIError
}

// Ops — құрылғылар, push, кіру және қателер бойынша жиынтық.
func (s *Service) Ops(ctx context.Context) (OpsView, error) {
	now := s.clock.Now()
	var v OpsView
	var err error
	if v.Summary, err = s.repo.Ops(ctx, now); err != nil {
		return v, err
	}
	if v.AppVersions, err = s.repo.AppVersionMix(ctx, now); err != nil {
		return v, err
	}
	if v.OSVersions, err = s.repo.OSVersionReport(ctx, now); err != nil {
		return v, err
	}
	v.RecentErrors, _, err = s.repo.ListAPIErrors(ctx, repository.APIErrorFilter{Page: traits.NewPage(10, 0)})
	return v, err
}
