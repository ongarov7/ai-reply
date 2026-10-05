package apptest

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// Жіберуші жынысы мен онбординг нұсқасы /me арқылы сақталады.
func TestProfileStoresGenderAndOnboardingVersion(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 707 600 10 01")

	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(session.access))
	if me.str("profile", "grammatical_gender") != "unspecified" || me.num("profile", "onboarding_version") != 0 {
		t.Fatalf("defaults: %s", me.raw)
	}

	patched := h.do(http.MethodPatch, "/api/v1/me",
		map[string]any{"grammatical_gender": "female", "onboarding_version": 2}, h.auth(session.access))
	if patched.status != http.StatusOK || patched.str("grammatical_gender") != "female" || patched.num("onboarding_version") != 2 {
		t.Fatalf("PATCH /me: %d %s", patched.status, patched.raw)
	}

	// An older device cannot move the version back; the Android POST alias works the same.
	older := h.do(http.MethodPost, "/api/v1/me", map[string]any{"onboarding_version": 1}, h.auth(session.access))
	if older.status != http.StatusOK || older.num("onboarding_version") != 2 {
		t.Fatalf("version went back: %d %s", older.status, older.raw)
	}

	// A body from an old client leaves both fields alone.
	oldClient := h.do(http.MethodPatch, "/api/v1/me", map[string]any{"role": "сатушы"}, h.auth(session.access))
	if oldClient.str("grammatical_gender") != "female" || oldClient.num("onboarding_version") != 2 || oldClient.str("role") != "сатушы" {
		t.Fatalf("old body changed the new fields: %s", oldClient.raw)
	}

	normalised := h.do(http.MethodPatch, "/api/v1/me", map[string]any{"grammatical_gender": " MALE "}, h.auth(session.access))
	if normalised.str("grammatical_gender") != "male" {
		t.Fatalf("gender not normalised: %s", normalised.raw)
	}
	me = h.do(http.MethodGet, "/api/v1/me", nil, h.auth(session.access))
	if me.str("profile", "grammatical_gender") != "male" || me.num("profile", "onboarding_version") != 2 {
		t.Fatalf("round trip: %s", me.raw)
	}

	// The sign-in session carries the same profile shape.
	h.do(http.MethodPost, "/api/v1/auth/request-otp", map[string]any{"identifier": "+7 707 600 10 01"}, nil)
	verify := h.do(http.MethodPost, "/api/v1/auth/verify-otp", map[string]any{
		"identifier": "+7 707 600 10 01", "code": "1111",
		"device": map[string]any{"platform": "ios", "app_version": "1.0.0", "locale": "kk"},
	}, nil)
	if verify.status != http.StatusOK || verify.str("profile", "grammatical_gender") != "male" ||
		verify.num("profile", "onboarding_version") != 2 {
		t.Fatalf("session profile: %d %s", verify.status, verify.raw)
	}
}

func TestProfileRejectsInvalidGenderAndVersion(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 707 600 10 02")
	h.do(http.MethodPatch, "/api/v1/me", map[string]any{"grammatical_gender": "female", "onboarding_version": 3}, h.auth(session.access))

	for _, body := range []map[string]any{
		{"grammatical_gender": "robot"},
		{"grammatical_gender": ""},
		{"onboarding_version": -1},
		{"onboarding_version": 1001},
		{"onboarding_version": 1.5},
		{"onboarding_version": "4"},
		{"gender": "male"},
	} {
		res := h.do(http.MethodPatch, "/api/v1/me", body, h.auth(session.access))
		if res.status != http.StatusBadRequest || res.errorCode() != "INVALID_REQUEST" {
			t.Fatalf("%v: %d %s, want 400 INVALID_REQUEST", body, res.status, res.raw)
		}
	}
	me := h.do(http.MethodGet, "/api/v1/me", nil, h.auth(session.access))
	if me.str("profile", "grammatical_gender") != "female" || me.num("profile", "onboarding_version") != 3 {
		t.Fatalf("a rejected update changed the profile: %s", me.raw)
	}
}

