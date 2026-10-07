package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// deliveryInsert — хабарламаны сүзгіге сай орнатуларға кезекке қоятын INSERT.
//
// INSERT OR IGNORE + the unique (notification_id, installation_id) index:
// running the same fan-out twice (a retried request, a restarted worker, a
// second backend process) cannot queue a second copy for the same device.
const deliveryInsert = `
	INSERT OR IGNORE INTO notification_deliveries
		(id, notification_id, campaign_id, channel, installation_id, user_id, platform, provider, status,
		 attempt_count, next_attempt_at, created_at, updated_at)
	SELECT lower(hex(randomblob(16))), ?, ?, 'push', i.id, i.user_id, i.platform, i.push_provider, 'queued',
	       0, ?, ?, ?` + audienceFrom

// newDeliveryID — SQL жасайтын жеткізу идентификаторларымен бірдей пішім (32 hex).
func newDeliveryID() string { return traits.RandomToken(16) }

func encodeData(data map[string]string) string {
	if len(data) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func decodeData(raw string) map[string]string {
	out := map[string]string{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

const insertNotification = `
	INSERT OR IGNORE INTO notifications (id, dedupe_key, idempotency_key, campaign_id, user_id,
		category, type, locale, title, body, link, data, params, created_at)
	VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

// UserChannels — бір қолданушының хабарламасы қай арнамен кетеді.
type UserChannels struct {
	// PushSkip — "" queues the push to every eligible device (none at all is
	// recorded as one skipped row, "no_devices"); a code records one skipped
	// row with that code instead (e.g. "push_disabled").
	PushSkip string
	// Email — one e-mail delivery; EmailSkip records it as skipped with that code.
	Email     bool
	EmailSkip string
}

// UserNotificationResult — CreateUserNotification не істегені.
type UserNotificationResult struct {
	ID      string
	Created bool   // false: the dedupe key existed, nothing new was queued
	Devices int    // push deliveries queued now
	Skipped string // why no push was queued ("" when Devices > 0)
	Email   bool   // an e-mail delivery was queued now
}

// CreateUserNotification — бір қолданушыға бір логикалық хабарлама және оның
// жеткізулері (push, қажет болса пошта), бір транзакцияда.
func (s *Store) CreateUserNotification(ctx context.Context, n domain.Notification, q AudienceQuery, ch UserChannels) (UserNotificationResult, error) {
	var out UserNotificationResult
	now := ms(q.Now)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		out = UserNotificationResult{}
		candidate := traits.NewID()
		res, err := tx.ExecContext(ctx, insertNotification,
			candidate, n.DedupeKey, n.IdempotencyKey, nil, n.UserID, n.Category, n.Type, n.Locale,
			n.Title, n.Body, n.Link, encodeData(n.Data), encodeData(n.Params), now)
		if err != nil {
			return err
		}
		if inserted, _ := res.RowsAffected(); inserted == 0 {
			return tx.QueryRowContext(ctx, `SELECT id FROM notifications WHERE dedupe_key = ?`, n.DedupeKey).Scan(&out.ID)
		}
		out.ID, out.Created = candidate, true

		out.Skipped = ch.PushSkip
		if ch.PushSkip == "" {
			where, args := eligible(q)
			res, err = tx.ExecContext(ctx, deliveryInsert+where, append([]any{out.ID, nil, now, now, now}, args...)...)
			if err != nil {
				return err
			}
			affected, _ := res.RowsAffected()
			out.Devices = int(affected)
			if out.Devices == 0 {
				out.Skipped = "no_devices"
			}
		}
		if out.Skipped != "" {
			if err := insertOwnDelivery(ctx, tx, out.ID, n.UserID, domain.ChannelPush, "", out.Skipped, now); err != nil {
				return err
			}
		}
		if ch.Email {
			status := domain.DeliveryQueued
			if ch.EmailSkip != "" {
				status = domain.DeliverySkipped
			}
			if err := insertOwnDelivery(ctx, tx, out.ID, n.UserID, domain.ChannelEmail, domain.ProviderEmail,
				ch.EmailSkip, now); err != nil {
				return err
			}
			out.Email = status == domain.DeliveryQueued
		}
		return nil
	})
	return out, err
}

// insertOwnDelivery — құрылғысыз жол: хат не push жіберілмегені туралы жазба.
// skip == "" — кезекке қойылады, әйтпесе сол кодпен skipped.
func insertOwnDelivery(ctx context.Context, tx *sql.Tx, notificationID, userID, channel, provider, skip string, now int64) error {
	status := domain.DeliveryQueued
	if skip != "" {
		status = domain.DeliverySkipped
	}
	_, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO notification_deliveries
			(id, notification_id, campaign_id, channel, installation_id, user_id, platform, provider, status,
			 attempt_count, next_attempt_at, error_code, created_at, updated_at)
		VALUES (?,?,NULL,?,NULL,?,'',?,?,0,?,?,?,?)`,
		newDeliveryID(), notificationID, channel, nullText(userID), provider, status, now, skip, now, now)
	return err
}

// ---------------------------------------------------------------- campaigns

const campaignColumns = `id, name, title, body, content, fallback_locale, category, link, data, audience_filter,
	status, created_by, COALESCE((SELECT a.email FROM admin_users a WHERE a.id = created_by), ''),
	COALESCE(idempotency_key, ''), recipient_count, device_count, created_at, updated_at, queued_at,
	started_at, completed_at, cancelled_at, final_stats`

func scanCampaign(row interface{ Scan(...any) error }) (domain.Campaign, error) {
	var (
		c                                       domain.Campaign
		content, data, audience, final          string
		created, updated                        int64
		queued, started, completed, cancelledAt sql.NullInt64
	)
	err := row.Scan(&c.ID, &c.Name, &c.Title, &c.Body, &content, &c.FallbackLocale, &c.Category, &c.Link, &data,
		&audience, &c.Status, &c.CreatedBy, &c.CreatedByEmail, &c.IdempotencyKey, &c.RecipientCount, &c.DeviceCount,
		&created, &updated, &queued, &started, &completed, &cancelledAt, &final)
	if err != nil {
		return domain.Campaign{}, err
	}
	c.Content = map[string]domain.LocalizedText{}
	_ = json.Unmarshal([]byte(content), &c.Content)
	c.Data = decodeData(data)
	_ = json.Unmarshal([]byte(audience), &c.Audience)
	if final != "" {
		var stats domain.DeliveryStats
		if json.Unmarshal([]byte(final), &stats) == nil {
			c.FinalStats = &stats
		}
	}
	c.CreatedAt, c.UpdatedAt = timeFrom(created), timeFrom(updated)
	c.QueuedAt, c.StartedAt, c.CompletedAt, c.CancelledAt = timePtr(queued), timePtr(started), timePtr(completed), timePtr(cancelledAt)
	return c, nil
}

// CreateCampaign — жаңа науқан. Сол Idempotency-Key-мен екінші сұраныс бірінші науқанды қайтарады.
func (s *Store) CreateCampaign(ctx context.Context, c domain.Campaign) (domain.Campaign, bool, error) {
	if c.ID == "" {
		c.ID = traits.NewID()
	}
	audience, err := json.Marshal(c.Audience)
	if err != nil {
		return domain.Campaign{}, false, err
	}
	content, err := json.Marshal(c.Content)
	if err != nil {
		return domain.Campaign{}, false, err
	}
	summary := "all"
	if string(audience) != "{}" {
		summary = "filtered"
	}
	now := ms(c.CreatedAt)
	_, err = s.db.Writer().ExecContext(ctx, `
		INSERT INTO notification_campaigns (id, title, body, audience, status, created_by, created_at, updated_at,
			name, category, link, data, audience_filter, content, fallback_locale, idempotency_key)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Title, c.Body, summary, c.Status, c.CreatedBy, now, now,
		c.Name, c.Category, c.Link, encodeData(c.Data), string(audience), string(content), c.FallbackLocale,
		nullText(c.IdempotencyKey))
	if err != nil {
		if isUnique(err) && c.IdempotencyKey != "" {
			existing, lookupErr := s.CampaignByIdempotencyKey(ctx, c.IdempotencyKey)
			if lookupErr != nil {
				return domain.Campaign{}, false, lookupErr
			}
			return existing, false, nil
		}
		return domain.Campaign{}, false, err
	}
	created, err := s.Campaign(ctx, c.ID)
	return created, true, err
}

// Campaign — бір науқан.
func (s *Store) Campaign(ctx context.Context, id string) (domain.Campaign, error) {
	c, err := scanCampaign(s.db.Reader().QueryRowContext(ctx,
		`SELECT `+campaignColumns+` FROM notification_campaigns WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Campaign{}, domain.ErrNotFound
	}
	return c, err
}

// CampaignByIdempotencyKey — Idempotency-Key бойынша.
func (s *Store) CampaignByIdempotencyKey(ctx context.Context, key string) (domain.Campaign, error) {
	c, err := scanCampaign(s.db.Writer().QueryRowContext(ctx,
		`SELECT `+campaignColumns+` FROM notification_campaigns WHERE idempotency_key = ?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Campaign{}, domain.ErrNotFound
	}
	return c, err
}

// ListCampaigns — соңғылары алдымен.
func (s *Store) ListCampaigns(ctx context.Context, status string, page traits.Page) ([]domain.Campaign, int, error) {
	where, args := "1=1", []any{}
	if status != "" {
		where, args = "status = ?", []any{status}
	}
	var total int
	if err := s.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_campaigns WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT `+campaignColumns+` FROM notification_campaigns
		WHERE `+where+` ORDER BY created_at DESC, id LIMIT ? OFFSET ?`, append(args, page.Limit, page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Campaign
	for rows.Next() {
		c, err := scanCampaign(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// QueueCampaign — draft → queued. Басқа күйдегі науқан өзгермейді (қайта басу зиянсыз).
func (s *Store) QueueCampaign(ctx context.Context, id string, now time.Time) (bool, error) {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE notification_campaigns SET status = 'queued', queued_at = ?, updated_at = ?
		WHERE id = ? AND status = 'draft'`, ms(now), ms(now), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// CancelCampaign — әлі жіберілмеген жеткізулерді тоқтатады.
func (s *Store) CancelCampaign(ctx context.Context, id string, now time.Time) (bool, error) {
	var changed bool
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		changed = false
		res, err := tx.ExecContext(ctx, `
			UPDATE notification_campaigns SET status = 'cancelled', cancelled_at = ?, updated_at = ?
			WHERE id = ? AND status IN ('draft','queued','processing')`, ms(now), ms(now), id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return nil
		}
		changed = true
		_, err = tx.ExecContext(ctx, `
			UPDATE notification_deliveries SET status = 'cancelled', error_code = 'campaign_cancelled', updated_at = ?
			WHERE campaign_id = ? AND status IN ('queued','retrying')`, ms(now), id)
		return err
	})
	return changed, err
}

// QueuedCampaignIDs — жіберуді күтіп тұрған науқандар.
func (s *Store) QueuedCampaignIDs(ctx context.Context, limit int) ([]string, error) {
	return s.ids(ctx, `SELECT id FROM notification_campaigns WHERE status = 'queued' ORDER BY queued_at LIMIT ?`, limit)
}

// ProcessingCampaignIDs — жеткізілуі жүріп жатқан науқандар.
func (s *Store) ProcessingCampaignIDs(ctx context.Context, limit int) ([]string, error) {
	return s.ids(ctx, `SELECT id FROM notification_campaigns WHERE status = 'processing' ORDER BY started_at LIMIT ?`, limit)
}

func (s *Store) ids(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// FanOutCampaign — queued → processing және алушыларды тіл бойынша кезекке қою, бір транзакцияда.
//
// The status change is the claim: of two workers that see the same queued
// campaign, one UPDATE changes a row and fans out, the other changes nothing
// and returns claimed=false. Each language gets its own notification row
// (dedupe "campaign:<id>:<lang>") with the text from texts, and the devices
// whose notification language is that one. A crash in the middle rolls
// everything back and the campaign stays queued for the next attempt.
func (s *Store) FanOutCampaign(ctx context.Context, c domain.Campaign, q AudienceQuery, texts map[string]domain.LocalizedText) (claimed bool, devices, users int, err error) {
	now := ms(q.Now)
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		claimed, devices, users = false, 0, 0
		res, err := tx.ExecContext(ctx, `
			UPDATE notification_campaigns SET status = 'processing', started_at = ?, updated_at = ?
			WHERE id = ? AND status = 'queued'`, now, now, c.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		claimed = true

		for _, lang := range domain.Locales {
			text, ok := texts[lang]
			if !ok {
				continue
			}
			dedupe := "campaign:" + c.ID + ":" + lang
			if _, err := tx.ExecContext(ctx, insertNotification,
				traits.NewID(), dedupe, dedupe, c.ID, nil, c.Category, domain.TypeCampaign, lang,
				text.Title, text.Body, c.Link, encodeData(c.Data), "{}", now); err != nil {
				return err
			}
			var notificationID string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM notifications WHERE dedupe_key = ?`, dedupe).
				Scan(&notificationID); err != nil {
				return err
			}
			q.Language = lang
			where, args := eligible(q)
			res, err := tx.ExecContext(ctx, deliveryInsert+where,
				append([]any{notificationID, c.ID, now, now, now}, args...)...)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				// Nobody reads this language: no empty notification is kept.
				if _, err := tx.ExecContext(ctx, `DELETE FROM notifications WHERE id = ?
					AND NOT EXISTS (SELECT 1 FROM notification_deliveries d WHERE d.notification_id = notifications.id)`,
					notificationID); err != nil {
					return err
				}
			}
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*), COUNT(DISTINCT user_id) FROM notification_deliveries WHERE campaign_id = ?`, c.ID).
			Scan(&devices, &users); err != nil {
			return err
		}
		if devices == 0 {
			_, err = tx.ExecContext(ctx, `
				UPDATE notification_campaigns SET status = 'completed', completed_at = ?, updated_at = ?,
					recipient_count = 0, device_count = 0, final_stats = '{"total":0}'
				WHERE id = ?`, now, now, c.ID)
			return err
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE notification_campaigns SET recipient_count = ?, device_count = ?, updated_at = ? WHERE id = ?`,
			users, devices, now, c.ID)
		return err
	})
	return claimed, devices, users, err
}

// CampaignStats — науқан жеткізулерінің күйі (live, жолдардан есептеледі), тіл бойынша да.
func (s *Store) CampaignStats(ctx context.Context, campaignID string) (domain.DeliveryStats, error) {
	var stats domain.DeliveryStats
	locales := map[string]string{}
	rows, err := s.db.Reader().QueryContext(ctx,
		`SELECT id, locale FROM notifications WHERE campaign_id = ?`, campaignID)
	if err != nil {
		return stats, err
	}
	for rows.Next() {
		var id, locale string
		if err := rows.Scan(&id, &locale); err != nil {
			rows.Close()
			return stats, err
		}
		locales[id] = locale
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return stats, err
	}

	rows, err = s.db.Reader().QueryContext(ctx, `
		SELECT notification_id, status, platform, COUNT(*),
		       COALESCE(SUM(CASE WHEN opened_at IS NOT NULL THEN 1 ELSE 0 END), 0)
		FROM notification_deliveries WHERE campaign_id = ?
		GROUP BY notification_id, status, platform`, campaignID)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var notificationID, status, platform string
		var count, opened int
		if err := rows.Scan(&notificationID, &status, &platform, &count, &opened); err != nil {
			return stats, err
		}
		stats.Add(status, count)
		stats.Opened += opened
		stats.AddLanguage(locales[notificationID], status, count, opened)
		switch platform {
		case domain.PlatformAndroid:
			stats.Android += count
		case domain.PlatformIOS:
			stats.IOS += count
		}
	}
	return stats, rows.Err()
}

