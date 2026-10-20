package apptest

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/database"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/migrations"
)

// ---------------------------------------------------------------- helpers

func clientMessageID(seed int) string { return fmt.Sprintf("6f1c2a3b-0000-4000-8000-%012d", seed) }

func (h *harness) supportSend(access, text, clientID string) response {
	h.t.Helper()
	body := map[string]any{"text": text}
	if clientID != "" {
		body["client_message_id"] = clientID
	}
	return h.do(http.MethodPost, "/api/v1/support/messages", body, h.auth(access))
}

func (h *harness) mustSupportSend(access, text string) string {
	h.t.Helper()
	res := h.supportSend(access, text, "")
	mustStatus(h.t, res, http.StatusCreated, "")
	return res.str("message", "id")
}

func (h *harness) supportMessages(access, query string) response {
	h.t.Helper()
	return h.do(http.MethodGet, "/api/v1/support/messages"+query, nil, h.auth(access))
}

// supportItems — жауаптағы хабарламалар (messages тізімі).
func supportItems(res response) []map[string]any {
	var out []map[string]any
	list, _ := res.body["messages"].([]any)
	for _, item := range list {
		out = append(out, item.(map[string]any))
	}
	return out
}

func supportTexts(res response) string {
	var out []string
	for _, m := range supportItems(res) {
		out = append(out, m["text"].(string))
	}
	return strings.Join(out, ",")
}

func (h *harness) supportAdmin(admin adminSession, method, path string, body any) response {
	h.t.Helper()
	return h.do(method, "/api/v1/admin/support"+path, body, admin.headers(h.cfg.Admin.CookieName))
}

func (h *harness) supportConversationID(access string) string {
	h.t.Helper()
	res := h.do(http.MethodGet, "/api/v1/support/conversation", nil, h.auth(access))
	mustStatus(h.t, res, http.StatusOK, "")
	return res.str("conversation", "id")
}

// ---------------------------------------------------------------- user API

