// Package aicontext — the account's AI context: who the user is, how they
// write and how they talk to each kind of recipient.
//
// The website cabinet edits it; the server adds it to every reply and compose
// request, whatever app sends them, so the apps need no update to use it. The
// contract (fields, limits, pricing, API) is docs/web-cabinet-ai-context.md.
//
// PRIVACY: the document is text the user typed about themselves. It is never
// logged; it reaches the AI provider only inside a generation, as data.
package aicontext

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Limits of the document, in Unicode code points (what a person counts).
const (
	MaxRoles      = 5
	MaxTraits     = 6
	MaxOccupation = 120
	MaxAbout      = 2000
	MaxAudience   = 300
	MaxNotes      = 1000
)

// Built-in recipients, in display order. Request template ids map onto them
// with RecipientFor.
const (
	RecipientFriend    = "friend"
	RecipientClient    = "client"
	RecipientColleague = "colleague"
	RecipientPartner   = "partner"
)

// Recipients — every recipient key of the document.
var Recipients = []string{RecipientFriend, RecipientClient, RecipientColleague, RecipientPartner}

// role is a role chip: its code and how the prompt names it. The prompt text is
// fixed server text in English; the user-facing labels live in the locales.
type label struct {
	Code   string
	Prompt string
}

var roles = []label{
	{"seller", "seller"},
	{"business_owner", "business owner"},
	{"entrepreneur", "entrepreneur"},
	{"investor", "investor"},
	{"partner", "business partner"},
	{"manager", "manager"},
	{"employee", "employee"},
	{"freelancer", "freelancer"},
	{"designer", "designer"},
	{"artist", "artist"},
	{"developer", "software developer"},
	{"marketer", "marketer"},
	{"blogger", "blogger"},
	{"influencer", "influencer"},
	{"student", "student"},
	{"teacher", "teacher"},
	{"doctor", "doctor"},
	{"lawyer", "lawyer"},
	{"realtor", "realtor"},
}

var traits = []label{
	{"introvert", "introvert: calm, low-key messages"},
	{"extrovert", "extrovert: open, energetic messages"},
	{"brief", "writes briefly"},
	{"detailed", "writes in more detail"},
	{"formal", "formal, polite form of address"},
	{"casual", "casual, informal"},
	{"humor", "light humour when it fits"},
	{"emoji", "likes emoji"},
	{"no_emoji", "avoids emoji"},
	{"diplomatic", "diplomatic"},
	{"direct", "direct and to the point"},
	{"warm", "warm and caring"},
}

// RoleCodes and TraitCodes — the catalog, in display order.
func RoleCodes() []string  { return codes(roles) }
func TraitCodes() []string { return codes(traits) }

func codes(list []label) []string {
	out := make([]string, len(list))
	for i, l := range list {
		out[i] = l.Code
	}
	return out
}

func promptLabel(list []label, code string) (string, bool) {
	for _, l := range list {
		if l.Code == code {
			return l.Prompt, true
		}
	}
	return "", false
}

// Notes — what the user saved for one recipient.
type Notes struct {
	Notes string `json:"notes"`
}

// Document — the account's AI context.
type Document struct {
	Enabled    bool             `json:"enabled"`
	Roles      []string         `json:"roles"`
	Occupation string           `json:"occupation"`
	Traits     []string         `json:"traits"`
	About      string           `json:"about"`
	Audience   string           `json:"audience"`
	Recipients map[string]Notes `json:"recipients"`
}

// Default — the document of an account that never saved one.
func Default() Document {
	return Document{Enabled: true, Roles: []string{}, Traits: []string{}, Recipients: completeRecipients(nil)}
}

// completeRecipients — every recipient key present, nothing else.
func completeRecipients(in map[string]Notes) map[string]Notes {
	out := make(map[string]Notes, len(Recipients))
	for _, key := range Recipients {
		out[key] = Notes{Notes: in[key].Notes}
	}
	return out
}

// Complete — the shape every response carries: non-nil lists and all four
// recipients. Used on documents read back from the database.
func (d Document) Complete() Document {
	if d.Roles == nil {
		d.Roles = []string{}
	}
	if d.Traits == nil {
		d.Traits = []string{}
	}
	d.Recipients = completeRecipients(d.Recipients)
	return d
}

// FieldError — a rejected field, with the limit when there is one. It unwraps
// to domain.ErrInvalidRequest in the transport layer (see errors.go).
type FieldError struct {
	Field  string
	Reason string
	Max    int
}

func (e *FieldError) Error() string { return "invalid " + e.Field + ": " + e.Reason }

// Unwrap — the transport layer answers 400 INVALID_REQUEST with Details.
func (e *FieldError) Unwrap() error { return domain.ErrInvalidRequest }

// Details — what the client shows next to the field.
func (e *FieldError) Details() map[string]any {
	d := map[string]any{"field": e.Field, "reason": e.Reason}
	if e.Max > 0 {
		d["max"] = e.Max
	}
	return d
}

// Validation reasons (details.reason).
const (
	ReasonTooLong     = "too_long"
	ReasonTooMany     = "too_many"
	ReasonUnknown     = "unknown"
	ReasonInvalidText = "invalid_text"
)