// ErrorCount — қате коды және саны.
type ErrorCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// CampaignErrors — сәтсіз жеткізулердің кодтары бойынша бөлінісі.
func (s *Store) CampaignErrors(ctx context.Context, campaignID string) ([]ErrorCount, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT CASE WHEN error_code = '' THEN status ELSE error_code END, COUNT(*)
		FROM notification_deliveries
		WHERE campaign_id = ? AND status IN ('provider_failed','invalid_token','skipped','retrying')
		GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT 12`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ErrorCount{}
	for rows.Next() {
		var e ErrorCount
		if err := rows.Scan(&e.Code, &e.Count); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CancelledCampaignsWithoutStats — тоқтатылған, қорытынды есебі әлі сақталмаған науқандар.
func (s *Store) CancelledCampaignsWithoutStats(ctx context.Context, limit int) ([]string, error) {
	return s.ids(ctx, `SELECT id FROM notification_campaigns WHERE status = 'cancelled' AND final_stats = ''
		ORDER BY cancelled_at LIMIT ?`, limit)
}

// SaveCancelledCampaignStats — тоқтатылған науқанның соңғы есебі (жеткізулері өшкенде де қалады).
func (s *Store) SaveCancelledCampaignStats(ctx context.Context, id string, stats domain.DeliveryStats, now time.Time) (bool, error) {
	raw, err := json.Marshal(stats)
	if err != nil {
		return false, err
	}
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE notification_campaigns SET final_stats = ?, updated_at = ?
		WHERE id = ? AND status = 'cancelled' AND final_stats = ''`, string(raw), ms(now), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// FinishCampaign — processing → қорытынды күй, соңғы есеп жолмен бірге сақталады.
func (s *Store) FinishCampaign(ctx context.Context, id, status string, stats domain.DeliveryStats, now time.Time) (bool, error) {
	raw, err := json.Marshal(stats)
	if err != nil {
		return false, err
	}
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE notification_campaigns SET status = ?, completed_at = ?, final_stats = ?, updated_at = ?
		WHERE id = ? AND status = 'processing'`, status, ms(now), string(raw), ms(now), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ---------------------------------------------------------------- deliveries

// ClaimedDelivery — жұмысшы алған жеткізу.
type ClaimedDelivery struct {
	ID             string
	NotificationID string
	CampaignID     string
	Channel        string
	InstallationID string
	UserID         string
	Platform       string
	Provider       string
	AttemptCount   int
}

// inCancelledCampaign — жол әкімші тоқтатқан науқанға тиесілі.
const inCancelledCampaign = `campaign_id IN (SELECT id FROM notification_campaigns WHERE status = 'cancelled')`

// ClaimDeliveries — мерзімі жеткен жолдарды lease-пен алады (бір атомарлы UPDATE ... RETURNING).
//
// SQLite runs one write transaction at a time for the whole database file,
// across every process that opens it, so two workers can never claim the
// same row. A row whose lease ran out (its worker died mid-send) becomes
// claimable again; that retry counts as an attempt, unless its campaign was
// cancelled meanwhile: then it ends as cancelled. Automatic notifications
// (a purchase confirmation) are claimed before campaign rows, so a large
// campaign never delays them.
func (s *Store) ClaimDeliveries(ctx context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]ClaimedDelivery, error) {
	var out []ClaimedDelivery
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		out = nil
		if _, err := tx.ExecContext(ctx, `
			UPDATE notification_deliveries SET status = 'cancelled', error_code = 'campaign_cancelled',
				lease_owner = '', lease_until = NULL, updated_at = ?
			WHERE status = 'sending' AND lease_until < ? AND `+inCancelledCampaign, ms(now), ms(now)); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `
			UPDATE notification_deliveries
			SET status = 'sending', lease_owner = ?, lease_until = ?, attempt_count = attempt_count + 1, updated_at = ?
			WHERE id IN (
				SELECT id FROM notification_deliveries
				WHERE (status IN ('queued','retrying') AND next_attempt_at <= ?)
				   OR (status = 'sending' AND lease_until < ?)
				ORDER BY campaign_id IS NOT NULL, next_attempt_at LIMIT ?)
			RETURNING id, notification_id, COALESCE(campaign_id, ''), channel, COALESCE(installation_id, ''),
			          COALESCE(user_id, ''), platform, provider, attempt_count`,
			owner, ms(now.Add(lease)), ms(now), ms(now), ms(now), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d ClaimedDelivery
			if err := rows.Scan(&d.ID, &d.NotificationID, &d.CampaignID, &d.Channel, &d.InstallationID, &d.UserID,
				&d.Platform, &d.Provider, &d.AttemptCount); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}

// DeliveryOutcome — бір жіберу әрекетінің нәтижесі.
type DeliveryOutcome struct {
	Status            string
	NextAttemptAt     time.Time
	ProviderMessageID string
	TokenFingerprint  string
	ErrorCode         string
	ErrorDetail       string
	Now               time.Time
}

// FinishDelivery — нәтижені жазады, тек lease әлі осы жұмысшыда болса. Жазылған күйді
// қайтарады ("" — lease енді бұл жұмысшыда емес, ештеңе жазылмады).
//
// Cancelling a campaign stops only the rows nobody is sending: a send that
// was in flight and asks for a retry ends here as cancelled instead.
func (s *Store) FinishDelivery(ctx context.Context, id, owner string, o DeliveryOutcome) (string, error) {
	now := ms(o.Now)
	var stored string
	err := s.db.Writer().QueryRowContext(ctx, `
		UPDATE notification_deliveries SET
			status = CASE WHEN ? = 'retrying' AND `+inCancelledCampaign+` THEN 'cancelled' ELSE ? END,
			error_code = CASE WHEN ? = 'retrying' AND `+inCancelledCampaign+` THEN 'campaign_cancelled' ELSE ? END,
			lease_owner = '', lease_until = NULL,
			next_attempt_at = CASE WHEN ? = 'retrying' THEN ? ELSE next_attempt_at END,
			provider_message_id = CASE WHEN ? <> '' THEN ? ELSE provider_message_id END,
			token_fingerprint = CASE WHEN ? <> '' THEN ? ELSE token_fingerprint END,
			error_detail = ?, updated_at = ?,
			sent_at = CASE WHEN ? = 'provider_accepted' THEN ? ELSE sent_at END,
			failed_at = CASE WHEN ? IN ('provider_failed','invalid_token') THEN ? ELSE failed_at END
		WHERE id = ? AND lease_owner = ? AND status = 'sending'
		RETURNING status`,
		o.Status, o.Status, o.Status, o.ErrorCode, o.Status, ms(o.NextAttemptAt),
		o.ProviderMessageID, o.ProviderMessageID, o.TokenFingerprint, o.TokenFingerprint,
		o.ErrorDetail, now, o.Status, now, o.Status, now, id, owner).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return stored, err
}

// ReleaseDeliveries — алынған, бірақ жіберілмеген жолдарды кезекке қайтарады (жұмысшы тоқтағанда).
//
// The attempt the claim counted is given back. A row whose campaign was
// cancelled meanwhile ends as cancelled.
func (s *Store) ReleaseDeliveries(ctx context.Context, owner string, ids []string, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	args := []any{ms(now)}
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := s.db.Writer().ExecContext(ctx, `
		UPDATE notification_deliveries SET
			status = CASE WHEN `+inCancelledCampaign+` THEN 'cancelled'
			              WHEN attempt_count > 1 THEN 'retrying' ELSE 'queued' END,
			error_code = CASE WHEN `+inCancelledCampaign+` THEN 'campaign_cancelled' ELSE error_code END,
			attempt_count = attempt_count - 1, lease_owner = '', lease_until = NULL, updated_at = ?
		WHERE id IN (`+placeholders(len(ids))+`) AND lease_owner = ? AND status = 'sending'`,
		append(args, owner)...)
	return err
}

// NotificationByID — хабарлама мазмұны (жұмысшыға).
func (s *Store) NotificationByID(ctx context.Context, id string) (domain.Notification, error) {
	var (
		n                  domain.Notification
		campaignID, userID sql.NullString
		data, params       string
		created            int64
	)
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT id, dedupe_key, idempotency_key, campaign_id, user_id, category, type, locale, title, body,
		       link, data, params, created_at
		FROM notifications WHERE id = ?`, id).
		Scan(&n.ID, &n.DedupeKey, &n.IdempotencyKey, &campaignID, &userID, &n.Category, &n.Type, &n.Locale,
			&n.Title, &n.Body, &n.Link, &data, &params, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Notification{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Notification{}, err
	}
	n.CampaignID, n.UserID = text(campaignID), text(userID)
	n.Data, n.Params = decodeData(data), decodeData(params)
	n.CreatedAt = timeFrom(created)
	return n, nil
}

// MarkDeliveryOpened — қосымша «ашылды» деп хабарлаған жеткізу (тек сол орнатудан).
func (s *Store) MarkDeliveryOpened(ctx context.Context, deliveryID, installationID string, at time.Time) (bool, error) {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE notification_deliveries SET opened_at = COALESCE(opened_at, ?), updated_at = ?
		WHERE id = ? AND channel = 'push'
		  AND installation_id = (SELECT id FROM app_installations WHERE installation_id = ?)`,
		ms(at), ms(at), deliveryID, installationID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// EmailRecipient — хат жіберу сәтіндегі алушы.
type EmailRecipient struct {
	Email    string
	Verified bool
	Active   bool // status active and an ordinary account
}

// NotificationEmailRecipient — тіркелгінің поштасы (жіберу сәтінде оқылады, кезекте сақталмайды).
func (s *Store) NotificationEmailRecipient(ctx context.Context, userID string) (EmailRecipient, error) {
	var (
		r        EmailRecipient
		email    sql.NullString
		verified sql.NullInt64
		active   int
	)
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT email, email_verified_at,
		       CASE WHEN status = 'active' AND kind = 'account' AND deleted_at IS NULL THEN 1 ELSE 0 END
		FROM users WHERE id = ?`, userID).Scan(&email, &verified, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return EmailRecipient{}, domain.ErrNotFound
	}
	if err != nil {
		return EmailRecipient{}, err
	}
	r.Email, r.Verified, r.Active = text(email), verified.Valid, active == 1
	return r, nil
}

