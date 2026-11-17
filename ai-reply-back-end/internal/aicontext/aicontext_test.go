package aicontext

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

func TestExtraFollowsTheOwnersRule(t *testing.T) {
	p := DefaultPricing() // 400 characters a reply, nothing free, at most +2
	for chars, want := range map[int]int{0: 0, 1: 1, 399: 1, 400: 1, 401: 2, 800: 2, 801: 2, 5000: 2} {
		if got := p.Extra(chars); got != want {
			t.Errorf("Extra(%d) = %d, want %d", chars, got, want)
		}
	}
	free := Pricing{CharsPerReply: 400, FreeChars: 300, MaxExtraReplies: 3}
	for chars, want := range map[int]int{300: 0, 301: 1, 700: 1, 701: 2, 1101: 3, 9000: 3} {
		if got := free.Extra(chars); got != want {
			t.Errorf("free Extra(%d) = %d, want %d", chars, got, want)
		}
	}
	if (Pricing{CharsPerReply: 0, MaxExtraReplies: 2}).Extra(5000) != 0 || (Pricing{CharsPerReply: 400}).Extra(5000) != 0 {
		t.Error("a zero rate or a zero cap means no surcharge")
	}
}

func TestPricingValidateRejectsOutOfRange(t *testing.T) {
	for _, p := range []Pricing{{CharsPerReply: -1}, {CharsPerReply: 4001}, {FreeChars: 10001}, {MaxExtraReplies: 11}} {
		var e *PricingError
		if err := p.Validate(); !errors.As(err, &e) || !errors.Is(err, domain.ErrInvalidRequest) {
			t.Errorf("%+v: %v", p, err)
		}
	}
	if err := DefaultPricing().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeTrimsDeduplicatesAndRefuses(t *testing.T) {
	doc, err := Normalize(Document{Enabled: true, Roles: []string{" seller", "blogger", "seller"}, Traits: nil,
		About: "  Строка 1\r\nСтрока 2\t ", Recipients: map[string]Notes{"client": {Notes: " На «Вы» "}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(doc.Roles, ",") != "seller,blogger" || doc.About != "Строка 1\nСтрока 2" ||
		doc.Recipients["client"].Notes != "На «Вы»" || len(doc.Recipients) != 4 || doc.Traits == nil {
		t.Fatalf("normalized %+v", doc)
	}
	cases := map[string]struct {
		doc    Document
		field  string
		reason string
	}{
		"long":      {Document{About: strings.Repeat("ы", MaxAbout+1)}, "about", ReasonTooLong},
		"many":      {Document{Roles: []string{"seller", "investor", "manager", "designer", "artist", "doctor"}}, "roles", ReasonTooMany},
		"unknown":   {Document{Traits: []string{"angry"}}, "traits", ReasonUnknown},
		"recipient": {Document{Recipients: map[string]Notes{"boss": {}}}, "recipients", ReasonUnknown},
		"control":   {Document{Audience: "a\x00b"}, "audience", ReasonInvalidText},
		"opposite":  {Document{Traits: []string{"formal", "casual"}}, "traits", ReasonConflict},
	}
	for name, tc := range cases {
		_, err := Normalize(tc.doc)
		var e *FieldError
		if !errors.As(err, &e) || e.Field != tc.field || e.Reason != tc.reason || !errors.Is(err, domain.ErrInvalidRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}
	pasted, err := Normalize(Document{About: "a\u2028b\u2029c\u0085d\ve\r\nf", Audience: "\u202eabc\u202c\u200e"})
	if err != nil || pasted.About != "a\nb\nc\nd\ne\nf" || pasted.Audience != "abc" {
		t.Fatalf("pasted %q %q %v", pasted.About, pasted.Audience, err)
	}
	// Exactly at the limit, counted in code points: 2000 emoji are 2000 characters.
	if _, err := Normalize(Document{About: strings.Repeat("😀", MaxAbout)}); err != nil {
		t.Fatal(err)
	}
}

func TestRecipientForMapsTheAppsIDs(t *testing.T) {
	for in, want := range map[string]string{"friend": "friend", "client": "client", "work": "colleague", "colleague": "colleague",
		"business": "partner", "Partner": "partner", "custom": "", "9d1c5e1a-uuid": "", "": ""} {
		if got := RecipientFor(in); got != want {
			t.Errorf("RecipientFor(%q) = %q, want %q", in, got, want)
		}
	}
	if RecipientFor("9d1c5e1a-uuid", "business") != "partner" {
		t.Error("the relationship decides when the id is a custom persona")
	}
}

func TestRenderAndCosts(t *testing.T) {
	doc := Document{Enabled: true, Roles: []string{"seller"}, Traits: []string{"brief"}, Occupation: "Флорист",
		About: strings.Repeat("я", 393), Recipients: completeRecipients(map[string]Notes{"client": {Notes: "На «Вы»"}})}
	general := doc.Render("")
	if !strings.Contains(general.Profile, "Roles: seller") || !strings.Contains(general.Profile, "Communication style: writes briefly") ||
		general.Notes != "" || general.Chars != 400 {
		t.Fatalf("general %+v", general)
	}
	client := doc.Render("client")
	if client.Recipient != "client" || client.Notes != "На «Вы»" || client.Chars != 407 {
		t.Fatalf("client %+v", client)
	}
	costs := DefaultPricing().Costs(doc, false)
	if costs.BaseChars != 400 || costs.General != 2 || costs.Client != 3 || costs.Friend != 2 {
		t.Fatalf("costs %+v", costs)
	}
	if c := DefaultPricing().Costs(doc, true); c.General != 1 || c.Client != 1 {
		t.Fatalf("unlimited %+v", c)
	}
	doc.Enabled = false
	if doc.Render("client").Present() || DefaultPricing().Costs(doc, false).Client != 1 {
		t.Fatal("a switched-off document adds nothing and costs nothing")
	}
	chipsOnly := Document{Enabled: true, Roles: []string{"investor"}}
	if r := chipsOnly.Render(""); !r.Present() || r.Chars != 0 || DefaultPricing().Costs(chipsOnly, false).General != 1 {
		t.Fatalf("chips are free: %+v", r)
	}
}

// memoryStore — Store in memory, with the repository's revision rule.
type memoryStore struct {
	raw      string
	revision int64
	settings map[string]string
	fail     error
}

func (m *memoryStore) AIContext(context.Context, string) (string, int64, time.Time, error) {
	if m.revision == 0 {
		return "", 0, time.Time{}, domain.ErrNotFound
	}
	return m.raw, m.revision, time.Unix(1, 0), nil
}

func (m *memoryStore) SaveAIContext(_ context.Context, _ string, raw string, expected int64, _ time.Time) (bool, int64, error) {
	if expected != m.revision {
		return false, m.revision, nil
	}
	m.raw, m.revision = raw, m.revision+1
	return true, m.revision, nil
}

func (m *memoryStore) Setting(_ context.Context, key string) (string, error) {
	if m.fail != nil {
		return "", m.fail
	}
	v, ok := m.settings[key]
	if !ok {
		return "", domain.ErrNotFound
	}
	return v, nil
}

func (m *memoryStore) SetSettings(_ context.Context, values map[string]string) error {
	for k, v := range values {
		m.settings[k] = v
	}
	return nil
}

func TestServiceRevisionsAndPricingCache(t *testing.T) {
	store := &memoryStore{settings: map[string]string{}}
	s := New(store).WithTTL(0)
	ctx := context.Background()
	if r, err := s.Get(ctx, "u"); err != nil || r.Revision != 0 || !r.Document.Enabled || len(r.Document.Recipients) != 4 {
		t.Fatalf("default %+v %v", r, err)
	}
	saved, err := s.Save(ctx, "u", Document{Enabled: true, About: "Привет"}, 0)
	if err != nil || saved.Revision != 1 {
		t.Fatalf("save %+v %v", saved, err)
	}
	var conflict *RevisionError
	if _, err := s.Save(ctx, "u", Document{Enabled: true}, 0); !errors.As(err, &conflict) || conflict.Current != 1 {
		t.Fatalf("stale save: %v", err)
	}
	if rendered, extra, err := s.ForRequest(ctx, "u", "client"); err != nil || rendered.Chars != 6 || extra != 1 {
		t.Fatalf("for request %+v %d %v", rendered, extra, err)
	}

	// A hand-edited or out-of-range row keeps the default.
	store.settings[SettingCharsPerReply] = "abc"
	store.settings[SettingMaxExtraReplies] = "99"
	store.settings[SettingFreeChars] = "100"
	if p := s.Pricing(ctx); p.CharsPerReply != 400 || p.MaxExtraReplies != 2 || p.FreeChars != 100 {
		t.Fatalf("pricing %+v", p)
	}
	if _, err := s.UpdatePricing(ctx, Pricing{CharsPerReply: 500, FreeChars: 0, MaxExtraReplies: 1}); err != nil {
		t.Fatal(err)
	}
	// A failing read keeps the last known values.
	store.fail = errors.New("database is locked")
	if p := s.Pricing(ctx); p.CharsPerReply != 500 || p.MaxExtraReplies != 1 {
		t.Fatalf("last known %+v", p)
	}
	if _, err := s.UpdatePricing(ctx, Pricing{CharsPerReply: 400, MaxExtraReplies: 50}); err == nil {
		t.Fatal("an out-of-range cap was stored")
	}
}
