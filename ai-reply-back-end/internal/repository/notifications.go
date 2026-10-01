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
// INSERT OR IGNORE + UNIQUE (notification_id, installation_id): running the
// same fan-out twice (a retried request, a restarted worker, a second
// backend process) cannot queue a second copy for the same device.
const deliveryInsert = `
	INSERT OR IGNORE INTO notification_deliveries
		(id, notification_id, campaign_id, installation_id, user_id, platform, provider, status,
		 attempt_count, next_attempt_at, created_at, updated_at)
	SELECT lower(hex(randomblob(16))), ?, ?, i.id, i.user_id, i.platform, i.push_provider, 'queued',
	       0, ?, ?, ?` + audienceFrom

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

// CreateUserNotification — бір қолданушыға бір логикалық хабарлама және оның
// құрылғыларына жеткізулер, бір транзакцияда.
//
// created=false means the dedupe key existed: the same event was already
// turned into a notification, and nothing new is queued.
func (s *Store) CreateUserNotification(ctx context.Context, n domain.Notification, q AudienceQuery) (id string, created bool, devices int, err error) {
	now := ms(q.Now)
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		candidate := traits.NewID()
		res, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO notifications (id, dedupe_key, idempotency_key, campaign_id, user_id,
				category, type, title, body, link, data, created_at)
			VALUES (?,?,?,NULL,?,?,?,?,?,?,?,?)`,
			candidate, n.DedupeKey, n.IdempotencyKey, n.UserID, n.Category, n.Type, n.Title, n.Body,
			n.Link, encodeData(n.Data), now)
		if err != nil {
			return err
		}
		if inserted, _ := res.RowsAffected(); inserted == 0 {
			return tx.QueryRowContext(ctx, `SELECT id FROM notifications WHERE dedupe_key = ?`, n.DedupeKey).Scan(&id)
		}
		id, created = candidate, true
		where, args := eligible(q)
		res, err = tx.ExecContext(ctx, deliveryInsert+where, append([]any{id, nil, now, now, now}, args...)...)
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		devices = int(affected)
		return nil
	})
	return id, created, devices, err
}

// ---------------------------------------------------------------- campaigns

const campaignColumns = `id, name, title, body, category, link, data, audience_filter, status, created_by,
	COALESCE((SELECT a.email FROM admin_users a WHERE a.id = created_by), ''),
	COALESCE(idempotency_key, ''), recipient_count, device_count, created_at, updated_at, queued_at,
	started_at, completed_at, cancelled_at, final_stats`

func scanCampaign(row interface{ Scan(...any) error }) (domain.Campaign, error) {
	var (
		c                                       domain.Campaign
		data, audience, final                   string
		created, updated                        int64
		queued, started, completed, cancelledAt sql.NullInt64
	)
	err := row.Scan(&c.ID, &c.Name, &c.Title, &c.Body, &c.Category, &c.Link, &data, &audience, &c.Status,
		&c.CreatedBy, &c.CreatedByEmail, &c.IdempotencyKey, &c.RecipientCount, &c.DeviceCount, &created, &updated,
		&queued, &started, &completed, &cancelledAt, &final)
	if err != nil {
		return domain.Campaign{}, err
	}
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
	summary := "all"
	if string(audience) != "{}" {
		summary = "filtered"
	}
	now := ms(c.CreatedAt)
	_, err = s.db.Writer().ExecContext(ctx, `
		INSERT INTO notification_campaigns (id, title, body, audience, status, created_by, created_at, updated_at,
			name, category, link, data, audience_filter, idempotency_key)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Title, c.Body, summary, c.Status, c.CreatedBy, now, now,
		c.Name, c.Category, c.Link, encodeData(c.Data), string(audience), nullText(c.IdempotencyKey))
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
		WHERE `+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, append(args, page.Limit, page.Offset)...)
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

// FanOutCampaign — queued → processing және алушыларды кезекке қою, бір транзакцияда.
//
// The status change is the claim: of two workers that see the same queued
// campaign, one UPDATE changes a row and fans out, the other changes nothing
// and returns claimed=false. A crash in the middle rolls everything back and
// the campaign stays queued for the next attempt.
func (s *Store) FanOutCampaign(ctx context.Context, id string, q AudienceQuery) (claimed bool, devices, users int, err error) {
	now := ms(q.Now)
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE notification_campaigns SET status = 'processing', started_at = ?, updated_at = ?
			WHERE id = ? AND status = 'queued'`, now, now, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		claimed = true

		campaign, err := scanCampaign(tx.QueryRowContext(ctx,
			`SELECT `+campaignColumns+` FROM notification_campaigns WHERE id = ?`, id))
		if err != nil {
			return err
		}
		dedupe := "campaign:" + id
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO notifications (id, dedupe_key, idempotency_key, campaign_id, user_id,
				category, type, title, body, link, data, created_at)
			VALUES (?,?,?,?,NULL,?,'campaign',?,?,?,?,?)`,
			traits.NewID(), dedupe, dedupe, id, campaign.Category, campaign.Title, campaign.Body,
			campaign.Link, encodeData(campaign.Data), now); err != nil {
			return err
		}
		var notificationID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM notifications WHERE dedupe_key = ?`, dedupe).
			Scan(&notificationID); err != nil {
			return err
		}
		q.Filter, q.Category = campaign.Audience, campaign.Category
		where, args := eligible(q)
		if _, err := tx.ExecContext(ctx, deliveryInsert+where,
			append([]any{notificationID, id, now, now, now}, args...)...); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*), COUNT(DISTINCT user_id) FROM notification_deliveries WHERE campaign_id = ?`, id).
			Scan(&devices, &users); err != nil {
			return err
		}
		if devices == 0 {
			_, err = tx.ExecContext(ctx, `
				UPDATE notification_campaigns SET status = 'completed', completed_at = ?, updated_at = ?,
					recipient_count = 0, device_count = 0, final_stats = '{"total":0}'
				WHERE id = ?`, now, now, id)
			return err
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE notification_campaigns SET recipient_count = ?, device_count = ?, updated_at = ? WHERE id = ?`,
			users, devices, now, id)
		return err
	})
	return claimed, devices, users, err
}

