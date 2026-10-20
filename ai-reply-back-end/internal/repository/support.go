package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

const supportConversationColumns = `c.id, c.user_id, c.status, c.assigned_admin_id, c.last_message_at,
	c.user_unread_count, c.operator_unread_count, c.resolved_at, c.created_at, c.updated_at`

type supportScanner interface{ Scan(dest ...any) error }

func scanSupportConversation(row supportScanner, extra ...any) (domain.SupportConversation, error) {
	var (
		c                     domain.SupportConversation
		assigned              sql.NullString
		last, created, update int64
		resolved              sql.NullInt64
	)
	dest := append([]any{&c.ID, &c.UserID, &c.Status, &assigned, &last, &c.UserUnread, &c.OperatorUnread,
		&resolved, &created, &update}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c, domain.ErrNotFound
		}
		return c, err
	}
	c.AssignedAdminID = assigned.String
	c.LastMessageAt, c.CreatedAt, c.UpdatedAt = timeFrom(last), timeFrom(created), timeFrom(update)
	c.ResolvedAt = timePtr(resolved)
	return c, nil
}

const supportMessageColumns = `m.id, m.conversation_id, m.sender, m.admin_id, m.client_message_id, m.body, m.created_at, m.read_at`

func scanSupportMessage(row supportScanner) (domain.SupportMessage, error) {
	var (
		m               domain.SupportMessage
		admin, clientID sql.NullString
		created         int64
		read            sql.NullInt64
	)
	if err := row.Scan(&m.ID, &m.ConversationID, &m.Sender, &admin, &clientID, &m.Body, &created, &read); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return m, domain.ErrNotFound
		}
		return m, err
	}
	m.AdminID, m.ClientMessageID = admin.String, clientID.String
	m.CreatedAt, m.ReadAt = timeFrom(created), timePtr(read)
	return m, nil
}

type supportQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// SupportConversationByUser — тіркелгінің сөйлесуі (жоқ болса ErrNotFound).
func (s *Store) SupportConversationByUser(ctx context.Context, userID string) (domain.SupportConversation, error) {
	return supportConversationWhere(ctx, s.db.Reader(), "c.user_id = ?", userID)
}

// SupportConversation — сөйлесу идентификаторы бойынша.
func (s *Store) SupportConversation(ctx context.Context, id string) (domain.SupportConversation, error) {
	return supportConversationWhere(ctx, s.db.Reader(), "c.id = ?", id)
}

func supportConversationWhere(ctx context.Context, q supportQuerier, where string, arg string) (domain.SupportConversation, error) {
	return scanSupportConversation(q.QueryRowContext(ctx,
		`SELECT `+supportConversationColumns+` FROM support_conversations c WHERE `+where, arg))
}

// SupportCursor — хабарламалар беті: Before не After (хабарлама id), бірі ғана.
type SupportCursor struct {
	Before string
	After  string
	Limit  int
}

