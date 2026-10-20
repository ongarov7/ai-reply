// Package support — қолданушы мен қолдау операторы арасындағы чат.
//
// The person writes from the app; operators answer from the admin panel and
// are told about new messages there only. An operator reply sends one
// support_reply push per unread burst. Message text is never written to the
// log: only ids, lengths and counters.
package support

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Беттің әдепкі және ең үлкен өлшемі.
const (
	DefaultPageSize = 30
	MaxPageSize     = 100
)

// Notifier — оператор жауабы туралы push (notifications.Events).
type Notifier interface {
	SupportReplied(ctx context.Context, userID, conversationID, firstUnreadMessageID string)
}

// DiagnosticsSource — қолданушының рұқсат диагностикасы (әкімшінің сөйлесу беті үшін).
//
// Hook for the permission diagnostics subsystem: when it is wired, the admin
// conversation view shows its summary next to the history; nil shows nothing.
type DiagnosticsSource func(ctx context.Context, userID string) (any, error)

// Service — қолдау чаты.
type Service struct {
	repo        *repository.Store
	subs        *subscriptions.Service
	notify      Notifier
	diagnostics DiagnosticsSource
	log         *slog.Logger
	clock       traits.Clock
}

// New — қызмет.
func New(repo *repository.Store, subs *subscriptions.Service, log *slog.Logger) *Service {
	return &Service{repo: repo, subs: subs, log: log, clock: traits.SystemClock{}}
}

// WithClock — тестке.
func (s *Service) WithClock(c traits.Clock) *Service { s.clock = c; return s }

// WithNotifier — оператор жауабының push-ы.
func (s *Service) WithNotifier(n Notifier) *Service { s.notify = n; return s }

// WithDiagnostics — әкімші бетіндегі рұқсат диагностикасының көзі.
func (s *Service) WithDiagnostics(d DiagnosticsSource) *Service { s.diagnostics = d; return s }

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Text — хабарлама мәтінін тексереді: шетіндегі бос орындарсыз 1–2000 таңба.
func Text(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	switch n := traits.RuneLen(text); {
	case n == 0:
		return "", domain.InvalidField("text", "required")
	case n > domain.SupportTextMax:
		return "", domain.InvalidField("text", "at most 2000 characters")
	}
	return text, nil
}

// Cursor — ?before=|after=&limit= (бірі ғана; limit 1–100, әдепкісі 30).
func Cursor(before, after string, limit int) (repository.SupportCursor, error) {
	before, after = strings.TrimSpace(before), strings.TrimSpace(after)
	if before != "" && after != "" {
		return repository.SupportCursor{}, domain.InvalidField("after", "use before or after, not both")
	}
	switch {
	case limit <= 0:
		limit = DefaultPageSize
	case limit > MaxPageSize:
		limit = MaxPageSize
	}
	return repository.SupportCursor{Before: before, After: after, Limit: limit}, nil
}

// ---------------------------------------------------------------- user side