// Бірінші хабарлама сөйлесуді ашады; client_message_id қайталанса, сол хабарлама қайтарылады.
func TestSupportConversationStartsWithTheFirstMessage(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 701 400 00 01")

	config := h.do(http.MethodGet, "/api/v1/config", nil, nil)
	if features, _ := config.body["features"].(map[string]any); features["support_chat"] != true {
		t.Fatalf("features.support_chat = %v", features["support_chat"])
	}
	empty := h.do(http.MethodGet, "/api/v1/support/conversation", nil, h.auth(s.access))
	mustStatus(t, empty, http.StatusOK, "")
	if v, ok := empty.body["conversation"]; !ok || v != nil {
		t.Fatalf("conversation before the first message: %s", empty.raw)
	}
	if list := h.supportMessages(s.access, ""); list.status != http.StatusOK || len(supportItems(list)) != 0 || list.body["has_more"] != false {
		t.Fatalf("messages before the first message: %d %s", list.status, list.raw)
	}

	first := h.supportSend(s.access, "  Сәлеметсіз бе!\nКлавиатура көрінбейді.  ", clientMessageID(1))
	mustStatus(t, first, http.StatusCreated, "")
	if first.str("message", "sender") != "user" || first.str("message", "text") != "Сәлеметсіз бе!\nКлавиатура көрінбейді." ||
		first.str("message", "created_at") == "" {
		t.Fatalf("message: %s", first.raw)
	}
	if m := first.body["message"].(map[string]any); m["read_at"] != nil {
		t.Fatalf("a new message is unread: %s", first.raw)
	}
	retry := h.supportSend(s.access, "Сәлеметсіз бе! (қайта)", strings.ToUpper(clientMessageID(1)))
	mustStatus(t, retry, http.StatusCreated, "")
	if retry.str("message", "id") != first.str("message", "id") || retry.str("message", "text") != first.str("message", "text") {
		t.Fatalf("a retry made another message: %s vs %s", retry.raw, first.raw)
	}

	conv := h.do(http.MethodGet, "/api/v1/support/conversation", nil, h.auth(s.access))
	if conv.str("conversation", "status") != "open" || conv.num("conversation", "unread_count") != 0 ||
		conv.str("conversation", "id") == "" || conv.str("conversation", "created_at") == "" || conv.str("conversation", "updated_at") == "" {
		t.Fatalf("conversation: %s", conv.raw)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM support_messages`); n != 1 {
		t.Fatalf("messages stored = %d", n)
	}
	if n := h.scalar(`SELECT operator_unread_count FROM support_conversations WHERE user_id = ?`, s.userID); n != 1 {
		t.Fatalf("operator unread = %d", n)
	}
	second := h.mustSupportSend(s.access, "Тағы бір сұрақ")
	if second == first.str("message", "id") || h.scalar(`SELECT COUNT(*) FROM support_conversations`) != 1 {
		t.Fatal("the second message must join the same conversation")
	}
	if strings.Contains(h.logs.String(), "Клавиатура көрінбейді") || strings.Contains(h.logs.String(), "Тағы бір сұрақ") {
		t.Fatal("the log contains message text")
	}
}

func TestSupportMessageValidation(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 701 400 00 02")

	for name, body := range map[string]map[string]any{
		"empty":        {"text": ""},
		"blank":        {"text": " \n\t "},
		"too long":     {"text": strings.Repeat("ә", 2001)},
		"bad uuid":     {"text": "Сәлем", "client_message_id": "not-a-uuid"},
		"unknown key":  {"text": "Сәлем", "operator_name": "x"},
		"missing text": {"client_message_id": clientMessageID(9)},
	} {
		res := h.do(http.MethodPost, "/api/v1/support/messages", body, h.auth(s.access))
		if res.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, res.status, res.raw)
		}
	}
	if n := h.scalar(`SELECT COUNT(*) FROM support_conversations`); n != 0 {
		t.Fatalf("a refused message opened a conversation (%d)", n)
	}
	mustStatus(t, h.supportSend(s.access, strings.Repeat("ә", 2000), ""), http.StatusCreated, "")

	// Consent is required like for the other account features.
	noConsent := h.signInWithoutConsent("support.noconsent@example.com")
	mustStatus(t, h.supportSend(noConsent.access, "Сәлем", ""), http.StatusForbidden, "CONSENT_REQUIRED")
	mustStatus(t, h.do(http.MethodGet, "/api/v1/support/conversation", nil, nil), http.StatusUnauthorized, "UNAUTHORIZED")
}

// Беттер әрқашан уақыт бойынша өседі; before/after екі бағытта да has_more дұрыс.
func TestSupportMessagesPageBothWays(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 701 400 00 03")
	var ids []string
	for i := 1; i <= 7; i++ {
		ids = append(ids, h.mustSupportSend(s.access, fmt.Sprintf("m%d", i)))
	}

	latest := h.supportMessages(s.access, "?limit=3")
	if supportTexts(latest) != "m5,m6,m7" || latest.body["has_more"] != true {
		t.Fatalf("latest page: %s", latest.raw)
	}
	older := h.supportMessages(s.access, "?limit=3&before="+ids[4])
	if supportTexts(older) != "m2,m3,m4" || older.body["has_more"] != true {
		t.Fatalf("older page: %s", older.raw)
	}
	oldest := h.supportMessages(s.access, "?limit=3&before="+ids[1])
	if supportTexts(oldest) != "m1" || oldest.body["has_more"] != false {
		t.Fatalf("oldest page: %s", oldest.raw)
	}
	newer := h.supportMessages(s.access, "?limit=2&after="+ids[2])
	if supportTexts(newer) != "m4,m5" || newer.body["has_more"] != true {
		t.Fatalf("newer page: %s", newer.raw)
	}
	tail := h.supportMessages(s.access, "?limit=10&after="+ids[4])
	if supportTexts(tail) != "m6,m7" || tail.body["has_more"] != false {
		t.Fatalf("tail page: %s", tail.raw)
	}
	none := h.supportMessages(s.access, "?after="+ids[6])
	if none.status != http.StatusOK || len(supportItems(none)) != 0 || none.body["has_more"] != false {
		t.Fatalf("after the last message: %s", none.raw)
	}
	if all := h.supportMessages(s.access, "?limit=500"); len(supportItems(all)) != 7 {
		t.Fatalf("a large limit is capped, not refused: %s", all.raw)
	}
	mustStatus(t, h.supportMessages(s.access, "?before="+ids[1]+"&after="+ids[0]), http.StatusBadRequest, "INVALID_REQUEST")
	mustStatus(t, h.supportMessages(s.access, "?before=00000000-0000-4000-8000-000000000000"), http.StatusBadRequest, "INVALID_REQUEST")

	// Messages of the same millisecond still come in the order they were written.
	stamps := h.text(`SELECT GROUP_CONCAT(created_at) FROM (SELECT created_at FROM support_messages ORDER BY created_at)`)
	if parts := strings.Split(stamps, ","); len(parts) != 7 || parts[0] == parts[1] {
		t.Fatalf("created_at = %s", stamps)
	}
}

// Бір қолданушы басқаның хабарламасын көре алмайды, оқылды деп белгілей алмайды.
func TestSupportConversationsArePrivate(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("+7 701 400 00 04")
	b := h.signIn("+7 701 400 00 05")
	aMessage := h.mustSupportSend(a.access, "Менің сұрағым")

	if conv := h.do(http.MethodGet, "/api/v1/support/conversation", nil, h.auth(b.access)); conv.body["conversation"] != nil {
		t.Fatalf("B sees a conversation: %s", conv.raw)
	}
	if list := h.supportMessages(b.access, ""); len(supportItems(list)) != 0 {
		t.Fatalf("B sees messages: %s", list.raw)
	}
	mustStatus(t, h.supportMessages(b.access, "?after="+aMessage), http.StatusBadRequest, "INVALID_REQUEST")
	mustStatus(t, h.do(http.MethodPost, "/api/v1/support/read", map[string]any{"up_to_message_id": aMessage}, h.auth(b.access)),
		http.StatusBadRequest, "INVALID_REQUEST")
	h.mustSupportSend(b.access, "Менің де сұрағым")
	mustStatus(t, h.supportMessages(b.access, "?after="+aMessage), http.StatusBadRequest, "INVALID_REQUEST")
	if list := h.supportMessages(b.access, ""); supportTexts(list) != "Менің де сұрағым" {
		t.Fatalf("B's history: %s", list.raw)
	}

	// The admin routes need an admin session, and writes need the CSRF header.
	conversation := h.supportConversationID(a.access)
	mustStatus(t, h.do(http.MethodGet, "/api/v1/admin/support/conversations", nil, nil), http.StatusUnauthorized, "UNAUTHORIZED")
	mustStatus(t, h.do(http.MethodGet, "/api/v1/admin/support/conversations/"+conversation, nil, h.auth(a.access)),
		http.StatusUnauthorized, "UNAUTHORIZED")
	admin := h.signInAdmin()
	noCSRF := map[string]string{"Cookie": h.cfg.Admin.CookieName + "=" + admin.cookie}
	for _, path := range []string{"/messages", "/resolve", "/reopen", "/read"} {
		res := h.do(http.MethodPost, "/api/v1/admin/support/conversations/"+conversation+path, map[string]any{"text": "x"}, noCSRF)
		mustStatus(t, res, http.StatusForbidden, "CSRF_MISMATCH")
	}
	if n := h.scalar(`SELECT COUNT(*) FROM support_messages WHERE sender = 'operator'`); n != 0 {
		t.Fatal("a request without CSRF wrote a reply")
	}
	mustStatus(t, h.supportAdmin(admin, http.MethodGet, "/conversations/unknown-id", nil), http.StatusNotFound, "NOT_FOUND")
	mustStatus(t, h.supportAdmin(admin, http.MethodPost, "/conversations/unknown-id/messages", map[string]any{"text": "x"}),
		http.StatusNotFound, "NOT_FOUND")
}

// ---------------------------------------------------------------- operator

// Оператор жауабы: қолданушыда оқылмаған саны өседі, бір «оқылмаған топқа» бір push.
func TestSupportReplyNotifiesOncePerUnreadBurst(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 701 400 00 06")
	h.mustRegister(installation(installID(406), domain.PlatformIOS, fcmToken(406)), s.access)
	question := h.mustSupportSend(s.access, "Пернетақта жұмыс істемейді")
	conversation := h.supportConversationID(s.access)
	admin := h.signInAdmin()

	reply := func(text string) string {
		t.Helper()
		res := h.supportAdmin(admin, http.MethodPost, "/conversations/"+conversation+"/messages", map[string]any{"text": text})
		mustStatus(t, res, http.StatusCreated, "")
		if res.str("message", "sender") != "operator" || res.str("message", "text") != text {
			t.Fatalf("reply: %s", res.raw)
		}
		return res.str("message", "id")
	}
	first := reply("Сәлеметсіз бе! Баптауларды тексеріңіз.")
	h.tick()
	second := reply("Тағы бір кеңес: қолданбаны қайта ашыңыз.")
	h.tick()

	pushes := h.pushesOf(domain.TypeSupportReply)
	if len(pushes) != 1 {
		t.Fatalf("pushes after two replies = %d", len(pushes))
	}
	p := pushes[0]
	if p.Data["link"] != "aireply://support" || p.Data["conversation_id"] != conversation || p.Data["category"] != domain.CategoryAccount ||
		p.Title != "Қолдау қызметі жауап берді" || strings.Contains(p.Body, "Баптауларды") {
		t.Fatalf("push = %+v", p)
	}
	conv := h.do(http.MethodGet, "/api/v1/support/conversation", nil, h.auth(s.access))
	if conv.num("conversation", "unread_count") != 2 {
		t.Fatalf("unread after two replies: %s", conv.raw)
	}
	// The operator who answered has read the question.
	list := h.supportMessages(s.access, "")
	if items := supportItems(list); len(items) != 3 || items[0]["id"] != question || items[0]["read_at"] == nil ||
		items[1]["sender"] != "operator" || items[1]["read_at"] != nil {
		t.Fatalf("history: %s", list.raw)
	}
	if strings.Contains(string(list.raw), adminEmail) || strings.Contains(string(list.raw), "operator_name") || strings.Contains(string(list.raw), "admin_id") {
		t.Fatalf("the operator is identified: %s", list.raw)
	}

	read := h.do(http.MethodPost, "/api/v1/support/read", map[string]any{"up_to_message_id": first}, h.auth(s.access))
	mustStatus(t, read, http.StatusOK, "")
	if read.num("unread_count") != 1 {
		t.Fatalf("unread after reading the first reply: %s", read.raw)
	}
	read = h.do(http.MethodPost, "/api/v1/support/read", map[string]any{"up_to_message_id": second}, h.auth(s.access))
	if read.num("unread_count") != 0 {
		t.Fatalf("unread after reading everything: %s", read.raw)
	}
	mustStatus(t, h.do(http.MethodPost, "/api/v1/support/read", map[string]any{"up_to_message_id": ""}, h.auth(s.access)),
		http.StatusBadRequest, "INVALID_REQUEST")

	// Everything was read: the next reply starts a new burst and a new push.
	reply("Мәселе шешілді ме?")
	h.tick()
	if n := len(h.pushesOf(domain.TypeSupportReply)); n != 2 {
		t.Fatalf("pushes after a new burst = %d", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM notifications WHERE type = ? AND user_id = ?`, domain.TypeSupportReply, s.userID); n != 2 {
		t.Fatalf("support notifications = %d", n)
	}
}

