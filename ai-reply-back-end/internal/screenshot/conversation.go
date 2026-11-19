package screenshot

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Roles a recognised message can have. The app decides them from the layout
// (bubble side and colour) and lets the user correct them; "unknown" is an
// honest "could not tell", never a guess dressed up as a fact.
const (
	RoleSelf    = "self"
	RoleOther   = "other"
	RoleUnknown = "unknown"
)

// Modes of a screenshot reply.
const (
	// ModeText — the app recognised the text on the iPhone; only the text
	// reaches the server and the AI provider.
	ModeText = "text"
	// ModeImage — the screenshot itself is analysed by the AI provider, after
	// the person agreed to send it.
	ModeImage = "image"
)

// Limits of one conversation. A phone screen holds 10–25 bubbles; a cropped
// tall screenshot a few more.
const (
	MaxMessages      = 60
	MaxMessageChars  = 1500
	MaxQuoteChars    = 500
	MaxTotalChars    = 8000
	MaxTitleChars    = 100
	MaxSenderChars   = 60
	MaxTimeChars     = 20
	MaxMessageIDSize = 40
)

// Apps the iPhone can name from the screenshot's look. Anything else is "".
var knownApps = map[string]string{
	"whatsapp": "WhatsApp", "telegram": "Telegram", "imessage": "Messages", "instagram": "Instagram",
	"messenger": "Messenger", "viber": "Viber", "other": "",
}

// Message — one bubble as the app read it.
type Message struct {
	ID     string `json:"id"`
	Role   string `json:"role"`
	Sender string `json:"sender,omitempty"`
	Text   string `json:"text"`
	// Quote — the message this one replies to, when the bubble shows one.
	Quote string `json:"quote,omitempty"`
	Time  string `json:"time,omitempty"`
}

// Conversation — the visible part of a chat, oldest message first.
type Conversation struct {
	App      string    `json:"app,omitempty"`
	Title    string    `json:"title,omitempty"`
	Messages []Message `json:"messages"`
	// TargetID — the message the person chose to answer; "" — the latest
	// message from the other side.
	TargetID string `json:"target_id,omitempty"`
}

var messageID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

// Normalize — trims and checks everything the person's phone sent. required:
// a text-mode reply needs at least one message; an image reply may have none.
// The errors name the field (and the limit), never the text.
func (c Conversation) Normalize(required bool) (Conversation, error) {
	out := Conversation{Title: oneLine(c.Title, MaxTitleChars), TargetID: strings.TrimSpace(c.TargetID)}
	if _, ok := knownApps[strings.ToLower(strings.TrimSpace(c.App))]; ok {
		out.App = strings.ToLower(strings.TrimSpace(c.App))
	}
	if len(c.Messages) > MaxMessages {
		return Conversation{}, &LimitError{Field: "conversation.messages", Max: MaxMessages}
	}
	seen := map[string]bool{}
	total := 0
	for i, m := range c.Messages {
		text := strings.TrimSpace(strings.ReplaceAll(m.Text, "\r\n", "\n"))
		if text == "" {
			continue
		}
		if utf8.RuneCountInString(text) > MaxMessageChars {
			return Conversation{}, &LimitError{Field: "conversation.messages.text", Max: MaxMessageChars}
		}
		id := strings.TrimSpace(m.ID)
		if id == "" {
			id = "m" + strconv.Itoa(i+1)
		}
		if !messageID.MatchString(id) || seen[id] {
			return Conversation{}, domain.InvalidField("conversation.messages.id", "format")
		}
		seen[id] = true
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role != RoleSelf && role != RoleOther {
			role = RoleUnknown
		}
		quote := strings.TrimSpace(m.Quote)
		if utf8.RuneCountInString(quote) > MaxQuoteChars {
			quote = string([]rune(quote)[:MaxQuoteChars])
		}
		total += utf8.RuneCountInString(text) + utf8.RuneCountInString(quote)
		if total > MaxTotalChars {
			return Conversation{}, &LimitError{Field: "conversation", Max: MaxTotalChars}
		}
		out.Messages = append(out.Messages, Message{
			ID: id, Role: role, Text: text, Quote: quote,
			Sender: oneLine(m.Sender, MaxSenderChars), Time: oneLine(m.Time, MaxTimeChars),
		})
	}
	if required && len(out.Messages) == 0 {
		return Conversation{}, domain.InvalidField("conversation.messages", "empty")
	}
	if out.TargetID != "" && !seen[out.TargetID] {
		// A target the conversation no longer has (the person edited it away)
		// falls back to the latest message from the other side.
		out.TargetID = ""
	}
	return out, nil
}

