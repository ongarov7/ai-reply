package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// OpenMark — қосымша «хабарлама ашылды» деп хабарлаған жеткізу.
type OpenMark struct {
	DeliveryID     string
	InstallationID string
	At             time.Time
}

// TelemetryBatch — бір транзакцияда жазылатын оқиғалар, сессиялар, ашылулар, қателер.
type TelemetryBatch struct {
	Events    []domain.AppEvent
	Sessions  []domain.AppSession
	Opens     []OpenMark
	APIErrors []domain.APIError
}

// WriteTelemetry — топтап жазу (бір транзакция — бір fsync).
//
// Events carrying a client id already stored for the same installation are
// ignored, so a batch the app retries after a timeout is not counted twice.
// A session id is only ever extended by the installation that created it.
func (s *Store) WriteTelemetry(ctx context.Context, b TelemetryBatch) (int, error) {
	inserted := 0
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		for _, e := range b.Events {
			props := "{}"
			if len(e.Properties) > 0 {
				raw, err := json.Marshal(e.Properties)
				if err != nil {
					return err
				}
				props = string(raw)
			}
			res, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO app_events (id, client_event_id, event_name, user_id, installation_id,
					session_id, platform, app_version, app_build, os_version, device_model, outcome, error_code,
					request_id, properties, occurred_at, received_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				traits.NewID(), nullText(e.ClientEventID), e.Name, nullText(e.UserID), e.InstallationID,
				e.SessionID, e.Platform, e.AppVersion, e.AppBuild, e.OSVersion, e.DeviceModel, e.Outcome,
				e.ErrorCode, e.RequestID, props, ms(e.OccurredAt), ms(e.ReceivedAt))
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			inserted += int(n)
		}
		for _, sess := range b.Sessions {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO app_sessions (id, session_id, installation_id, user_id, platform, app_version, app_build,
					os_version, device_model, started_at, last_activity_at, ended_at, event_count)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT (session_id) DO UPDATE SET
					started_at = MIN(app_sessions.started_at, excluded.started_at),
					last_activity_at = MAX(app_sessions.last_activity_at, excluded.last_activity_at),
					ended_at = COALESCE(excluded.ended_at, app_sessions.ended_at),
					event_count = app_sessions.event_count + excluded.event_count,
					user_id = COALESCE(excluded.user_id, app_sessions.user_id),
					app_version = excluded.app_version, app_build = excluded.app_build,
					os_version = excluded.os_version
				WHERE app_sessions.installation_id = excluded.installation_id`,
				traits.NewID(), sess.SessionID, sess.InstallationID, nullText(sess.UserID), sess.Platform,
				sess.AppVersion, sess.AppBuild, sess.OSVersion, sess.DeviceModel, ms(sess.StartedAt),
				ms(sess.LastActivityAt), msPtr(sess.EndedAt), sess.EventCount); err != nil {
				return err
			}
		}
		for _, o := range b.Opens {
			if _, err := tx.ExecContext(ctx, `
				UPDATE notification_deliveries SET opened_at = COALESCE(opened_at, ?), updated_at = ?
				WHERE id = ? AND installation_id = (SELECT id FROM app_installations WHERE installation_id = ?)`,
				ms(o.At), ms(o.At), o.DeliveryID, o.InstallationID); err != nil {
				return err
			}
		}
		for _, e := range b.APIErrors {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO api_errors (id, request_id, trace_id, user_id, installation_id, session_id, platform,
					app_version, app_build, os_version, method, route, status_code, error_code, duration_ms, occurred_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				traits.NewID(), e.RequestID, e.TraceID, nullText(e.UserID), e.InstallationID, e.SessionID,
				e.Platform, e.AppVersion, e.AppBuild, e.OSVersion, e.Method, e.Route, e.StatusCode,
				e.ErrorCode, e.DurationMS, ms(e.OccurredAt)); err != nil {
				return err
			}
		}
		return nil
	})
	return inserted, err
}

// InsertAuthEvent — кіру оқиғасы (синхронды: қауіпсіздік журналы кезекте жоғалмайды).
func (s *Store) InsertAuthEvent(ctx context.Context, e domain.AuthEvent) error {
	if e.ID == "" {
		e.ID = traits.NewID()
	}
	_, err := s.db.Writer().ExecContext(ctx, `
		INSERT INTO auth_events (id, event_name, method, outcome, error_code, user_id, subject_hash,
			installation_id, platform, app_version, app_build, os_version, ip, request_id, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Name, e.Method, e.Outcome, e.ErrorCode, nullText(e.UserID), e.SubjectHash, e.InstallationID,
		e.Platform, e.AppVersion, e.AppBuild, e.OSVersion, e.IP, e.RequestID, ms(e.CreatedAt))
	return err
}

// ---------------------------------------------------------------- listing

// TimeRange — [From, To) аралығы; нөлдік мән шектемейді.
type TimeRange struct {
	From time.Time
	To   time.Time
}

func (r TimeRange) apply(column string, where []string, args []any) ([]string, []any) {
	if !r.From.IsZero() {
		where = append(where, column+" >= ?")
		args = append(args, ms(r.From))
	}
	if !r.To.IsZero() {
		where = append(where, column+" < ?")
		args = append(args, ms(r.To))
	}
	return where, args
}

// inUsers — қолданушы сүзгісі (бірнеше идентификатор; бос тізім — ешкім).
func inUsers(column string, ids []string, where []string, args []any) ([]string, []any) {
	if ids == nil {
		return where, args
	}
	if len(ids) == 0 {
		return append(where, "0 = 1"), args
	}
	where = append(where, column+" IN ("+placeholders(len(ids))+")")
	for _, id := range ids {
		args = append(args, id)
	}
	return where, args
}

// EventFilter — қосымша оқиғаларының сүзгісі.
type EventFilter struct {
	UserIDs        []string // nil: any; empty: nobody matched the user query
	InstallationID string   // prefix of the app-generated id
	Name           string
	Platform       string
	AppVersion     string
	AppBuild       string
	OSVersion      string
	DeviceModel    string
	Outcome        string
	Range          TimeRange
	Page           traits.Page
}

// ListAppEvents — оқиғалар, соңғылары алдымен.
func (s *Store) ListAppEvents(ctx context.Context, f EventFilter) ([]domain.AppEvent, int, error) {
	where, args := []string{"1=1"}, []any{}
	where, args = inUsers("user_id", f.UserIDs, where, args)
	for column, value := range map[string]string{
		"event_name": f.Name, "platform": f.Platform,
		"app_version": f.AppVersion, "app_build": f.AppBuild, "os_version": f.OSVersion, "outcome": f.Outcome,
	} {
		if value != "" {
			where = append(where, column+" = ?")
			args = append(args, value)
		}
	}
	if f.InstallationID != "" {
		// A prefix: the admin panel only ever shows the first characters.
		// The handler admits [A-Za-z0-9-] only, so no GLOB metacharacters.
		where = append(where, "installation_id GLOB ?")
		args = append(args, f.InstallationID+"*")
	}
	if f.DeviceModel != "" {
		where = append(where, "device_model LIKE ?")
		args = append(args, f.DeviceModel+"%")
	}
	where, args = f.Range.apply("occurred_at", where, args)
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM app_events WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, COALESCE(client_event_id, ''), event_name, COALESCE(user_id, ''), installation_id, session_id,
		       platform, app_version, app_build, os_version, device_model, outcome, error_code, request_id,
		       properties, occurred_at, received_at
		FROM app_events WHERE `+clause+` ORDER BY occurred_at DESC, id LIMIT ? OFFSET ?`,
		append(args, f.Page.Limit, f.Page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.AppEvent
	for rows.Next() {
		var (
			e                  domain.AppEvent
			props              string
			occurred, received int64
		)
		if err := rows.Scan(&e.ID, &e.ClientEventID, &e.Name, &e.UserID, &e.InstallationID, &e.SessionID,
			&e.Platform, &e.AppVersion, &e.AppBuild, &e.OSVersion, &e.DeviceModel, &e.Outcome, &e.ErrorCode,
			&e.RequestID, &props, &occurred, &received); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal([]byte(props), &e.Properties)
		e.OccurredAt, e.ReceivedAt = timeFrom(occurred), timeFrom(received)
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// AuthEventFilter — кіру оқиғаларының сүзгісі.
type AuthEventFilter struct {
	UserIDs     []string
	SubjectHash string
	Name        string
	Method      string
	Outcome     string
	Range       TimeRange
	Page        traits.Page
}

// ListAuthEvents — кіру оқиғалары.
func (s *Store) ListAuthEvents(ctx context.Context, f AuthEventFilter) ([]domain.AuthEvent, int, error) {
	where, args := []string{"1=1"}, []any{}
	switch {
	case f.UserIDs != nil && f.SubjectHash != "":
		// An e-mail finds the account's events and failed attempts made with
		// that address before (or without) an account.
		if len(f.UserIDs) == 0 {
			where = append(where, "subject_hash = ?")
			args = append(args, f.SubjectHash)
		} else {
			where = append(where, "(user_id IN ("+placeholders(len(f.UserIDs))+") OR subject_hash = ?)")
			for _, id := range f.UserIDs {
				args = append(args, id)
			}
			args = append(args, f.SubjectHash)
		}
	case f.SubjectHash != "":
		where = append(where, "subject_hash = ?")
		args = append(args, f.SubjectHash)
	default:
		where, args = inUsers("user_id", f.UserIDs, where, args)
	}
	for column, value := range map[string]string{"event_name": f.Name, "method": f.Method, "outcome": f.Outcome} {
		if value != "" {
			where = append(where, column+" = ?")
			args = append(args, value)
		}
	}
	where, args = f.Range.apply("created_at", where, args)
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_events WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, event_name, method, outcome, error_code, COALESCE(user_id, ''), subject_hash, installation_id,
		       platform, app_version, app_build, os_version, ip, request_id, created_at
		FROM auth_events WHERE `+clause+` ORDER BY created_at DESC, id LIMIT ? OFFSET ?`,
		append(args, f.Page.Limit, f.Page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.AuthEvent
	for rows.Next() {
		var e domain.AuthEvent
		var created int64
		if err := rows.Scan(&e.ID, &e.Name, &e.Method, &e.Outcome, &e.ErrorCode, &e.UserID, &e.SubjectHash,
			&e.InstallationID, &e.Platform, &e.AppVersion, &e.AppBuild, &e.OSVersion, &e.IP, &e.RequestID,
			&created); err != nil {
			return nil, 0, err
		}
		e.CreatedAt = timeFrom(created)
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// APIErrorFilter — API қателерінің сүзгісі.
type APIErrorFilter struct {
	UserIDs        []string
	InstallationID string
	Platform       string
	AppVersion     string
	AppBuild       string
	Route          string
	StatusCode     int
	RequestID      string
	Range          TimeRange
	Page           traits.Page
}

// ListAPIErrors — API қателері.
func (s *Store) ListAPIErrors(ctx context.Context, f APIErrorFilter) ([]domain.APIError, int, error) {
	where, args := []string{"1=1"}, []any{}
	where, args = inUsers("user_id", f.UserIDs, where, args)
	for column, value := range map[string]string{
		"installation_id": f.InstallationID, "platform": f.Platform, "app_version": f.AppVersion,
		"app_build": f.AppBuild, "route": f.Route, "request_id": f.RequestID,
	} {
		if value != "" {
			where = append(where, column+" = ?")
			args = append(args, value)
		}
	}
	if f.StatusCode > 0 {
		where = append(where, "status_code = ?")
		args = append(args, f.StatusCode)
	}
	where, args = f.Range.apply("occurred_at", where, args)
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM api_errors WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, request_id, trace_id, COALESCE(user_id, ''), installation_id, session_id, platform, app_version,
		       app_build, os_version, method, route, status_code, error_code, duration_ms, occurred_at
		FROM api_errors WHERE `+clause+` ORDER BY occurred_at DESC, id LIMIT ? OFFSET ?`,
		append(args, f.Page.Limit, f.Page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.APIError
	for rows.Next() {
		var e domain.APIError
		var occurred int64
		if err := rows.Scan(&e.ID, &e.RequestID, &e.TraceID, &e.UserID, &e.InstallationID, &e.SessionID,
			&e.Platform, &e.AppVersion, &e.AppBuild, &e.OSVersion, &e.Method, &e.Route, &e.StatusCode,
			&e.ErrorCode, &e.DurationMS, &occurred); err != nil {
			return nil, 0, err
		}
		e.OccurredAt = timeFrom(occurred)
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// UserAppSessions — қолданушының соңғы сессиялары.
func (s *Store) UserAppSessions(ctx context.Context, userID string, limit int) ([]domain.AppSession, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT id, session_id, installation_id, COALESCE(user_id, ''), platform, app_version, app_build,
		       os_version, device_model, started_at, last_activity_at, ended_at, event_count
		FROM app_sessions WHERE user_id = ? ORDER BY started_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AppSession
	for rows.Next() {
		var (
			sess            domain.AppSession
			started, active int64
			ended           sql.NullInt64
		)
		if err := rows.Scan(&sess.ID, &sess.SessionID, &sess.InstallationID, &sess.UserID, &sess.Platform,
			&sess.AppVersion, &sess.AppBuild, &sess.OSVersion, &sess.DeviceModel, &started, &active, &ended,
			&sess.EventCount); err != nil {
			return nil, err
		}
		sess.StartedAt, sess.LastActivityAt, sess.EndedAt = timeFrom(started), timeFrom(active), timePtr(ended)
		out = append(out, sess)
	}
	return out, rows.Err()
}

// UserIDsMatching — әкімші іздеуі: пошта, телефон не идентификатордың басы.
func (s *Store) UserIDsMatching(ctx context.Context, email, phone, idPrefix string) ([]string, error) {
	switch {
	case email != "":
		return s.ids(ctx, `SELECT id FROM users WHERE lower(email) = lower(?) AND deleted_at IS NULL LIMIT 20`, email)
	case phone != "":
		return s.ids(ctx, `SELECT id FROM users WHERE phone = ? AND deleted_at IS NULL LIMIT 20`, phone)
	case idPrefix != "":
		return s.ids(ctx, `SELECT id FROM users WHERE id LIKE ? AND deleted_at IS NULL LIMIT 20`, idPrefix+"%")
	default:
		return nil, nil
	}
}

// ---------------------------------------------------------------- reports

// VersionRow — қосымша нұсқасының нақты қолданылуы.
type VersionRow struct {
	Platform          string    `json:"platform"`
	AppVersion        string    `json:"app_version"`
	AppBuild          string    `json:"app_build"`
	Installations     int       `json:"installations"`
	Active30d         int       `json:"active_30d"`
	PushReachable     int       `json:"push_reachable"`
	APIErrors7d       int       `json:"api_errors_7d"`
	PushRegFailures7d int       `json:"push_registration_failures_7d"`
	LastSeenAt        time.Time `json:"-"`
}

// VersionReport — қай нұсқа шынымен қолданыста және қайсысы қате береді.
func (s *Store) VersionReport(ctx context.Context, now time.Time) ([]VersionRow, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT platform, app_version, app_build, COUNT(*),
		       COALESCE(SUM(CASE WHEN last_seen_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN push_status = 'active' AND notifications_enabled = 1 THEN 1 ELSE 0 END), 0),
		       MAX(last_seen_at)
		FROM app_installations GROUP BY platform, app_version, app_build
		ORDER BY MAX(last_seen_at) DESC LIMIT 200`, ms(now.AddDate(0, 0, -30)))
	if err != nil {
		return nil, err
	}
	type key struct{ platform, version, build string }
	index := map[key]int{}
	var out []VersionRow
	for rows.Next() {
		var r VersionRow
		var last int64
		if err := rows.Scan(&r.Platform, &r.AppVersion, &r.AppBuild, &r.Installations, &r.Active30d,
			&r.PushReachable, &last); err != nil {
			rows.Close()
			return nil, err
		}
		r.LastSeenAt = timeFrom(last)
		index[key{r.Platform, r.AppVersion, r.AppBuild}] = len(out)
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	since := ms(now.AddDate(0, 0, -7))
	merge := func(query string, set func(*VersionRow, int)) error {
		rows, err := s.db.Reader().QueryContext(ctx, query, since)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k key
			var n int
			if err := rows.Scan(&k.platform, &k.version, &k.build, &n); err != nil {
				return err
			}
			i, ok := index[k]
			if !ok {
				index[k] = len(out)
				out = append(out, VersionRow{Platform: k.platform, AppVersion: k.version, AppBuild: k.build})
				i = len(out) - 1
			}
			set(&out[i], n)
		}
		return rows.Err()
	}
	if err := merge(`SELECT platform, app_version, app_build, COUNT(*) FROM api_errors
		WHERE occurred_at >= ? AND platform <> '' GROUP BY 1, 2, 3`, func(r *VersionRow, n int) { r.APIErrors7d = n }); err != nil {
		return nil, err
	}
	if err := merge(`SELECT platform, app_version, app_build, COUNT(*) FROM app_events
		WHERE occurred_at >= ? AND event_name = 'push_token_registration_failed' GROUP BY 1, 2, 3`,
		func(r *VersionRow, n int) { r.PushRegFailures7d = n }); err != nil {
		return nil, err
	}
	return out, nil
}

// OSVersionReport — соңғы 30 күнде көрінген орнатулардың ОЖ нұсқалары.
func (s *Store) OSVersionReport(ctx context.Context, now time.Time) ([]Point, error) {
	return collectPoints(s.db.Reader().QueryContext(ctx, `
		SELECT CASE WHEN os_name <> '' THEN os_name ELSE platform END || ' ' ||
		       CASE WHEN instr(os_version, '.') > 0 THEN substr(os_version, 1, instr(os_version, '.') - 1) ELSE os_version END,
		       COUNT(*)
		FROM app_installations WHERE last_seen_at >= ? GROUP BY 1 ORDER BY 2 DESC LIMIT 12`,
		ms(now.AddDate(0, 0, -30))))
}

// AppVersionMix — соңғы 30 күндегі белсенді нұсқалар.
func (s *Store) AppVersionMix(ctx context.Context, now time.Time) ([]Point, error) {
	return collectPoints(s.db.Reader().QueryContext(ctx, `
		SELECT platform || ' ' || app_version, COUNT(*) FROM app_installations
		WHERE last_seen_at >= ? AND app_version <> '' GROUP BY 1 ORDER BY 2 DESC LIMIT 12`,
		ms(now.AddDate(0, 0, -30))))
}

// OpsSummary — операциялық көрсеткіштер (әкімші тақтасы).
type OpsSummary struct {
	Installations    int                  `json:"installations"`
	Active30d        int                  `json:"active_30d"`
	Android          int                  `json:"android"`
	IOS              int                  `json:"ios"`
	Anonymous        int                  `json:"anonymous"`
	PushReachable    int                  `json:"push_reachable"`
	FCMActive        int                  `json:"fcm_active"`
	APNsActive       int                  `json:"apns_active"`
	InvalidTokens    int                  `json:"invalid_tokens"`
	PermissionDenied int                  `json:"permission_denied"`
	Deliveries7d     domain.DeliveryStats `json:"deliveries_7d"`
	LoginSuccess7d   int                  `json:"login_success_7d"`
	LoginFailure7d   int                  `json:"login_failure_7d"`
	APIErrors24h     int                  `json:"api_errors_24h"`
	ServerErrors24h  int                  `json:"server_errors_24h"`
}

// Ops — бір өтуде барлық операциялық көрсеткіштер.
func (s *Store) Ops(ctx context.Context, now time.Time) (OpsSummary, error) {
	var o OpsSummary
	r := s.db.Reader()
	if err := r.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN last_seen_at >= ? THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN platform = 'android' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN platform = 'ios' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN user_id IS NULL THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN push_status = 'active' AND notifications_enabled = 1
		                          AND push_permission IN ('authorized','provisional','ephemeral','unknown') THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN push_status = 'active' AND push_provider = 'fcm' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN push_status = 'active' AND push_provider = 'apns' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN push_status = 'invalid' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN push_permission = 'denied' THEN 1 ELSE 0 END), 0)
		FROM app_installations`, ms(now.AddDate(0, 0, -30))).
		Scan(&o.Installations, &o.Active30d, &o.Android, &o.IOS, &o.Anonymous, &o.PushReachable,
			&o.FCMActive, &o.APNsActive, &o.InvalidTokens, &o.PermissionDenied); err != nil {
		return o, err
	}
	var err error
	if o.Deliveries7d, err = s.DeliveryTotals(ctx, now.AddDate(0, 0, -7)); err != nil {
		return o, err
	}
	if err := r.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN event_name IN ('auth_login_success','auth_signup_success') THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN event_name = 'auth_login_failed' THEN 1 ELSE 0 END), 0)
		FROM auth_events WHERE created_at >= ?`, ms(now.AddDate(0, 0, -7))).
		Scan(&o.LoginSuccess7d, &o.LoginFailure7d); err != nil {
		return o, err
	}
	err = r.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END), 0)
		FROM api_errors WHERE occurred_at >= ?`, ms(now.Add(-24*time.Hour))).Scan(&o.APIErrors24h, &o.ServerErrors24h)
	return o, err
}

// ---------------------------------------------------------------- retention

// retentionTargets — қай кестеден, қай уақыт бағаны бойынша, қандай қосымша шартпен өшіріледі.
var retentionTargets = map[string]struct{ column, extra string }{
	"app_events":              {"occurred_at", ""},
	"app_sessions":            {"last_activity_at", ""},
	"api_errors":              {"occurred_at", ""},
	"auth_events":             {"created_at", ""},
	"admin_audit_logs":        {"created_at", ""},
	"notification_deliveries": {"created_at", "status NOT IN ('queued','sending','retrying')"},
	"notifications": {"created_at", "campaign_id IS NULL AND NOT EXISTS " +
		"(SELECT 1 FROM notification_deliveries d WHERE d.notification_id = notifications.id)"},
}

// DeleteOlderThan — сақтау мерзімі өткен жолдарды шағын топтармен өшіреді.
//
// Small batches keep each write transaction short, so a large cleanup never
// holds the single SQLite writer long enough to delay a sign-in or a reply.
func (s *Store) DeleteOlderThan(ctx context.Context, table string, cutoff time.Time, batch int) (int64, error) {
	target, ok := retentionTargets[table]
	if !ok {
		return 0, fmt.Errorf("retention: unknown table %q", table)
	}
	where := target.column + " < ?"
	if target.extra != "" {
		where += " AND " + target.extra
	}
	var total int64
	for ctx.Err() == nil {
		res, err := s.db.Writer().ExecContext(ctx, `DELETE FROM `+table+` WHERE rowid IN
			(SELECT rowid FROM `+table+` WHERE `+where+` LIMIT ?)`, ms(cutoff), batch)
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

// RetentionTables — тесттер мен құжатқа арналған тізім.
func RetentionTables() []string {
	out := make([]string, 0, len(retentionTargets))
	for k := range retentionTargets {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