// Сөйлесу бетінде қолданушы құрылғыларының пернетақта мен рұқсат күйі көрінеді.
func TestSupportConversationShowsPermissionDiagnostics(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("support.diag@example.com")
	h.mustRegister(installation(installID(408), domain.PlatformIOS, fcmToken(408)), s.access)
	body := diagnosticsBody()
	body["full_access_status"] = "observed_denied"
	if res := h.reportDiagnostics(installID(408), body, s.access); res.status != http.StatusOK {
		t.Fatalf("diagnostics: %d %s", res.status, res.raw)
	}
	h.mustSupportSend(s.access, "Қою жұмыс істемейді")
	bare := h.signIn("support.nodevice@example.com")
	h.mustSupportSend(bare.access, "Құрылғысыз сұрақ")
	admin := h.signInAdmin()

	detail := h.supportAdmin(admin, http.MethodGet, "/conversations/"+h.supportConversationID(s.access), nil)
	mustStatus(t, detail, http.StatusOK, "")
	devices, _ := detail.body["diagnostics"].([]any)
	if len(devices) != 1 {
		t.Fatalf("diagnostics: %s", detail.raw)
	}
	device := devices[0].(map[string]any)
	diag, _ := device["diagnostics"].(map[string]any)
	if device["platform"] != domain.PlatformIOS || diag["reported"] != true || diag["keyboard_setup_status"] != "not_verified" ||
		diag["full_access_status"] != "observed_denied" || diag["onboarding_status"] != "in_progress" ||
		diag["notification_authorization_status"] == nil {
		t.Fatalf("device diagnostics: %v", device)
	}

	empty := h.supportAdmin(admin, http.MethodGet, "/conversations/"+h.supportConversationID(bare.access), nil)
	mustStatus(t, empty, http.StatusOK, "")
	if list, ok := empty.body["diagnostics"].([]any); !ok || len(list) != 0 {
		t.Fatalf("no devices: %s", empty.raw)
	}
}