// SupportMessages — сөйлесу хабарламалары уақыт бойынша өсу ретімен және тағы бар ма.
//
// Without a cursor the newest page is returned. Before gives the page just
// older than that message, After the page just newer; has_more says whether
// more exist beyond the page in the same direction. A cursor that is not a
// message of this conversation is refused with the field name.
func (s *Store) SupportMessages(ctx context.Context, conversationID string, cur SupportCursor) ([]domain.SupportMessage, bool, error) {
	field, id := "before", cur.Before
	if cur.After != "" {
		field, id = "after", cur.After
	}
	var cursorAt int64
	if id != "" {
		err := s.db.Reader().QueryRowContext(ctx,
			`SELECT created_at FROM support_messages WHERE id = ? AND conversation_id = ?`, id, conversationID).Scan(&cursorAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, domain.InvalidField(field, "unknown message")
		}
		if err != nil {
			return nil, false, err
		}
	}
	where, args, order := "m.conversation_id = ?", []any{conversationID}, "DESC"
	switch {
	case cur.After != "":
		where += " AND (m.created_at > ? OR (m.created_at = ? AND m.id > ?))"
		args, order = append(args, cursorAt, cursorAt, id), "ASC"
	case cur.Before != "":
		where += " AND (m.created_at < ? OR (m.created_at = ? AND m.id < ?))"
		args = append(args, cursorAt, cursorAt, id)
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT `+supportMessageColumns+` FROM support_messages m
		WHERE `+where+` ORDER BY m.created_at `+order+`, m.id `+order+` LIMIT ?`, append(args, cur.Limit+1)...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []domain.SupportMessage{}
	for rows.Next() {
		m, err := scanSupportMessage(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(out) > cur.Limit
	if more {
		out = out[:cur.Limit]
	}
	if order == "DESC" {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, more, nil
}

// insertSupportMessage — хабарламаны жазады. created_at сөйлесу ішінде қатаң өседі:
// a page cursor never meets two messages of the same millisecond.
func insertSupportMessage(ctx context.Context, tx *sql.Tx, c domain.SupportConversation, m domain.SupportMessage) (domain.SupportMessage, error) {
	if !m.CreatedAt.After(c.LastMessageAt) {
		m.CreatedAt = c.LastMessageAt.Add(time.Millisecond)
	}
	m.ID, m.ConversationID = traits.NewID(), c.ID
	_, err := tx.ExecContext(ctx, `INSERT INTO support_messages
		(id, conversation_id, sender, admin_id, client_message_id, body, created_at) VALUES (?,?,?,?,?,?,?)`,
		m.ID, m.ConversationID, m.Sender, nullText(m.AdminID), nullText(m.ClientMessageID), m.Body, ms(m.CreatedAt))
	return m, err
}

// AddSupportUserMessage — қолданушының хабарламасы. created=false: осы client_message_id
// бұрын жазылған, сол хабарлама қайтарылады және ештеңе өзгермейді.
//
// The first message opens the conversation; a message to a resolved
// conversation reopens it. Everything happens in one write transaction.
func (s *Store) AddSupportUserMessage(ctx context.Context, userID, clientMessageID, body string, now time.Time) (domain.SupportMessage, bool, error) {
	var (
		out     domain.SupportMessage
		created bool
	)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		c, err := supportConversationWhere(ctx, tx, "c.user_id = ?", userID)
		if errors.Is(err, domain.ErrNotFound) {
			c = domain.SupportConversation{ID: traits.NewID(), UserID: userID, Status: domain.SupportOpen, CreatedAt: now}
			if _, err := tx.ExecContext(ctx, `INSERT INTO support_conversations
				(id, user_id, status, last_message_at, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
				c.ID, userID, c.Status, ms(now), ms(now), ms(now)); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if clientMessageID != "" {
			existing, err := scanSupportMessage(tx.QueryRowContext(ctx, `SELECT `+supportMessageColumns+`
				FROM support_messages m WHERE m.conversation_id = ? AND m.client_message_id = ?`, c.ID, clientMessageID))
			if err == nil {
				out = existing
				return nil
			}
			if !errors.Is(err, domain.ErrNotFound) {
				return err
			}
		}
		out, err = insertSupportMessage(ctx, tx, c, domain.SupportMessage{
			Sender: domain.SupportSenderUser, ClientMessageID: clientMessageID, Body: body, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		created = true
		_, err = tx.ExecContext(ctx, `UPDATE support_conversations SET last_message_at = ?,
			operator_unread_count = operator_unread_count + 1, status = ?, resolved_at = NULL, updated_at = ?
			WHERE id = ?`, ms(out.CreatedAt), domain.SupportOpen, ms(now), c.ID)
		return err
	})
	return out, created, err
}

// AddSupportOperatorMessage — оператордың жауабы. Жауап берген оператор сөйлесудің
// барлық хабарламасын оқыды деп саналады. firstUnread — қолданушы әлі оқымаған ең ескі
// оператор хабарламасы: бір «оқылмаған топ» бір push алады.
func (s *Store) AddSupportOperatorMessage(ctx context.Context, conversationID, adminID, body string, now time.Time) (msg domain.SupportMessage, conv domain.SupportConversation, firstUnread string, err error) {
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		c, err := supportConversationWhere(ctx, tx, "c.id = ?", conversationID)
		if err != nil {
			return err
		}
		msg, err = insertSupportMessage(ctx, tx, c, domain.SupportMessage{
			Sender: domain.SupportSenderOperator, AdminID: adminID, Body: body, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE support_messages SET read_at = ?
			WHERE conversation_id = ? AND sender = ? AND read_at IS NULL`, ms(now), c.ID, domain.SupportSenderUser); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE support_conversations SET last_message_at = ?,
			user_unread_count = user_unread_count + 1, operator_unread_count = 0, assigned_admin_id = ?, updated_at = ?
			WHERE id = ?`, ms(msg.CreatedAt), nullText(adminID), ms(now), c.ID); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT id FROM support_messages
			WHERE conversation_id = ? AND sender = ? AND read_at IS NULL ORDER BY created_at, id LIMIT 1`,
			c.ID, domain.SupportSenderOperator).Scan(&firstUnread); err != nil {
			return err
		}
		conv, err = supportConversationWhere(ctx, tx, "c.id = ?", c.ID)
		return err
	})
	return msg, conv, firstUnread, err
}

// MarkSupportReadByUser — қолданушы upTo хабарламасына дейінгі (қоса) оператор
// хабарламаларын оқыды. Қалған оқылмағандар саны қайтарылады.
func (s *Store) MarkSupportReadByUser(ctx context.Context, userID, upTo string, now time.Time) (int, error) {
	var unread int
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		c, err := supportConversationWhere(ctx, tx, "c.user_id = ?", userID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.InvalidField("up_to_message_id", "unknown message")
		}
		if err != nil {
			return err
		}
		var at int64
		err = tx.QueryRowContext(ctx, `SELECT created_at FROM support_messages WHERE id = ? AND conversation_id = ?`,
			upTo, c.ID).Scan(&at)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.InvalidField("up_to_message_id", "unknown message")
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE support_messages SET read_at = ?
			WHERE conversation_id = ? AND sender = ? AND read_at IS NULL
			  AND (created_at < ? OR (created_at = ? AND id <= ?))`,
			ms(now), c.ID, domain.SupportSenderOperator, at, at, upTo); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_messages
			WHERE conversation_id = ? AND sender = ? AND read_at IS NULL`, c.ID, domain.SupportSenderOperator).Scan(&unread); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE support_conversations SET user_unread_count = ?, updated_at = ?
			WHERE id = ? AND user_unread_count <> ?`, unread, ms(now), c.ID, unread)
		return err
	})
	return unread, err
}

