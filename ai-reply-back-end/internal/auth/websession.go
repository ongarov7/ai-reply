package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// AuthenticateWebSession validates an opaque, HttpOnly browser session. Its
// backing refresh-token row is never rotated or exposed to JavaScript. Native
// sessions keep their existing access-token and rotation rules.
func (s *Service) AuthenticateWebSession(ctx context.Context, token string) (domain.User, error) {
	if strings.TrimSpace(token) == "" {
		return domain.User{}, domain.ErrUnauthorized
	}
	stored, err := s.repo.RefreshTokenByHash(ctx, HashToken(s.cfg.RefreshSecret, token))
	if errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, domain.ErrUnauthorized
	}
	if err != nil {
		return domain.User{}, err
	}
	if stored.RevokedAt != nil || !s.clock.Now().Before(stored.ExpiresAt) {
		return domain.User{}, domain.ErrUnauthorized
	}
	device, err := s.repo.Device(ctx, stored.DeviceID)
	if err != nil || device.Platform != "web" || device.UserID != stored.UserID || device.RevokedAt != nil {
		return domain.User{}, domain.ErrUnauthorized
	}
	user, err := s.repo.UserByID(ctx, stored.UserID)
	if err != nil {
		return domain.User{}, domain.ErrUnauthorized
	}
	if user.Status == domain.UserDisabled {
		return domain.User{}, domain.ErrAccountDisabled
	}
	return user, nil
}
