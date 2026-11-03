package admin

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func (s *Service) ProDevices(ctx context.Context, actor domain.AdminUser, userID string) ([]domain.ProDeviceAuthorization, []domain.ProDeviceEvent, error) {
	if !actor.IsActive || actor.Role != "admin" {
		return nil, nil, domain.ErrProDeviceManagementForbidden
	}
	if _, err := s.repo.UserByID(ctx, userID); err != nil {
		return nil, nil, err
	}
	devices, err := s.repo.ProDeviceAuthorizations(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	events, err := s.repo.ProDeviceEvents(ctx, userID, 100)
	return devices, events, err
}

func (s *Service) RevokeProDevice(ctx context.Context, actor domain.AdminUser, userID, authorizationID, reason string) error {
	if !actor.IsActive || actor.Role != "admin" {
		return domain.ErrProDeviceManagementForbidden
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 200 {
		return domain.InvalidField("reason", "required; maximum 200 characters")
	}
	// The repository commits slot release, session revocation and the admin
	// audit together. An audit failure must not silently commit the override.
	return s.repo.RevokeProDevice(ctx, userID, authorizationID, actor.ID, reason, true, s.clock.Now())
}