// MarkSupportReadByOperator — оператор сөйлесудегі қолданушы хабарламаларының бәрін оқыды.
func (s *Store) MarkSupportReadByOperator(ctx context.Context, conversationID string, now time.Time) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := supportConversationWhere(ctx, tx, "c.id = ?", conversationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE support_messages SET read_at = ?
			WHERE conversation_id = ? AND sender = ? AND read_at IS NULL`, ms(now), conversationID, domain.SupportSenderUser); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE support_conversations SET operator_unread_count = 0, updated_at = ?
			WHERE id = ? AND operator_unread_count <> 0`, ms(now), conversationID)
		return err
	})
}

// SetSupportStatus — open | resolved. changed=false: сөйлесу бұрын да осы күйде болған.
func (s *Store) SetSupportStatus(ctx context.Context, conversationID, status string, now time.Time) (bool, error) {
	if _, err := s.SupportConversation(ctx, conversationID); err != nil {
		return false, err
	}
	var resolvedAt any
	if status == domain.SupportResolved {
		resolvedAt = ms(now)
	}
	res, err := s.db.Writer().ExecContext(ctx, `UPDATE support_conversations SET status = ?, resolved_at = ?, updated_at = ?
		WHERE id = ? AND status <> ?`, status, resolvedAt, ms(now), conversationID, status)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SupportFilter — әкімші тізімінің сүзгісі.
type SupportFilter struct {
	Status string // open | resolved | "" (бәрі)
	Unread bool   // оператор оқымаған хабарламасы барлары ғана
	Search string // телефон, пошта не тіркелгі/сөйлесу id бөлігі
	Page   traits.Page
}

// SupportConversationRow — тізім жолы: тіркелгі және соңғы хабарлама.
type SupportConversationRow struct {
	Conversation domain.SupportConversation
	// User — тек бүркемеленген идентификатор үшін (телефон, пошта, legacy).
	User       domain.User
	LastSender string
	LastBody   string
}

// ListSupportConversations — сөйлесулер, соңғы хабарламасы жаңасы алдымен.
func (s *Store) ListSupportConversations(ctx context.Context, f SupportFilter) ([]SupportConversationRow, int, error) {
	where, args := []string{"1 = 1"}, []any{}
	if f.Status != "" {
		where, args = append(where, "c.status = ?"), append(args, f.Status)
	}
	if f.Unread {
		where = append(where, "c.operator_unread_count > 0")
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		like := "%" + escapeLike(q) + "%"
		where = append(where, `(u.phone LIKE ? ESCAPE '\' OR u.email LIKE ? ESCAPE '\' OR u.id LIKE ? ESCAPE '\'
			OR c.id LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like, like)
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM support_conversations c
		JOIN users u ON u.id = c.user_id WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT `+supportConversationColumns+`,
		u.phone, u.email, u.legacy_client,
		COALESCE((SELECT m.sender FROM support_messages m WHERE m.conversation_id = c.id
			ORDER BY m.created_at DESC, m.id DESC LIMIT 1), ''),
		COALESCE((SELECT m.body FROM support_messages m WHERE m.conversation_id = c.id
			ORDER BY m.created_at DESC, m.id DESC LIMIT 1), '')
		FROM support_conversations c JOIN users u ON u.id = c.user_id
		WHERE `+clause+` ORDER BY c.last_message_at DESC, c.id DESC LIMIT ? OFFSET ?`,
		append(args, f.Page.Limit, f.Page.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []SupportConversationRow{}
	for rows.Next() {
		var (
			row                  SupportConversationRow
			phone, email, legacy sql.NullString
		)
		c, err := scanSupportConversation(rows, &phone, &email, &legacy, &row.LastSender, &row.LastBody)
		if err != nil {
			return nil, 0, err
		}
		row.Conversation = c
		row.User = domain.User{ID: c.UserID, Phone: text(phone), Email: text(email), LegacyClient: text(legacy)}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// SupportSummary — ашық сөйлесулер және оператор оқымаған хабарламасы барлар саны (бүйір мәзір белгісі).
func (s *Store) SupportSummary(ctx context.Context) (open, unread int, err error) {
	err = s.db.Reader().QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN operator_unread_count > 0 THEN 1 ELSE 0 END), 0)
		FROM support_conversations`, domain.SupportOpen).Scan(&open, &unread)
	return open, unread, err
}

// SupportDevice — тіркелгінің соңғы көрінген орнатуы (қолдау бетіндегі қысқаша мәлімет).
type SupportDevice struct {
	Platform   string
	AppVersion string
	OSVersion  string
	LastSeenAt time.Time
}

// LatestSupportDevice — соңғы көрінген орнату; жоқ болса found=false.
func (s *Store) LatestSupportDevice(ctx context.Context, userID string) (SupportDevice, bool, error) {
	var (
		d    SupportDevice
		seen int64
	)
	err := s.db.Reader().QueryRowContext(ctx, `SELECT platform, app_version, os_version, last_seen_at
		FROM app_installations WHERE user_id = ? ORDER BY last_seen_at DESC, id LIMIT 1`, userID).
		Scan(&d.Platform, &d.AppVersion, &d.OSVersion, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return d, false, nil
	}
	if err != nil {
		return d, false, err
	}
	d.LastSeenAt = timeFrom(seen)
	return d, true, nil
}