// LimitError — a part of the conversation is longer than allowed.
type LimitError struct {
	Field string
	Max   int
}

func (e *LimitError) Error() string { return e.Field + " is too long" }

// Unwrap — 400 INVALID_REQUEST.
func (e *LimitError) Unwrap() error { return domain.ErrInvalidRequest }

// Details — the field and its limit.
func (e *LimitError) Details() map[string]any {
	return map[string]any{"field": e.Field, "reason": "too_long", "max": e.Max}
}

// Target — the message to answer: the chosen one, else the latest message from
// the other side, else the latest message whose sender is unknown, else none.
// It never picks the person's own message unless they chose it.
func (c Conversation) Target() (Message, int, bool) {
	if c.TargetID != "" {
		for i, m := range c.Messages {
			if m.ID == c.TargetID {
				return m, i, true
			}
		}
	}
	for _, role := range []string{RoleOther, RoleUnknown} {
		for i := len(c.Messages) - 1; i >= 0; i-- {
			if c.Messages[i].Role == role {
				return c.Messages[i], i, true
			}
		}
	}
	return Message{}, -1, false
}

// LanguageSample — the text the reply language is detected from: what the
// other side wrote (that is what the reply answers), else everything.
func (c Conversation) LanguageSample() string {
	var other, all []string
	for _, m := range c.Messages {
		all = append(all, m.Text)
		if m.Role != RoleSelf {
			other = append(other, m.Text)
		}
	}
	if target, _, ok := c.Target(); ok {
		other = append(other, target.Text)
	}
	if len(other) > 0 {
		return strings.Join(other, "\n")
	}
	return strings.Join(all, "\n")
}

// Chars — characters of all message texts and quotes (usage metadata only).
func (c Conversation) Chars() int {
	n := 0
	for _, m := range c.Messages {
		n += utf8.RuneCountInString(m.Text) + utf8.RuneCountInString(m.Quote)
	}
	return n
}

// AppName — a readable app name, or "".
func (c Conversation) AppName() string { return knownApps[c.App] }

// Render — the conversation as data for the prompt: one numbered line per
// message with who wrote it, the target marked. User text is neutralised so it
// cannot open or close a prompt block.
func (c Conversation) Render() string {
	_, index, hasTarget := c.Target()
	var b strings.Builder
	for i, m := range c.Messages {
		b.WriteString("[")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString("] ")
		b.WriteString(roleLabel(m.Role))
		if m.Sender != "" && m.Role != RoleSelf {
			b.WriteString(" (")
			b.WriteString(Neutralize(m.Sender))
			b.WriteString(")")
		}
		if m.Time != "" {
			b.WriteString(", ")
			b.WriteString(Neutralize(m.Time))
		}
		if hasTarget && i == index {
			b.WriteString(" ← REPLY TO THIS")
		}
		b.WriteString(":")
		if m.Quote != "" {
			b.WriteString("\n    (quoting an earlier message: «")
			b.WriteString(Neutralize(m.Quote))
			b.WriteString("»)")
		}
		for _, line := range strings.Split(Neutralize(m.Text), "\n") {
			b.WriteString("\n    ")
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func roleLabel(role string) string {
	switch role {
	case RoleSelf:
		return "Me"
	case RoleOther:
		return "Them"
	}
	return "Unknown sender"
}

// promptTag — a tag that could open or close one of the prompt's blocks.
var promptTag = regexp.MustCompile(`(?i)<\s*/?\s*(conversation|incoming_message|user_instruction|target_message|screenshot_notes|account_context|recipient_notes|template_instructions|user_profile|business_context|user_rules|working_hours|message)\s*>`)

// Neutralize — angle brackets of a prompt-block tag become look-alike
// characters; every other character stays exactly as the person sees it.
func Neutralize(s string) string {
	return promptTag.ReplaceAllStringFunc(s, func(tag string) string {
		return strings.NewReplacer("<", "‹", ">", "›").Replace(tag)
	})
}

// oneLine — a single-line field: whitespace collapsed, control characters
// dropped, at most max characters.
func oneLine(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s
}