// Normalize — the document as it is stored: trimmed text, no carriage returns,
// codes de-duplicated in their order, all four recipients. Anything over a
// limit, an unknown code or a control character is rejected, never cut: the
// user sees what was refused instead of losing part of their text.
func Normalize(in Document) (Document, error) {
	out := Document{Enabled: in.Enabled}
	var err error
	if out.Roles, err = normalizeCodes("roles", in.Roles, roles, MaxRoles); err != nil {
		return Document{}, err
	}
	if out.Traits, err = normalizeCodes("traits", in.Traits, traits, MaxTraits); err != nil {
		return Document{}, err
	}
	if out.Occupation, err = cleanText("occupation", in.Occupation, MaxOccupation); err != nil {
		return Document{}, err
	}
	if out.About, err = cleanText("about", in.About, MaxAbout); err != nil {
		return Document{}, err
	}
	if out.Audience, err = cleanText("audience", in.Audience, MaxAudience); err != nil {
		return Document{}, err
	}
	out.Recipients = make(map[string]Notes, len(Recipients))
	for key := range in.Recipients {
		if !isRecipient(key) {
			return Document{}, &FieldError{Field: "recipients", Reason: ReasonUnknown}
		}
	}
	for _, key := range Recipients {
		notes, err := cleanText("recipients."+key+".notes", in.Recipients[key].Notes, MaxNotes)
		if err != nil {
			return Document{}, err
		}
		out.Recipients[key] = Notes{Notes: notes}
	}
	return out, nil
}

func normalizeCodes(field string, in []string, catalog []label, max int) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, code := range in {
		code = strings.TrimSpace(code)
		if _, ok := promptLabel(catalog, code); !ok {
			return nil, &FieldError{Field: field, Reason: ReasonUnknown}
		}
		duplicate := false
		for _, seen := range out {
			duplicate = duplicate || seen == code
		}
		if !duplicate {
			out = append(out, code)
		}
	}
	if len(out) > max {
		return nil, &FieldError{Field: field, Reason: ReasonTooMany, Max: max}
	}
	return out, nil
}

// cleanText — trimmed text without "\r"; other control characters except "\n"
// and "\t" are refused (they only ever come from a broken client or an attempt
// to smuggle formatting into the prompt).
func cleanText(field, value string, max int) (string, error) {
	if !utf8.ValidString(value) {
		return "", &FieldError{Field: field, Reason: ReasonInvalidText}
	}
	value = strings.ReplaceAll(value, "\r", "")
	for _, r := range value {
		if r != '\n' && r != '\t' && (unicode.IsControl(r) || r == ' ' || r == ' ') {
			return "", &FieldError{Field: field, Reason: ReasonInvalidText}
		}
	}
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > max {
		return "", &FieldError{Field: field, Reason: ReasonTooLong, Max: max}
	}
	return value, nil
}

func isRecipient(key string) bool {
	for _, r := range Recipients {
		if r == key {
			return true
		}
	}
	return false
}

// RecipientFor — the recipient of a request, from the apps' template ids and
// relationships: friend, client, work/colleague, business/partner. Anything
// else (a custom persona, no template) has no recipient notes: "".
func RecipientFor(values ...string) string {
	for _, v := range values {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "friend":
			return RecipientFriend
		case "client":
			return RecipientClient
		case "work", "colleague":
			return RecipientColleague
		case "business", "partner":
			return RecipientPartner
		}
	}
	return ""
}

// BaseChars — the characters of the general part that count for pricing:
// occupation, about and audience. Roles and traits are free.
func (d Document) BaseChars() int {
	return utf8.RuneCountInString(d.Occupation) + utf8.RuneCountInString(d.About) +
		utf8.RuneCountInString(d.Audience)
}

// Chars — what a request for recipient r carries ("" — no recipient notes).
func (d Document) Chars(recipient string) int {
	return d.BaseChars() + utf8.RuneCountInString(d.Recipients[recipient].Notes)
}

// Rendered — the context of one request, ready for the prompt. Empty when the
// document is switched off or has nothing to say for this request.
type Rendered struct {
	// Profile — the general part, one "Label: value" line each.
	Profile string
	// Recipient and Notes — the notes the user saved for this recipient.
	Recipient string
	Notes     string
	// Chars — the characters that count for pricing (Document.Chars).
	Chars int
}

// Present — something will be added to the prompt.
func (r Rendered) Present() bool { return r.Profile != "" || r.Notes != "" }

// Render — what a request for recipient r adds to the prompt. Labels are fixed
// server text; the values are the user's own words and stay data.
func (d Document) Render(recipient string) Rendered {
	if !d.Enabled {
		return Rendered{}
	}
	var lines []string
	if names := promptLabels(roles, d.Roles); names != "" {
		lines = append(lines, "Roles: "+names)
	}
	if d.Occupation != "" {
		lines = append(lines, "Occupation: "+d.Occupation)
	}
	if style := promptLabels(traits, d.Traits); style != "" {
		lines = append(lines, "Communication style: "+style)
	}
	if d.About != "" {
		lines = append(lines, "About: "+d.About)
	}
	if d.Audience != "" {
		lines = append(lines, "Audience: "+d.Audience)
	}
	out := Rendered{Profile: strings.Join(lines, "\n")}
	if recipient != "" {
		if notes := d.Recipients[recipient].Notes; notes != "" {
			out.Recipient, out.Notes = recipient, notes
		}
	}
	if out.Present() {
		out.Chars = d.BaseChars() + utf8.RuneCountInString(out.Notes)
	}
	return out
}

func promptLabels(list []label, selected []string) string {
	var names []string
	for _, code := range selected {
		if name, ok := promptLabel(list, code); ok {
			names = append(names, name)
		}
	}
	return strings.Join(names, "; ")
}