func TestConfigAnnouncesSenderProfileAndPolish(t *testing.T) {
	h := newHarness(t)
	features, _ := h.do(http.MethodGet, "/api/v1/config", nil, nil).body["features"].(map[string]any)
	if features["sender_profile"] != true || features["instruction_polish"] != true || features["reply_preferences"] != true {
		t.Fatalf("features = %v", features)
	}
}

// reply жауабы: сұраныстағы жыныс → сақталған жыныс → бейтарап.
func TestReplyGenderReachesTheDeveloperPrompt(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 707 600 10 03")
	reply := func(profile map[string]any) {
		t.Helper()
		body := map[string]any{"source_text": "Спасибо за помощь!", "template_id": "friend", "language": "ru"}
		if profile != nil {
			body["profile"] = profile
		}
		if res := h.do(http.MethodPost, "/api/v1/ai/reply", body, h.auth(session.access)); res.status != http.StatusOK {
			t.Fatalf("reply: %d %s", res.status, res.raw)
		}
	}

	reply(nil)
	if !strings.Contains(h.provider.lastDeveloper, "The sender's gender is unknown.") {
		t.Fatal("a profile without gender must get the neutral section")
	}

	h.do(http.MethodPatch, "/api/v1/me", map[string]any{"grammatical_gender": "female"}, h.auth(session.access))
	reply(nil)
	if !strings.Contains(h.provider.lastDeveloper, "is a woman") {
		t.Fatal("the stored gender was not used")
	}

	reply(map[string]any{"grammatical_gender": "male"})
	if !strings.Contains(h.provider.lastDeveloper, "is a man") || strings.Contains(h.provider.lastDeveloper, "is a woman") {
		t.Fatal("the request override did not win")
	}
	for _, leak := range []string{"is a man", "male", "grammatical"} {
		if strings.Contains(h.provider.lastUser, leak) {
			t.Fatalf("%q leaked into the user message", leak)
		}
	}

	reply(map[string]any{"grammatical_gender": "robot"})
	if !strings.Contains(h.provider.lastDeveloper, "is a woman") {
		t.Fatal("an invalid override must be ignored, not trusted")
	}
}

func TestComposeUsesTheStoredGenderAndItsOverride(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 707 600 10 04")
	h.do(http.MethodPatch, "/api/v1/me", map[string]any{"grammatical_gender": "male"}, h.auth(session.access))

	if res := h.compose(session.access, map[string]any{"instruction": composeInstruction}); res.status != http.StatusOK {
		t.Fatalf("compose: %d %s", res.status, res.raw)
	}
	if !strings.Contains(h.provider.lastDeveloper, "is a man") {
		t.Fatal("compose did not load the stored gender")
	}

	res := h.compose(session.access, map[string]any{"instruction": composeInstruction,
		"profile": map[string]any{"grammatical_gender": "female"}, "input_language": "ru"})
	if res.status != http.StatusOK || !strings.Contains(h.provider.lastDeveloper, "is a woman") {
		t.Fatalf("compose override: %d %s", res.status, res.raw)
	}

	// Compose's profile holds the gender and nothing else.
	extra := h.compose(session.access, map[string]any{"instruction": composeInstruction,
		"profile": map[string]any{"grammatical_gender": "female", "description": "Парфюм сатамын"}})
	if extra.status != http.StatusBadRequest {
		t.Fatalf("unknown profile field accepted: %d", extra.status)
	}
}

// input_language — тіл анық болмаса шешеді; белгісіз мән 400 емес, елеусіз.
func TestInputLanguageBreaksTheTie(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 707 600 10 05")
	send := func(input string) response {
		return h.do(http.MethodPost, "/api/v1/ai/reply", map[string]any{
			"source_text": "👍", "template_id": "friend", "language": "en", "input_language": input,
		}, h.auth(session.access))
	}
	if res := send("kk"); res.status != http.StatusOK || !strings.Contains(h.provider.lastDeveloper, "write in Kazakh.") {
		t.Fatalf("keyboard language ignored: %d %s", res.status, res.raw)
	}
	if res := send("de"); res.status != http.StatusOK || !strings.Contains(h.provider.lastDeveloper, "write in English.") {
		t.Fatalf("an unknown keyboard code must fall back to the app language: %d %s", res.status, res.raw)
	}

	compose := h.compose(session.access, map[string]any{"instruction": "Айгерим 🎉", "input_language": "ru", "language": "kk"})
	if compose.status != http.StatusOK || !strings.Contains(h.provider.lastDeveloper, "write in Russian.") {
		t.Fatalf("compose keyboard language: %d %s", compose.status, compose.raw)
	}
}