// Conversation — қолданушының сөйлесуі; әлі жазбаған болса nil.
func (s *Service) Conversation(ctx context.Context, userID string) (*domain.SupportConversation, error) {
	c, err := s.repo.SupportConversationByUser(ctx, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Messages — қолданушының хабарламалары, уақыт бойынша өсу ретімен.
func (s *Service) Messages(ctx context.Context, userID string, cur repository.SupportCursor) ([]domain.SupportMessage, bool, error) {
	c, err := s.Conversation(ctx, userID)
	if err != nil {
		return nil, false, err
	}
	if c == nil {
		if cur.Before != "" {
			return nil, false, domain.InvalidField("before", "unknown message")
		}
		if cur.After != "" {
			return nil, false, domain.InvalidField("after", "unknown message")
		}
		return []domain.SupportMessage{}, false, nil
	}
	return s.repo.SupportMessages(ctx, c.ID, cur)
}

// Send — қолданушы жазды. Бірінші хабарлама сөйлесуді ашады, шешілген сөйлесуді
// қайта ашады; бұрын көрген client_message_id сол хабарламаны қайтарады.
func (s *Service) Send(ctx context.Context, userID, rawText, clientMessageID string) (domain.SupportMessage, error) {
	text, err := Text(rawText)
	if err != nil {
		return domain.SupportMessage{}, err
	}
	clientID := strings.ToLower(strings.TrimSpace(clientMessageID))
	if clientID != "" && !uuidPattern.MatchString(clientID) {
		return domain.SupportMessage{}, domain.InvalidField("client_message_id", "not a UUID")
	}
	msg, created, err := s.repo.AddSupportUserMessage(ctx, userID, clientID, text, s.clock.Now())
	if err != nil {
		return domain.SupportMessage{}, err
	}
	s.log.Info("support message received", "user_id", userID, "conversation_id", msg.ConversationID,
		"message_id", msg.ID, "length", traits.RuneLen(text), "created", created)
	return msg, nil
}

// MarkRead — қолданушы upTo хабарламасына дейін оқыды; қалған оқылмағандар саны.
func (s *Service) MarkRead(ctx context.Context, userID, upTo string) (int, error) {
	upTo = strings.TrimSpace(upTo)
	if upTo == "" {
		return 0, domain.InvalidField("up_to_message_id", "required")
	}
	return s.repo.MarkSupportReadByUser(ctx, userID, upTo, s.clock.Now())
}

// ---------------------------------------------------------------- operator side

// List — әкімші тізімі. status: open | resolved | all | "".
func (s *Service) List(ctx context.Context, status string, unread bool, search string, page traits.Page) ([]repository.SupportConversationRow, int, error) {
	if status == "all" {
		status = ""
	}
	if status != "" && !traits.OneOf(status, domain.SupportStatuses...) {
		return nil, 0, domain.InvalidField("status", "unknown value")
	}
	return s.repo.ListSupportConversations(ctx, repository.SupportFilter{
		Status: status, Unread: unread, Search: traits.Clamp(strings.TrimSpace(search), 80), Page: page,
	})
}

// Summary — ашық сөйлесулер және оператор оқымағаны бар сөйлесулер саны.
func (s *Service) Summary(ctx context.Context) (open, unread int, err error) {
	return s.repo.SupportSummary(ctx)
}

// Detail — әкімшінің сөйлесу беті: сөйлесу, тіркелгі туралы қысқаша, диагностика.
type Detail struct {
	Conversation domain.SupportConversation
	User         domain.User
	Entitlement  domain.Entitlement
	Device       *repository.SupportDevice
	Diagnostics  any
}

// Detail — сөйлесу және оның иесі туралы қысқаша мәлімет (мазмұнсыз).
func (s *Service) Detail(ctx context.Context, conversationID string) (Detail, error) {
	c, err := s.repo.SupportConversation(ctx, conversationID)
	if err != nil {
		return Detail{}, err
	}
	user, err := s.repo.UserByID(ctx, c.UserID)
	if err != nil {
		return Detail{}, err
	}
	out := Detail{Conversation: c, User: user}
	if out.Entitlement, err = s.subs.Entitlement(ctx, c.UserID); err != nil {
		return Detail{}, err
	}
	device, found, err := s.repo.LatestSupportDevice(ctx, c.UserID)
	if err != nil {
		return Detail{}, err
	}
	if found {
		out.Device = &device
	}
	if s.diagnostics != nil {
		// Diagnostics only add context: a failure there never hides the conversation.
		if out.Diagnostics, err = s.diagnostics(ctx, c.UserID); err != nil {
			s.log.Error("support diagnostics failed", "conversation_id", c.ID, "error", err.Error())
			out.Diagnostics = nil
		}
	}
	return out, nil
}

// OperatorMessages — сөйлесу хабарламалары әкімшіге.
func (s *Service) OperatorMessages(ctx context.Context, conversationID string, cur repository.SupportCursor) ([]domain.SupportMessage, bool, error) {
	if _, err := s.repo.SupportConversation(ctx, conversationID); err != nil {
		return nil, false, err
	}
	return s.repo.SupportMessages(ctx, conversationID, cur)
}

// Reply — оператордың жауабы. Жазылғаннан кейін қолданушыға push кетеді (бір
// оқылмаған топқа біреу ғана: кілті — топтың бірінші хабарламасы).
func (s *Service) Reply(ctx context.Context, conversationID, adminID, rawText string) (domain.SupportMessage, error) {
	text, err := Text(rawText)
	if err != nil {
		return domain.SupportMessage{}, err
	}
	msg, conv, firstUnread, err := s.repo.AddSupportOperatorMessage(ctx, conversationID, adminID, text, s.clock.Now())
	if err != nil {
		return domain.SupportMessage{}, err
	}
	s.log.Info("support reply sent", "conversation_id", conv.ID, "message_id", msg.ID,
		"length", traits.RuneLen(text), "user_unread", conv.UserUnread)
	if s.notify != nil {
		s.notify.SupportReplied(ctx, conv.UserID, conv.ID, firstUnread)
	}
	return msg, nil
}

// SetStatus — open | resolved. changed=false: бұрын да осы күйде.
func (s *Service) SetStatus(ctx context.Context, conversationID, status string) (bool, error) {
	if !traits.OneOf(status, domain.SupportStatuses...) {
		return false, domain.InvalidField("status", "unknown value")
	}
	return s.repo.SetSupportStatus(ctx, conversationID, status, s.clock.Now())
}

// MarkOperatorRead — оператор сөйлесуді оқыды.
func (s *Service) MarkOperatorRead(ctx context.Context, conversationID string) error {
	return s.repo.MarkSupportReadByOperator(ctx, conversationID, s.clock.Now())
}