// Шешілді / қайта ашу, жазу шешілген сөйлесуді ашады, тізім сүзгілері мен белгі.
func TestSupportResolveReopenAndList(t *testing.T) {
	h := newHarness(t)
	a := h.signIn("+7 701 400 00 07")
	b := h.signIn("support.list@example.com")
	h.mustSupportSend(a.access, "Бірінші сұрақ")
	h.clock.Advance(time.Minute)
	h.mustSupportSend(b.access, "Екінші сұрақ")
	convA, convB := h.supportConversationID(a.access), h.supportConversationID(b.access)
	admin := h.signInAdmin()

	summary := h.supportAdmin(admin, http.MethodGet, "/summary", nil)
	if summary.num("open") != 2 || summary.num("unread") != 2 {
		t.Fatalf("summary: %s", summary.raw)
	}
	list := h.supportAdmin(admin, http.MethodGet, "/conversations?status=open&unread=1", nil)
	if list.num("total") != 2 {
		t.Fatalf("open unread list: %s", list.raw)
	}
	rows := list.body["conversations"].([]any)
	if top := rows[0].(map[string]any); top["id"] != convB || top["preview"] != "Екінші сұрақ" ||
		top["last_sender"] != "user" || top["unread"] != float64(1) || !strings.Contains(top["identifier"].(string), "*") {
		t.Fatalf("newest first, masked: %v", top)
	}
	if found := h.supportAdmin(admin, http.MethodGet, "/conversations?q=support.list", nil); found.num("total") != 1 {
		t.Fatalf("search: %s", found.raw)
	}
	if found := h.supportAdmin(admin, http.MethodGet, "/conversations?q=%25", nil); found.num("total") != 0 {
		t.Fatalf("%% is matched literally: %s", found.raw)
	}
	mustStatus(t, h.supportAdmin(admin, http.MethodGet, "/conversations?status=closed", nil), http.StatusBadRequest, "INVALID_REQUEST")

	detail := h.supportAdmin(admin, http.MethodGet, "/conversations/"+convA, nil)
	mustStatus(t, detail, http.StatusOK, "")
	if detail.str("user", "id") != a.userID || detail.str("user", "plan_code") == "" || detail.str("conversation", "status") != "open" ||
		len(supportItems(detail)) != 1 || detail.body["has_more"] != false {
		t.Fatalf("detail: %s", detail.raw)
	}
	mustStatus(t, h.supportAdmin(admin, http.MethodPost, "/conversations/"+convA+"/read", map[string]any{}), http.StatusOK, "")
	if summary := h.supportAdmin(admin, http.MethodGet, "/summary", nil); summary.num("unread") != 1 {
		t.Fatalf("summary after reading: %s", summary.raw)
	}

	resolve := h.supportAdmin(admin, http.MethodPost, "/conversations/"+convA+"/resolve", map[string]any{})
	if resolve.status != http.StatusOK || resolve.body["changed"] != true {
		t.Fatalf("resolve: %s", resolve.raw)
	}
	if again := h.supportAdmin(admin, http.MethodPost, "/conversations/"+convA+"/resolve", map[string]any{}); again.body["changed"] != false {
		t.Fatalf("second resolve: %s", again.raw)
	}
	if conv := h.do(http.MethodGet, "/api/v1/support/conversation", nil, h.auth(a.access)); conv.str("conversation", "status") != "resolved" {
		t.Fatalf("the person sees: %s", conv.raw)
	}
	if resolved := h.supportAdmin(admin, http.MethodGet, "/conversations?status=resolved", nil); resolved.num("total") != 1 {
		t.Fatalf("resolved list: %s", resolved.raw)
	}

	// Writing to a resolved conversation reopens it.
	h.mustSupportSend(a.access, "Тағы да мәселе бар")
	if conv := h.do(http.MethodGet, "/api/v1/support/conversation", nil, h.auth(a.access)); conv.str("conversation", "status") != "open" {
		t.Fatalf("after writing: %s", conv.raw)
	}
	mustStatus(t, h.supportAdmin(admin, http.MethodPost, "/conversations/"+convA+"/resolve", map[string]any{}), http.StatusOK, "")
	reopen := h.supportAdmin(admin, http.MethodPost, "/conversations/"+convA+"/reopen", map[string]any{})
	if reopen.status != http.StatusOK || reopen.body["changed"] != true {
		t.Fatalf("reopen: %s", reopen.raw)
	}
	if all := h.supportAdmin(admin, http.MethodGet, "/conversations?status=all", nil); all.num("total") != 2 {
		t.Fatalf("all: %s", all.raw)
	}

	for action, want := range map[string]int{"support.resolve": 2, "support.reopen": 1, "support.reply": 0} {
		if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE action = ? AND entity_id = ?`, action, convA); n != want {
			t.Errorf("%s audited %d times, want %d", action, n, want)
		}
	}
}

// Аудитте тек ұзындық; қолданушы бетінде хабарлама мазмұны жоқ.
func TestSupportAuditIsContentFree(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 701 400 00 08")
	h.mustSupportSend(s.access, "Құпия сұрақ мәтіні")
	conversation := h.supportConversationID(s.access)
	admin := h.signInAdmin()
	reply := "Құпия жауап мәтіні"
	mustStatus(t, h.supportAdmin(admin, http.MethodPost, "/conversations/"+conversation+"/messages",
		map[string]any{"text": reply}), http.StatusCreated, "")

	meta := h.text(`SELECT metadata FROM admin_audit_logs WHERE action = 'support.reply' AND entity_type = 'support_conversation' AND entity_id = ?`, conversation)
	if meta != `{"length":18}` {
		t.Fatalf("audit metadata = %s", meta)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM admin_audit_logs WHERE metadata LIKE '%Құпия%'`); n != 0 {
		t.Fatal("the audit log holds message text")
	}
	if logs := h.logs.String(); strings.Contains(logs, "Құпия") {
		t.Fatal("the log holds message text")
	}
	detail := h.do(http.MethodGet, "/api/v1/admin/users/"+s.userID, nil, admin.headers(h.cfg.Admin.CookieName))
	mustStatus(t, detail, http.StatusOK, "")
	for _, word := range []string{"Құпия", "conversation", "message"} {
		if strings.Contains(strings.ToLower(string(detail.raw)), word) {
			t.Errorf("user detail contains %q", word)
		}
	}
}