// Жаңа өрістер қосылды, бірақ белгісіз кілттер бұрынғыдай 400.
func TestUnknownFieldsAreStillRejected(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 707 600 10 06")
	for _, c := range []struct {
		path string
		body map[string]any
	}{
		{"/api/v1/ai/reply", map[string]any{"source_text": "Привет", "gender": "male"}},
		{"/api/v1/ai/reply", map[string]any{"source_text": "Привет", "profile": map[string]any{"gender": "male"}}},
		{"/api/v1/ai/compose", map[string]any{"instruction": "Привет", "grammatical_gender": "male"}},
		{"/api/v1/ai/polish", map[string]any{"text": "привет как дела", "language": "ru"}},
	} {
		if res := h.do(http.MethodPost, c.path, c.body, h.auth(session.access)); res.status != http.StatusBadRequest {
			t.Fatalf("%s %v: %d, want 400", c.path, c.body, res.status)
		}
	}
	if h.provider.calls != 0 {
		t.Fatalf("rejected requests reached the provider: %d", h.provider.calls)
	}
}

// prompt_version оқиғаға жазылады, бірақ әкімші JSON-ында жоқ.
func TestPromptVersionIsRecordedButNotShownToAdmins(t *testing.T) {
	h := newHarness(t)
	session := h.signIn("+7 707 600 10 07")
	h.do(http.MethodPatch, "/api/v1/me", map[string]any{"grammatical_gender": "female"}, h.auth(session.access))
	h.generate(session.access, "Сәлеметсіз бе, бағасы қанша?")
	h.compose(session.access, map[string]any{"instruction": "Әріптесімді туған күнімен құттықта"})

	events, err := h.store.UserEvents(context.Background(), session.userID, 10)
	if err != nil || len(events) != 2 {
		t.Fatalf("events: %v %d", err, len(events))
	}
	versions := map[string]string{events[0].Mode: events[0].PromptVersion, events[1].Mode: events[1].PromptVersion}
	if versions["reply"] != "reply_v2" || versions["compose"] != "compose_v2" {
		t.Fatalf("versions = %v", versions)
	}

	admin := h.signInAdmin()
	detail := h.do(http.MethodGet, "/api/v1/admin/users/"+session.userID, nil, admin.headers(h.cfg.Admin.CookieName))
	if detail.status != http.StatusOK {
		t.Fatalf("user detail: %d", detail.status)
	}
	for _, forbidden := range []string{"prompt", "_v2", "grammatical", "female"} {
		if strings.Contains(string(detail.raw), forbidden) {
			t.Fatalf("admin payload exposes %q", forbidden)
		}
	}
	for _, leak := range []string{"female", "grammatical"} {
		if strings.Contains(h.logs.String(), leak) {
			t.Fatalf("logs contain %q", leak)
		}
	}
	if !strings.Contains(h.logs.String(), `"prompt_version":"reply_v2"`) {
		t.Fatal("the success log has no prompt version")
	}
}

