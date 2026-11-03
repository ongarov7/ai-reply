package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// ProDeviceNotificationJob is an internal durable job, never an API payload.
type ProDeviceNotificationJob struct {
	ID, UserID, DeviceID, Category, Platform string
	OccurredAt                               time.Time
}

// QueueProDeviceNotification atomically applies a rolling per-device cooldown
// and an account-wide ceiling (three notices per cooldown), even across workers.
// Attempts themselves are recorded independently in the security event log.
func (s *Store) QueueProDeviceNotification(ctx context.Context, userID, deviceID, category, platform string, now time.Time, cooldown time.Duration) (bool, error) {
	created := false
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		created, err = queueProDeviceNotification(ctx, tx, userID, deviceID, category, platform, now, cooldown)
		return err
	})
	return created, err
}

func queueProDeviceNotification(ctx context.Context, tx *sql.Tx, userID, deviceID, category, platform string, now time.Time, cooldown time.Duration) (bool, error) {
	var duplicate, count int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pro_device_notification_jobs
			WHERE user_id=? AND device_id=? AND device_category=? AND occurred_at>?)`,
		userID, deviceID, category, ms(now.Add(-cooldown))).Scan(&duplicate); err != nil {
		return false, err
	}
	if duplicate != 0 {
		return false, nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_device_notification_jobs WHERE user_id=? AND occurred_at>?`,
		userID, ms(now.Add(-cooldown))).Scan(&count); err != nil {
		return false, err
	}
	if count >= 3 {
		return false, nil
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO pro_device_notification_jobs
			(id,user_id,device_id,device_category,platform,occurred_at,next_attempt_at,updated_at)
			SELECT ?,?,?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM devices WHERE id=? AND user_id=?)`,
		traits.NewID(), userID, deviceID, category, platform, ms(now), ms(now), ms(now), deviceID, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ReconcileProDeviceNotificationIntent atomically queues or cooldown-suppresses
// each requested enforced event and marks that intent processed. Old rows and
// shadow evaluations never participate. A failed transaction retains intent.
func (s *Store) ReconcileProDeviceNotificationIntent(ctx context.Context, now time.Time, cooldown time.Duration) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,user_id,COALESCE(device_id,''),category,platform,created_at FROM pro_device_events
			WHERE notify_requested=1 AND notification_processed_at IS NULL AND kind='limit_reached' AND mode='enforce'
			ORDER BY created_at,id LIMIT 25`)
		if err != nil {
			return err
		}
		jobs := []ProDeviceNotificationJob{}
		for rows.Next() {
			var j ProDeviceNotificationJob
			var created int64
			if err := rows.Scan(&j.ID, &j.UserID, &j.DeviceID, &j.Category, &j.Platform, &created); err != nil {
				rows.Close()
				return err
			}
			j.OccurredAt = timeFrom(created)
			jobs = append(jobs, j)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, j := range jobs {
			if _, err := queueProDeviceNotification(ctx, tx, j.UserID, j.DeviceID, j.Category, j.Platform, j.OccurredAt, cooldown); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE pro_device_events SET notification_processed_at=? WHERE id=?`, ms(now), j.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) PendingProDeviceNotifications(ctx context.Context, now time.Time) ([]ProDeviceNotificationJob, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT id,user_id,device_id,device_category,platform,occurred_at
		FROM pro_device_notification_jobs WHERE status='pending' AND next_attempt_at<=? ORDER BY occurred_at,id LIMIT 25`, ms(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProDeviceNotificationJob{}
	for rows.Next() {
		var j ProDeviceNotificationJob
		var occurred int64
		if err := rows.Scan(&j.ID, &j.UserID, &j.DeviceID, &j.Category, &j.Platform, &occurred); err != nil {
			return nil, err
		}
		j.OccurredAt = timeFrom(occurred)
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) CompleteProDeviceNotification(ctx context.Context, id, notificationID string, now time.Time) error {
	_, err := s.db.Writer().ExecContext(ctx, `UPDATE pro_device_notification_jobs SET status='queued',notification_id=?,last_error='',updated_at=? WHERE id=? AND status='pending'`, nullText(notificationID), ms(now), id)
	return err
}

// RetryProDeviceNotification retains a failed queueing attempt for bounded retry.
// Provider retries and delivery outcomes belong to the existing delivery outbox.
func (s *Store) RetryProDeviceNotification(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Writer().ExecContext(ctx, `UPDATE pro_device_notification_jobs SET attempts=attempts+1,
		status=CASE WHEN attempts>=9 THEN 'failed' ELSE 'pending' END,
		last_error='notification_queue_failed',next_attempt_at=?,updated_at=? WHERE id=? AND status='pending'`, ms(now.Add(time.Minute)), ms(now), id)
	return err
}