func TestSupportWritesAreRateLimited(t *testing.T) {
	h := newHarness(t, withEnv("RATE_SUPPORT_PER_MINUTE", "2"))
	s := h.signIn("+7 701 400 00 09")
	other := h.signIn("+7 701 400 00 10")
	h.mustSupportSend(s.access, "1")
	h.mustSupportSend(s.access, "2")
	mustStatus(t, h.supportSend(s.access, "3", ""), http.StatusTooManyRequests, "RATE_LIMITED")
	// Reading is not limited, and the bucket is per person.
	mustStatus(t, h.supportMessages(s.access, ""), http.StatusOK, "")
	h.mustSupportSend(other.access, "1")
}

// Тіркелгі жойылса, сөйлесу де, барлық хабарлама да кетеді.
func TestAccountDeletionRemovesSupportChat(t *testing.T) {
	h := newHarness(t)
	s := h.signIn("+7 701 400 00 11")
	other := h.signIn("+7 701 400 00 12")
	h.mustSupportSend(s.access, "Сұрақ")
	h.mustSupportSend(other.access, "Басқа сұрақ")
	conversation := h.supportConversationID(s.access)
	admin := h.signInAdmin()
	mustStatus(t, h.supportAdmin(admin, http.MethodPost, "/conversations/"+conversation+"/messages",
		map[string]any{"text": "Жауап"}), http.StatusCreated, "")
	if n := h.scalar(`SELECT COUNT(*) FROM support_messages WHERE conversation_id = ?`, conversation); n != 2 {
		t.Fatalf("messages before deletion = %d", n)
	}

	mustStatus(t, h.do(http.MethodDelete, "/api/v1/me", nil, h.auth(s.access)), http.StatusOK, "")
	if n := h.scalar(`SELECT COUNT(*) FROM support_conversations WHERE user_id = ?`, s.userID); n != 0 {
		t.Fatalf("conversations left = %d", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM support_messages WHERE conversation_id = ?`, conversation); n != 0 {
		t.Fatalf("messages left = %d", n)
	}
	if n := h.scalar(`SELECT COUNT(*) FROM support_messages`); n != 1 {
		t.Fatalf("another account lost its messages: %d left", n)
	}
}

// ---------------------------------------------------------------- migration

// 0017 бар дерекқорға қолданылады: бар деректер сақталады, кестелер каскадпен жойылады.
func TestSupportMigrationKeepsExistingAccounts(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(database.Options{Path: filepath.Join(t.TempDir(), "old.db"), MaxReadConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	before := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") && e.Name() < "0017" {
			body, _ := fs.ReadFile(migrations.FS, e.Name())
			before[e.Name()] = &fstest.MapFile{Data: body}
		}
	}
	if _, err := database.Migrate(ctx, db, before); err != nil {
		t.Fatalf("old migrations: %v", err)
	}
	if _, err := db.Writer().ExecContext(ctx, `INSERT INTO users (id, email, status, locale, created_at, updated_at)
		VALUES ('u-1', 'old@example.com', 'active', 'ru', 1000, 1000)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	applied, err := database.Migrate(ctx, db, migrations.FS)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !strings.Contains(strings.Join(applied, ","), "0017_support_chat.sql") {
		t.Fatalf("applied = %v", applied)
	}
	var email string
	if err := db.Reader().QueryRow(`SELECT email FROM users WHERE id = 'u-1'`).Scan(&email); err != nil || email != "old@example.com" {
		t.Fatalf("user = %q (%v)", email, err)
	}
	for _, stmt := range []string{
		`INSERT INTO support_conversations (id, user_id, last_message_at, created_at, updated_at) VALUES ('c-1', 'u-1', 1, 1, 1)`,
		`INSERT INTO support_messages (id, conversation_id, sender, client_message_id, body, created_at) VALUES ('m-1', 'c-1', 'user', 'k', 'x', 1)`,
		`INSERT INTO support_messages (id, conversation_id, sender, body, created_at) VALUES ('m-2', 'c-1', 'operator', 'y', 2)`,
		`INSERT INTO support_messages (id, conversation_id, sender, body, created_at) VALUES ('m-3', 'c-1', 'operator', 'z', 3)`,
	} {
		if _, err := db.Writer().ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	var status string
	if err := db.Reader().QueryRow(`SELECT status FROM support_conversations WHERE id = 'c-1'`).Scan(&status); err != nil || status != "open" {
		t.Fatalf("default status = %q (%v)", status, err)
	}
	for _, stmt := range []string{
		`INSERT INTO support_conversations (id, user_id, last_message_at, created_at, updated_at) VALUES ('c-2', 'u-1', 1, 1, 1)`,
		`INSERT INTO support_messages (id, conversation_id, sender, client_message_id, body, created_at) VALUES ('m-4', 'c-1', 'user', 'k', 'x', 4)`,
	} {
		if _, err := db.Writer().ExecContext(ctx, stmt); err == nil {
			t.Fatalf("accepted a duplicate: %s", stmt)
		}
	}
	if _, err := db.Writer().ExecContext(ctx, `DELETE FROM users WHERE id = 'u-1'`); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := db.Reader().QueryRow(`SELECT (SELECT COUNT(*) FROM support_conversations) + (SELECT COUNT(*) FROM support_messages)`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("rows left after the account = %d (%v)", left, err)
	}
	if again, err := database.Migrate(ctx, db, migrations.FS); err != nil || len(again) != 0 {
		t.Fatalf("second run applied %v (%v)", again, err)
	}
}