// Түзету: бірінші жауап тексеруден өтпейді, екіншісі өтеді; квота бір рет.
func TestReplyRepairFixesTheSenderGender(t *testing.T) {
	h := newHarness(t, withEnv("AI_REPAIR_ENABLED", "true"))
	session := h.signIn("+7 707 600 10 08")
	h.do(http.MethodPatch, "/api/v1/me", map[string]any{"grammatical_gender": "male"}, h.auth(session.access))
	h.provider.replies = []string{"Ответ: Пожалуйста! Рада была помочь.", "Пожалуйста! Рад был помочь."}

	res := h.do(http.MethodPost, "/api/v1/ai/reply", map[string]any{
		"source_text": "Спасибо за помощь!", "template_id": "friend", "language": "ru",
	}, h.auth(session.access))
	if res.status != http.StatusOK || res.str("reply") != "Пожалуйста! Рад был помочь." {
		t.Fatalf("reply: %d %s", res.status, res.raw)
	}
	if h.provider.calls != 2 || res.num("usage", "used_today") != 1 {
		t.Fatalf("calls %d, usage %s", h.provider.calls, res.raw)
	}
	if repair := h.provider.prompts[1]; repair.Version != "repair_v1" || !strings.Contains(repair.User, "Рада была помочь") {
		t.Fatalf("repair prompt: %+v", repair)
	}

	events, _ := h.store.UserEvents(context.Background(), session.userID, 10)
	if len(events) != 1 || events[0].PromptVersion != "reply_v2+repair_v1" || events[0].TotalTokens != 320 {
		t.Fatalf("one event with both calls' tokens: %+v", events)
	}
	if usage := h.do(http.MethodGet, "/api/v1/me/usage", nil, h.auth(session.access)); usage.num("used_today") != 1 {
		t.Fatalf("the repair was charged: %s", usage.raw)
	}
	logs := h.logs.String()
	if !strings.Contains(logs, `"msg":"ai_reply_repaired"`) || !strings.Contains(logs, `"issues":["gender"]`) {
		t.Fatal("the repair is not logged with its issue codes")
	}
	for _, text := range []string{"Рада была помочь", "Рад был помочь"} {
		if strings.Contains(logs, text) || h.dbContains(text) {
			t.Fatalf("%q was written somewhere", text)
		}
	}
}

func TestReplyRepairThatStillFailsKeepsTheOriginal(t *testing.T) {
	h := newHarness(t, withEnv("AI_REPAIR_ENABLED", "true"))
	session := h.signIn("+7 707 600 10 09")
	h.do(http.MethodPatch, "/api/v1/me", map[string]any{"grammatical_gender": "male"}, h.auth(session.access))
	h.provider.replies = []string{"Пожалуйста! Рада была помочь.", "Всегда рада помочь!"}

	res := h.do(http.MethodPost, "/api/v1/ai/reply", map[string]any{
		"source_text": "Спасибо за помощь!", "template_id": "friend",
	}, h.auth(session.access))
	if res.status != http.StatusOK || res.str("reply") != "Пожалуйста! Рада была помочь." {
		t.Fatalf("reply: %d %s", res.status, res.raw)
	}
	events, _ := h.store.UserEvents(context.Background(), session.userID, 10)
	if h.provider.calls != 2 || len(events) != 1 || events[0].PromptVersion != "reply_v2" || res.num("usage", "used_today") != 1 {
		t.Fatalf("calls %d, events %+v", h.provider.calls, events)
	}
}

func TestComposeRepairFixesTheLanguage(t *testing.T) {
	h := newHarness(t, withEnv("AI_REPAIR_ENABLED", "true"))
	session := h.signIn("+7 707 600 10 10")
	h.provider.replies = []string{"Туған күніңмен құттықтаймын! Бақыт пен денсаулық тілеймін 🎉",
		"Поздравляю с днём рождения! Счастья и здоровья 🎉"}

	res := h.compose(session.access, map[string]any{"instruction": "Поздравь коллегу с днём рождения"})
	if res.status != http.StatusOK || res.str("text") != "Поздравляю с днём рождения! Счастья и здоровья 🎉" {
		t.Fatalf("compose: %d %s", res.status, res.raw)
	}
	if !strings.Contains(h.provider.lastDeveloper, "The message must be in Russian") {
		t.Fatal("the repair did not name the language problem")
	}

	// A request that names a language may be answered in it: no language repair.
	h.provider.replies = []string{"Туған күніңмен құттықтаймын! 🎉"}
	named := h.compose(session.access, map[string]any{"instruction": "Поздравь коллегу с днём рождения на казахском"})
	if named.status != http.StatusOK || h.provider.calls != 3 {
		t.Fatalf("named language: %d, calls %d", named.status, h.provider.calls)
	}
}
