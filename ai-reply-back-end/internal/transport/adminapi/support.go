package adminapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/support"
	"github.com/aireply/ai-reply-back-end/internal/traits"
	"github.com/aireply/ai-reply-back-end/internal/transport/httpx"
)

// supportRepliesPerMinute — бір әкімшінің бір минуттағы жауаптары (екі рет басылған түйме емес, ағын).
const supportRepliesPerMinute = 60

// registerSupport — «Қолдау» беті: сөйлесулер, тарих, жауап, шешілді/қайта ашу.
//
// The history lives only behind these routes, never in the user detail
// payload. Operators are told about new messages here only (the sidebar badge
// polls /support/summary).
func (s *Server) registerSupport(mux *http.ServeMux) {
	if s.support == nil {
		return
	}
	mux.Handle("GET /api/v1/admin/support/summary", s.guard(s.handleSupportSummary))
	mux.Handle("GET /api/v1/admin/support/conversations", s.guard(s.handleSupportConversations))
	mux.Handle("GET /api/v1/admin/support/conversations/{id}", s.guard(s.handleSupportConversation))
	mux.Handle("POST /api/v1/admin/support/conversations/{id}/messages", s.guard(s.handleSupportReply))
	mux.Handle("POST /api/v1/admin/support/conversations/{id}/resolve", s.guard(s.handleSupportStatus(domain.SupportResolved)))
	mux.Handle("POST /api/v1/admin/support/conversations/{id}/reopen", s.guard(s.handleSupportStatus(domain.SupportOpen)))
	mux.Handle("POST /api/v1/admin/support/conversations/{id}/read", s.guard(s.handleSupportRead))
}

func (s *Server) supportConversationOut(c domain.SupportConversation) map[string]any {
	loc := s.cfg.App.Location()
	return map[string]any{
		"id": c.ID, "user_id": c.UserID, "status": c.Status,
		"unread": c.OperatorUnread, "user_unread": c.UserUnread,
		"last_message_at": c.LastMessageAt.In(loc).Format("2006-01-02 15:04"),
		"resolved_at":     optionalTime(c.ResolvedAt, loc),
		"created_at":      c.CreatedAt.In(loc).Format("2006-01-02 15:04"),
	}
}

func (s *Server) supportMessageOut(m domain.SupportMessage) map[string]any {
	loc := s.cfg.App.Location()
	return map[string]any{
		"id": m.ID, "sender": m.Sender, "text": m.Body,
		"created_at": m.CreatedAt.In(loc).Format("2006-01-02 15:04"), "read_at": optionalTime(m.ReadAt, loc),
	}
}

// handleSupportSummary — бүйір мәзірдің белгісі: ашық және оператор оқымаған сөйлесулер.
func (s *Server) handleSupportSummary(w http.ResponseWriter, r *http.Request) {
	open, unread, err := s.support.Summary(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]int{"open": open, "unread": unread})
}

// handleSupportConversations — ?status=open|resolved|all&unread=1&q=&page=.
func (s *Server) handleSupportConversations(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page := 1
	if v, err := strconv.Atoi(query.Get("page")); err == nil && v > 1 {
		page = v
	}
	limit := 25
	rows, total, err := s.support.List(r.Context(), query.Get("status"), query.Get("unread") == "1",
		query.Get("q"), traits.NewPage(limit, (page-1)*limit))
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := s.supportConversationOut(row.Conversation)
		item["identifier"] = maskIdentifier(row.User)
		item["last_sender"] = row.LastSender
		item["preview"] = traits.Clamp(row.LastBody, 120)
		out = append(out, item)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"conversations": out, "total": total, "page": page, "limit": limit})
}

// handleSupportConversation — сөйлесу, оның иесі туралы қысқаша және хабарламалар беті
// (?before=<id>|after=<id>&limit=). Ашу оқылды деп белгілемейді: ол — POST .../read.
func (s *Server) handleSupportConversation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	cur, err := support.Cursor(query.Get("before"), query.Get("after"), limit)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	detail, err := s.support.Detail(r.Context(), id)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	messages, more, err := s.support.OperatorMessages(r.Context(), id, cur)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	loc := s.cfg.App.Location()
	locale := adminFrom(r.Context()).Locale
	u, ent := detail.User, detail.Entitlement
	user := map[string]any{
		"id": u.ID, "identifier": maskIdentifier(u), "status": u.Status, "locale": u.Locale,
		"plan_code": ent.Plan.Code, "plan_name": ent.Plan.LocalizedName(locale),
		"platform": u.Platform, "app_version": u.AppVersion, "os_version": u.OSVersion, "last_seen": "",
	}
	if d := detail.Device; d != nil {
		user["platform"], user["app_version"], user["os_version"] = d.Platform, d.AppVersion, d.OSVersion
		user["last_seen"] = d.LastSeenAt.In(loc).Format("2006-01-02 15:04")
	}
	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		out = append(out, s.supportMessageOut(m))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"conversation": s.supportConversationOut(detail.Conversation),
		"user":         user,
		"diagnostics":  detail.Diagnostics,
		"messages":     out,
		"has_more":     more,
	})
}

// handleSupportReply — оператордың жауабы {text}. Аудитке тек ұзындығы жазылады, мәтіні емес.
func (s *Server) handleSupportReply(w http.ResponseWriter, r *http.Request) {
	adminUser := adminFrom(r.Context())
	var body struct {
		Text string `json:"text"`
	}
	if err := httpx.Decode(w, r, 32*1024, &body); err != nil {
		httpx.Fail(w, err)
		return
	}
	if !s.allow(w, "admin_support_reply:"+adminUser.ID, supportRepliesPerMinute, time.Minute,
		"Too many replies. Try again shortly.") {
		return
	}
	id := r.PathValue("id")
	msg, err := s.support.Reply(r.Context(), id, adminUser.ID, body.Text)
	if err != nil {
		httpx.Fail(w, err)
		return
	}
	s.admin.Audit(r.Context(), adminUser, s.ip(r), "support.reply", "support_conversation", id,
		map[string]any{"length": traits.RuneLen(msg.Body)})
	httpx.JSON(w, http.StatusCreated, map[string]any{"message": s.supportMessageOut(msg)})
}

// handleSupportStatus — шешілді / қайта ашу (күй өзгерсе аудитке жазылады).
func (s *Server) handleSupportStatus(status string) http.HandlerFunc {
	action := "support.reopen"
	if status == domain.SupportResolved {
		action = "support.resolve"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		adminUser := adminFrom(r.Context())
		id := r.PathValue("id")
		changed, err := s.support.SetStatus(r.Context(), id, status)
		if err != nil {
			httpx.Fail(w, err)
			return
		}
		if changed {
			s.admin.Audit(r.Context(), adminUser, s.ip(r), action, "support_conversation", id, nil)
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "changed": changed})
	}
}

// handleSupportRead — оператор сөйлесуді оқыды (белгі ғана; аудитке жазылмайды).
func (s *Server) handleSupportRead(w http.ResponseWriter, r *http.Request) {
	if err := s.support.MarkOperatorRead(r.Context(), r.PathValue("id")); err != nil {
		httpx.Fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