// CampaignStats — науқан жеткізулерінің күйі (live, жолдардан есептеледі).
func (s *Store) CampaignStats(ctx context.Context, campaignID string) (domain.DeliveryStats, error) {
	return s.deliveryStats(ctx, `notification_deliveries`, `campaign_id = ?`, campaignID)
}

// deliveryStats — counts by status and platform. from names the table (with an
// index hint where the planner, lacking statistics, would scan the table).
func (s *Store) deliveryStats(ctx context.Context, from, where string, args ...any) (domain.DeliveryStats, error) {
	var stats domain.DeliveryStats
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT status, platform, COUNT(*), COALESCE(SUM(CASE WHEN opened_at IS NOT NULL THEN 1 ELSE 0 END), 0)
		FROM `+from+` WHERE `+where+` GROUP BY status, platform`, args...)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var status, platform string
		var count, opened int
		if err := rows.Scan(&status, &platform, &count, &opened); err != nil {
			return stats, err
		}
		stats.Add(status, count)
		stats.Opened += opened
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
		GROUP BY 1 ORDER BY 2 DESC LIMIT 12`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ErrorCount
	for rows.Next() {
		var e ErrorCount
		if err := rows.Scan(&e.Code, &e.Count); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
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
	InstallationID string
	UserID         string
	Platform       string
	Provider       string
	AttemptCount   int
}

// ClaimDeliveries — мерзімі жеткен жолдарды lease-пен алады (бір атомарлы UPDATE ... RETURNING).
//
// SQLite runs one write transaction at a time for the whole database file,
// across every process that opens it, so two workers can never claim the
// same row. A row whose lease ran out (its worker died mid-send) becomes
// claimable again; that retry counts as an attempt.
func (s *Store) ClaimDeliveries(ctx context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]ClaimedDelivery, error) {
	var out []ClaimedDelivery
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			UPDATE notification_deliveries
			SET status = 'sending', lease_owner = ?, lease_until = ?, attempt_count = attempt_count + 1, updated_at = ?
			WHERE id IN (
				SELECT id FROM notification_deliveries
				WHERE (status IN ('queued','retrying') AND next_attempt_at <= ?)
				   OR (status = 'sending' AND lease_until < ?)
				ORDER BY next_attempt_at LIMIT ?)
			RETURNING id, notification_id, COALESCE(campaign_id, ''), installation_id, COALESCE(user_id, ''),
			          platform, provider, attempt_count`,
			owner, ms(now.Add(lease)), ms(now), ms(now), ms(now), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d ClaimedDelivery
			if err := rows.Scan(&d.ID, &d.NotificationID, &d.CampaignID, &d.InstallationID, &d.UserID,
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
	ErrorCode         string
	ErrorDetail       string
	Now               time.Time
}

// FinishDelivery — нәтижені жазады, тек lease әлі осы жұмысшыда болса.
func (s *Store) FinishDelivery(ctx context.Context, id, owner string, o DeliveryOutcome) (bool, error) {
	now := ms(o.Now)
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE notification_deliveries SET
			status = ?, lease_owner = '', lease_until = NULL,
			next_attempt_at = CASE WHEN ? = 'retrying' THEN ? ELSE next_attempt_at END,
			provider_message_id = CASE WHEN ? <> '' THEN ? ELSE provider_message_id END,
			error_code = ?, error_detail = ?, updated_at = ?,
			sent_at = CASE WHEN ? = 'provider_accepted' THEN ? ELSE sent_at END,
			failed_at = CASE WHEN ? IN ('provider_failed','invalid_token') THEN ? ELSE failed_at END
		WHERE id = ? AND lease_owner = ? AND status = 'sending'`,
		o.Status, o.Status, ms(o.NextAttemptAt), o.ProviderMessageID, o.ProviderMessageID,
		o.ErrorCode, o.ErrorDetail, now, o.Status, now, o.Status, now, id, owner)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// NotificationByID — хабарлама мазмұны (жұмысшыға).
