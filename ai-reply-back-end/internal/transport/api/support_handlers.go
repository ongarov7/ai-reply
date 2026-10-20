package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/middleware"
	"github.com/aireply/ai-reply-back-end/internal/support"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// registerSupport — қолдау чатының маршруттары (қызмет қосылмаса — жоқ).
//
// Like the other account features they need a signed-in person with the
// current consent. Only writing has its own per-user bucket: the app polls
// ?after= every few seconds while the chat is open.
func (s *Server) registerSupport(mux *http.ServeMux, perUser middleware.KeyFunc) {
	if s.support == nil {
		return
	}
	writeLimit := middleware.RateLimit(s.limiter, "support", s.cfg.Limits.SupportPerMinute, time.Minute, perUser)
	user := func(h http.HandlerFunc) http.Handler { return s.requireUser(s.requireConsent(h)) }
	mux.Handle("GET /api/v1/support/conversation", user(s.handleSupportConversation))
	mux.Handle("GET /api/v1/support/messages", user(s.handleSupportMessages))
	mux.Handle("POST /api/v1/support/messages", s.requireUser(s.requireConsent(writeLimit(http.HandlerFunc(s.handleSupportSend)))))
	mux.Handle("POST /api/v1/support/read", user(s.handleSupportRead))
}

type supportConversationDTO struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	UnreadCount int    `json:"unread_count"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// supportMessageDTO — қолданушы көретін хабарлама: оператордың кім екені айтылмайды.
type supportMessageDTO struct {
	ID        string  `json:"id"`
	Sender    string  `json:"sender"`
	Text      string  `json:"text"`
	CreatedAt string  `json:"created_at"`
	ReadAt    *string `json:"read_at"`
}

func supportMessageOut(m domain.SupportMessage) supportMessageDTO {
	out := supportMessageDTO{ID: m.ID, Sender: m.Sender, Text: m.Body, CreatedAt: m.CreatedAt.Format(time.RFC3339)}
	if m.ReadAt != nil {
		v := m.ReadAt.Format(time.RFC3339)
		out.ReadAt = &v
	}
	return out
}

// handleSupportConversation — GET /api/v1/support/conversation: әлі жазбаған болса null.
func (s *Server) handleSupportConversation(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	c, err := s.support.Conversation(r.Context(), user.ID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	var out *supportConversationDTO
	if c != nil {
		out = &supportConversationDTO{ID: c.ID, Status: c.Status, UnreadCount: c.UserUnread,
			CreatedAt: c.CreatedAt.Format(time.RFC3339), UpdatedAt: c.UpdatedAt.Format(time.RFC3339)}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"conversation": out})
}

// handleSupportMessages — GET /api/v1/support/messages?before=<id>|after=<id>&limit=30.
func (s *Server) handleSupportMessages(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	cur, err := support.Cursor(query.Get("before"), query.Get("after"), limit)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	messages, more, err := s.support.Messages(r.Context(), user.ID, cur)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	out := make([]supportMessageDTO, 0, len(messages))
	for _, m := range messages {
		out = append(out, supportMessageOut(m))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"messages": out, "has_more": more})
}

// handleSupportSend — POST /api/v1/support/messages {text, client_message_id} → 201 {message}.
//
// A retry with the same client_message_id answers the message stored the
// first time. The text is never logged.
func (s *Server) handleSupportSend(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body struct {
		Text            string `json:"text"`
		ClientMessageID string `json:"client_message_id"`
	}
	if err := httpx.Decode(w, r, s.cfg.Limits.RequestBodyBytes, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	msg, err := s.support.Send(r.Context(), user.ID, body.Text, body.ClientMessageID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"message": supportMessageOut(msg)})
}

// handleSupportRead — POST /api/v1/support/read {up_to_message_id} → {unread_count}.
func (s *Server) handleSupportRead(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	var body struct {
		UpToMessageID string `json:"up_to_message_id"`
	}
	if err := httpx.Decode(w, r, 1024, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	unread, err := s.support.MarkRead(r.Context(), user.ID, body.UpToMessageID)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]int{"unread_count": unread})
}
