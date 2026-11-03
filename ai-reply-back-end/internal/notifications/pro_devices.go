package notifications

import (
	"context"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

const TypeProDeviceLimit = domain.TypeProDeviceLimit

// WithProDeviceNotificationDelivery also gates already-created outbox rows
// during rollback without disabling unrelated account notifications.
func (s *Service) WithProDeviceNotificationDelivery(enabled bool) *Service {
	s.proDeviceDelivery.Store(enabled)
	return s
}

// QueueProDeviceLimit only writes a durable job. It performs no provider calls
// and must only be invoked after a real enforced denial, never in shadow mode.
func (s *Service) QueueProDeviceLimit(ctx context.Context, userID, deviceID, category, platform string, now time.Time, cooldown time.Duration) error {
	if category != "mobile" && category != "desktop" {
		return domain.InvalidField("device_category", "unknown")
	}
	if (category == "mobile" && platform != "ios" && platform != "android" && platform != "ipados") ||
		(category == "desktop" && platform != "macos" && platform != "windows") {
		return domain.InvalidField("platform", "does not match device category")
	}
	if cooldown <= 0 {
		cooldown = time.Hour
	}
	created, err := s.repo.QueueProDeviceNotification(ctx, userID, deviceID, category, platform, now, cooldown)
	if err != nil {
		return err
	}
	if created {
		s.log.Info("Pro device notification queued", "event", "pro_device_notification_queued", "user_id", userID, "device_category", category)
	}
	return nil
}

// PumpProDeviceNotifications moves a bounded batch of durable jobs into the
// existing notification outbox. A restart between creation and completion is
// safe: NotifyUser uses the stable job ID as its idempotency key.
func (s *Service) PumpProDeviceNotifications(ctx context.Context, cooldowns ...time.Duration) error {
	cooldown := time.Hour
	if len(cooldowns) > 0 && cooldowns[0] > 0 {
		cooldown = cooldowns[0]
	}
	if err := s.repo.ReconcileProDeviceNotificationIntent(ctx, s.clock.Now(), cooldown); err != nil {
		return err
	}
	jobs, err := s.repo.PendingProDeviceNotifications(ctx, s.clock.Now())
	if err != nil {
		return err
	}
	var errs []error
	for _, j := range jobs {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		targets, err := s.repo.ProDevicePushTargets(ctx, j.UserID, j.DeviceID)
		if targets == nil {
			targets = []string{}
		}
		if err == nil {
			var result NotifyResult
			result, err = s.NotifyUser(ctx, UserNotification{
				UserID: j.UserID, IdempotencyKey: TypeProDeviceLimit + ":" + j.ID,
				Type: TypeProDeviceLimit, Category: domain.CategorySecurity,
				Link: "aireply://support", TitleKey: "push.pro_device_limit.title", BodyKey: "push.pro_device_limit.body",
				PushInstallationIDs: targets, Email: true, Transactional: true,
				Params: map[string]string{"platform": j.Platform, "category": j.Category, "date": j.OccurredAt.In(s.loc).Format("2006-01-02 15:04:05 MST"), "blocked_device_id": j.DeviceID},
				ParamsFor: func(locale string) map[string]string {
					category := j.Category
					if s.translate != nil {
						category = s.translate(locale, "pro_devices.category."+j.Category)
					}
					return map[string]string{"category": category}
				},
			})
			if err == nil {
				err = s.repo.CompleteProDeviceNotification(ctx, j.ID, result.NotificationID, s.clock.Now())
			}
		}
		if err != nil {
			s.log.Error("Pro device notification queue failed", "event", "pro_device_notification_failed", "job_id", j.ID, "user_id", j.UserID)
			errs = append(errs, err)
			if retryErr := s.repo.RetryProDeviceNotification(ctx, j.ID, s.clock.Now()); retryErr != nil {
				errs = append(errs, retryErr)
			}
		}
	}
	return errors.Join(errs...)
}