func (s *Store) NotificationByID(ctx context.Context, id string) (domain.Notification, error) {
	var (
		n                  domain.Notification
		campaignID, userID sql.NullString
		data               string
		created            int64
	)
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT id, dedupe_key, idempotency_key, campaign_id, user_id, category, type, title, body, link, data, created_at
		FROM notifications WHERE id = ?`, id).
		Scan(&n.ID, &n.DedupeKey, &n.IdempotencyKey, &campaignID, &userID, &n.Category, &n.Type,
			&n.Title, &n.Body, &n.Link, &data, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Notification{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Notification{}, err
	}
	n.CampaignID, n.UserID = text(campaignID), text(userID)
	n.Data = decodeData(data)
	n.CreatedAt = timeFrom(created)
	return n, nil
}

// MarkDeliveryOpened — қосымша «ашылды» деп хабарлаған жеткізу (тек сол орнатудан).
func (s *Store) MarkDeliveryOpened(ctx context.Context, deliveryID, installationID string, at time.Time) (bool, error) {
	res, err := s.db.Writer().ExecContext(ctx, `
		UPDATE notification_deliveries SET opened_at = COALESCE(opened_at, ?), updated_at = ?
		WHERE id = ? AND installation_id = (SELECT id FROM app_installations WHERE installation_id = ?)`,
		ms(at), ms(at), deliveryID, installationID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeliveryFilter — әкімші тізімі.
type DeliveryFilter struct {
	CampaignID     string
	UserID         string
	InstallationID string
	Status         string
	Platform       string
	Page           traits.Page
}

// DeliveryRow — жеткізу мен оның хабарламасы, құрылғысы (токенсіз).
type DeliveryRow struct {
	Delivery     domain.Delivery
	Title        string
	Type         string
	Category     string
	CampaignName string
	DeviceModel  string
	AppVersion   string
	TokenHash    string
}

// ListDeliveries — сүзгі және беттеу.
func (s *Store) ListDeliveries(ctx context.Context, f DeliveryFilter) ([]DeliveryRow, int, error) {
	where := []string{"1=1"}
	args := []any{}
	for column, value := range map[string]string{
		"d.campaign_id": f.CampaignID, "d.user_id": f.UserID, "d.installation_id": f.InstallationID,
		"d.status": f.Status, "d.platform": f.Platform,
	} {
		if value != "" {
			where = append(where, column+" = ?")
			args = append(args, value)
		}
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_deliveries d WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT d.id, d.notification_id, COALESCE(d.campaign_id, ''), d.installation_id, COALESCE(d.user_id, ''),
		       d.platform, d.provider, d.status, d.attempt_count, d.next_attempt_at, d.provider_message_id,
		       d.error_code, d.error_detail, d.created_at, d.updated_at, d.sent_at, d.failed_at, d.opened_at,
		       n.title, n.type, n.category, COALESCE(c.name, ''),
		       COALESCE(i.device_model, ''), COALESCE(i.app_version, ''), COALESCE(i.push_token_hash, '')
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
		if err := rows.Scan(&d.ID, &d.NotificationID, &d.CampaignID, &d.InstallationID, &d.UserID,
			&d.Platform, &d.Provider, &d.Status, &d.AttemptCount, &next, &d.ProviderMessageID,
			&d.ErrorCode, &d.ErrorDetail, &created, &updated, &sent, &failed, &openedAt,
			&r.Title, &r.Type, &r.Category, &r.CampaignName, &r.DeviceModel, &r.AppVersion, &r.TokenHash); err != nil {
			return nil, 0, err
		}
		d.NextAttemptAt, d.CreatedAt, d.UpdatedAt = timeFrom(next), timeFrom(created), timeFrom(updated)
		d.SentAt, d.FailedAt, d.OpenedAt = timePtr(sent), timePtr(failed), timePtr(openedAt)
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// DeliveryTotals — кезең ішіндегі жеткізу есебі (әкімші тақтасына).
func (s *Store) DeliveryTotals(ctx context.Context, since time.Time) (domain.DeliveryStats, error) {
	return s.deliveryStats(ctx, `notification_deliveries INDEXED BY idx_deliveries_created_stats`,
		`created_at >= ?`, ms(since))
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
			v := 0
			if enabled {
				v = 1
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO notification_preferences (user_id, category, enabled, updated_at) VALUES (?,?,?,?)
				ON CONFLICT (user_id, category) DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at`,
				userID, category, v, ms(now)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---------------------------------------------------------------- subscription notices

// SubscriptionNotice — тариф мерзімі туралы хабарлама үшін дерек.
type SubscriptionNotice struct {
	SubscriptionID string
	UserID         string
	Locale         string
	PlanName       map[string]string
	ExpiresAt      time.Time
}

func (s *Store) subscriptionNotices(ctx context.Context, query string, args ...any) ([]SubscriptionNotice, error) {
	rows, err := s.db.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubscriptionNotice
	for rows.Next() {
		var (
			n              SubscriptionNotice
			expires        int64
			kk, ru, en, uz string
		)
		if err := rows.Scan(&n.SubscriptionID, &n.UserID, &n.Locale, &expires, &kk, &ru, &en, &uz); err != nil {
			return nil, err
		}
		n.ExpiresAt = timeFrom(expires)
		n.PlanName = map[string]string{"kk": kk, "ru": ru, "en": en, "uz": uz}
		out = append(out, n)
	}
	return out, rows.Err()
}

// SubscriptionsExpiringBetween — ақылы, белсенді, мерзімі (from, to] аралығында бітетін жазылымдар.
func (s *Store) SubscriptionsExpiringBetween(ctx context.Context, from, to time.Time) ([]SubscriptionNotice, error) {
	return s.subscriptionNotices(ctx, `
		SELECT s.id, s.user_id, u.locale, s.expires_at, p.name_kk, p.name_ru, p.name_en, p.name_uz
		FROM subscriptions s JOIN plans p ON p.id = s.plan_id JOIN users u ON u.id = s.user_id
		WHERE p.is_free = 0 AND s.status IN ('active','trial') AND s.expires_at > ? AND s.expires_at <= ?
		  AND u.status = 'active' AND u.deleted_at IS NULL
		LIMIT 1000`, ms(from), ms(to))
}

// SubscriptionsExpiredBetween — мерзімі (from, to] аралығында біткен, орнына жаңа ақылы тариф жоқ.
func (s *Store) SubscriptionsExpiredBetween(ctx context.Context, from, to time.Time) ([]SubscriptionNotice, error) {
	return s.subscriptionNotices(ctx, `
		SELECT s.id, s.user_id, u.locale, s.expires_at, p.name_kk, p.name_ru, p.name_en, p.name_uz
		FROM subscriptions s JOIN plans p ON p.id = s.plan_id JOIN users u ON u.id = s.user_id
		WHERE p.is_free = 0 AND s.status IN ('active','trial','expired') AND s.expires_at > ? AND s.expires_at <= ?
		  AND u.status = 'active' AND u.deleted_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM subscriptions s2 JOIN plans p2 ON p2.id = s2.plan_id
		                  WHERE s2.user_id = s.user_id AND p2.is_free = 0 AND s2.id <> s.id
		                    AND s2.status IN ('active','trial') AND (s2.expires_at IS NULL OR s2.expires_at > ?))
		LIMIT 1000`, ms(from), ms(to), ms(to))
}
