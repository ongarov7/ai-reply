package aicontext

import (
	"fmt"
	"strconv"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Admin settings (system_settings) of the context surcharge. The owner's rule:
// a reply costs 1 + one more for every chars_per_reply characters of context
// beyond free_chars, at most max_extra_replies more. Pro (unlimited) pays none.
const (
	SettingCharsPerReply   = "ai_context_chars_per_reply"
	SettingFreeChars       = "ai_context_free_chars"
	SettingMaxExtraReplies = "ai_context_max_extra_replies"
)

// Code defaults (owner, 2026-11-17): 400 characters = 1 reply, nothing free,
// at most +2.
const (
	DefaultCharsPerReply   = 400
	DefaultFreeChars       = 0
	DefaultMaxExtraReplies = 2
)

// Range — an inclusive range the admin panel may choose from.
type Range struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Admin ranges. chars_per_reply = 0 switches the surcharge off.
var (
	CharsPerReplyRange   = Range{Min: 0, Max: 4000}
	FreeCharsRange       = Range{Min: 0, Max: 10000}
	MaxExtraRepliesRange = Range{Min: 0, Max: 10}
)

// Pricing — the surcharge settings in effect.
type Pricing struct {
	CharsPerReply   int `json:"chars_per_reply"`
	FreeChars       int `json:"free_chars"`
	MaxExtraReplies int `json:"max_extra_replies"`
}

// DefaultPricing — the code defaults.
func DefaultPricing() Pricing {
	return Pricing{CharsPerReply: DefaultCharsPerReply, FreeChars: DefaultFreeChars, MaxExtraReplies: DefaultMaxExtraReplies}
}

// PricingError — a value outside its range (admin panel).
type PricingError struct {
	Field string
	Range Range
}

func (e *PricingError) Error() string {
	return fmt.Sprintf("%s must be between %d and %d", e.Field, e.Range.Min, e.Range.Max)
}

// Unwrap — 400 INVALID_REQUEST in the admin API.
func (e *PricingError) Unwrap() error { return domain.ErrInvalidRequest }

// Details — for the admin panel.
func (e *PricingError) Details() map[string]any {
	return map[string]any{"field": e.Field, "min": e.Range.Min, "max": e.Range.Max}
}

// Validate — every value inside its range.
func (p Pricing) Validate() error {
	for _, c := range []struct {
		field string
		value int
		rng   Range
	}{
		{SettingCharsPerReply, p.CharsPerReply, CharsPerReplyRange},
		{SettingFreeChars, p.FreeChars, FreeCharsRange},
		{SettingMaxExtraReplies, p.MaxExtraReplies, MaxExtraRepliesRange},
	} {
		if c.value < c.rng.Min || c.value > c.rng.Max {
			return &PricingError{Field: c.field, Range: c.rng}
		}
	}
	return nil
}

func (p Pricing) settings() map[string]string {
	return map[string]string{
		SettingCharsPerReply:   strconv.Itoa(p.CharsPerReply),
		SettingFreeChars:       strconv.Itoa(p.FreeChars),
		SettingMaxExtraReplies: strconv.Itoa(p.MaxExtraReplies),
	}
}

// Extra — the extra replies a request with this many context characters costs
// on a plan that is not unlimited:
//
//	min(max_extra, ceil(max(0, chars − free) / chars_per_reply))
func (p Pricing) Extra(chars int) int {
	if p.CharsPerReply <= 0 || p.MaxExtraReplies <= 0 || chars <= p.FreeChars {
		return 0
	}
	over := chars - p.FreeChars
	extra := (over + p.CharsPerReply - 1) / p.CharsPerReply
	return min(extra, p.MaxExtraReplies)
}

// Cost — what one reply costs, in replies from the package, per recipient.
type Cost struct {
	BaseChars int `json:"base_chars"`
	General   int `json:"general"`
	Friend    int `json:"friend"`
	Client    int `json:"client"`
	Colleague int `json:"colleague"`
	Partner   int `json:"partner"`
}

// Costs — the cost table the cabinet shows. An unlimited plan or a switched-off
// document costs 1 everywhere; an empty document costs 1.
func (p Pricing) Costs(d Document, unlimited bool) Cost {
	units := func(recipient string) int {
		if unlimited {
			return 1
		}
		rendered := d.Render(recipient)
		if !rendered.Present() {
			return 1
		}
		return 1 + p.Extra(rendered.Chars)
	}
	return Cost{
		BaseChars: d.BaseChars(),
		General:   units(""),
		Friend:    units(RecipientFriend),
		Client:    units(RecipientClient),
		Colleague: units(RecipientColleague),
		Partner:   units(RecipientPartner),
	}
}