// LatestInstallationLocale — қолданушының соңғы көрінген құрылғысының тілі (қолдау көрсетілсе).
func (s *Store) LatestInstallationLocale(ctx context.Context, userID string) (string, error) {
	var locale string
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT locale FROM app_installations
		WHERE user_id = ? AND locale IN (`+supportedLanguages+`)
		ORDER BY last_seen_at DESC, id LIMIT 1`, userID).Scan(&locale)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return locale, err
}

// DeliveryFilter — әкімші тізімі.
type DeliveryFilter struct {
	CampaignID     string
	UserID         string
	InstallationID string
	Status         string
	Platform       string
	Channel        string
	Type           string
	Locale         string
	Source         string // campaign | automatic | ""
	Page           traits.Page
}

// Хабарлама көзі (жеткізулер тізімінің сүзгісі).
const (
	SourceCampaign  = "campaign"
	SourceAutomatic = "automatic"
)

// DeliveryRow — жеткізу мен оның хабарламасы, құрылғысы (токенсіз).
type DeliveryRow struct {
	Delivery     domain.Delivery
	Title        string
	Type         string
	Category     string
	Locale       string
	CampaignName string
	DeviceModel  string
	Manufacturer string
	AppVersion   string
}

// ListDeliveries — сүзгі және беттеу.
func (s *Store) ListDeliveries(ctx context.Context, f DeliveryFilter) ([]DeliveryRow, int, error) {
	where := []string{"1=1"}
	args := []any{}
	for _, c := range []struct{ column, value string }{
		{"d.campaign_id", f.CampaignID}, {"d.user_id", f.UserID}, {"d.installation_id", f.InstallationID},
		{"d.status", f.Status}, {"d.platform", f.Platform}, {"d.channel", f.Channel},
		{"n.type", f.Type}, {"n.locale", f.Locale},
	} {
		if c.value != "" {
			where = append(where, c.column+" = ?")
			args = append(args, c.value)
		}
	}
	switch f.Source {
	case SourceCampaign:
		where = append(where, "d.campaign_id IS NOT NULL")
	case SourceAutomatic:
		where = append(where, "d.campaign_id IS NULL")
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_deliveries d
		JOIN notifications n ON n.id = d.notification_id WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT d.id, d.notification_id, COALESCE(d.campaign_id, ''), d.channel, COALESCE(d.installation_id, ''),
		       COALESCE(d.user_id, ''), d.platform, d.provider, d.status, d.attempt_count, d.next_attempt_at,
		       d.provider_message_id, d.token_fingerprint, d.error_code, d.error_detail, d.created_at,
		       d.updated_at, d.sent_at, d.failed_at, d.opened_at,
		       n.title, n.type, n.category, n.locale, COALESCE(c.name, ''),
		       COALESCE(i.device_model, ''), COALESCE(i.manufacturer, ''), COALESCE(i.app_version, '')
		FROM notification_deliveries d
		JOIN notifications n ON n.id = d.notification_id
		LEFT JOIN notification_campaigns c ON c.id = d.campaign_id
		LEFT JOIN app_installations i ON i.id = d.installation_id
		WHERE `+clause+` ORDER BY d.created_at DESC, d.id LIMIT ? OFFSET ?`,
		append(args, f.Page.Limit, f.Page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []DeliveryRow
	for rows.Next() {
		var (
			r                      DeliveryRow
			next, created, updated int64
			sent, failed, openedAt sql.NullInt64
		)
		d := &r.Delivery
		if err := rows.Scan(&d.ID, &d.NotificationID, &d.CampaignID, &d.Channel, &d.InstallationID, &d.UserID,
			&d.Platform, &d.Provider, &d.Status, &d.AttemptCount, &next, &d.ProviderMessageID,
			&d.TokenFingerprint, &d.ErrorCode, &d.ErrorDetail, &created, &updated, &sent, &failed, &openedAt,
			&r.Title, &r.Type, &r.Category, &r.Locale, &r.CampaignName, &r.DeviceModel, &r.Manufacturer,
			&r.AppVersion); err != nil {
			return nil, 0, err
		}
		d.NextAttemptAt, d.CreatedAt, d.UpdatedAt = timeFrom(next), timeFrom(created), timeFrom(updated)
		d.SentAt, d.FailedAt, d.OpenedAt = timePtr(sent), timePtr(failed), timePtr(openedAt)
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// ---------------------------------------------------------------- preferences

// NotificationPreferences — өшірілген/қосылған санаттар (жол жоқ — қосулы).
func (s *Store) NotificationPreferences(ctx context.Context, userID string) (map[string]bool, error) {
	out := map[string]bool{}
	rows, err := s.db.Reader().QueryContext(ctx,
		`SELECT category, enabled FROM notification_preferences WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var category string
		var enabled int
		if err := rows.Scan(&category, &enabled); err != nil {
			return nil, err
		}
		out[category] = enabled == 1
	}
	return out, rows.Err()
}

// SetNotificationPreferences — берілген санаттарды жазады.
func (s *Store) SetNotificationPreferences(ctx context.Context, userID string, prefs map[string]bool, now time.Time) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		for category, enabled := range prefs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO notification_preferences (user_id, category, enabled, updated_at) VALUES (?,?,?,?)
				ON CONFLICT (user_id, category) DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at`,
				userID, category, boolInt(enabled), ms(now)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---------------------------------------------------------------- retention

// DeleteFinishedDeliveries — cutoff-тан ескі, аяқталған жеткізулер. Кезектегілер ешқашан өшпейді.
//
// Small batches keep each write transaction short, so a large cleanup never
// holds the single SQLite writer long enough to delay a sign-in or a reply.
func (s *Store) DeleteFinishedDeliveries(ctx context.Context, cutoff time.Time, batch int) (int64, error) {
	return s.deleteInBatches(ctx, `DELETE FROM notification_deliveries WHERE rowid IN (
		SELECT rowid FROM notification_deliveries
		WHERE created_at < ? AND status NOT IN ('queued','sending','retrying') LIMIT ?)`, cutoff, batch)
}

// DeleteOrphanNotifications — жеткізуі қалмаған ескі автоматты хабарламалар.
// Науқан хабарламалары науқанмен бірге қалады (қорытынды есеп науқанда).
func (s *Store) DeleteOrphanNotifications(ctx context.Context, cutoff time.Time, batch int) (int64, error) {
	return s.deleteInBatches(ctx, `DELETE FROM notifications WHERE rowid IN (
		SELECT rowid FROM notifications
		WHERE created_at < ? AND campaign_id IS NULL
		  AND NOT EXISTS (SELECT 1 FROM notification_deliveries d WHERE d.notification_id = notifications.id)
		LIMIT ?)`, cutoff, batch)
}

func (s *Store) deleteInBatches(ctx context.Context, query string, cutoff time.Time, batch int) (int64, error) {
	var total int64
	for ctx.Err() == nil {
		res, err := s.db.Writer().ExecContext(ctx, query, ms(cutoff), batch)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < int64(batch) {
			break
		}
	}
	return total, ctx.Err()
}
