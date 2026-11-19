// Package screenshot — replies written from a screenshot of a conversation:
// the request model, its limits and the credit policy that prices it.
//
// THE CREDIT POLICY (docs/SCREENSHOT_REPLIES.md), in one paragraph: one
// credit is one standard reply — the prompt of an ordinary reply plus a full
// 400-character answer. A screenshot reply is priced by the tokens it really
// uses, measured in those standard replies:
//
//	weighted = input_tokens + w × output_tokens
//	reference = ref_input_tokens + w × ref_output_tokens
//	credits = clamp(ceil(weighted / reference − grace), 1, max_units)
//
// w is how much one output token costs compared to one input token: the
// model's own price row when there is one (output_per_1m / input_per_1m),
// otherwise the admin value. Input includes the image, the conversation and
// the account AI context, so nothing is charged twice. grace is the rounding
// rule: 0.5 rounds to the nearest credit, 0 always rounds up.
//
// Before a request the server computes the most it can cost (the exact input
// plus the full output limit) and the user approves that maximum; afterwards
// it charges what the provider actually reported, never more than approved.
// Unlimited plans pay one unit, with a disclosed daily cap on image analysis.
package screenshot

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// PolicyVersion names the formula above. It is recorded with every charge.
const PolicyVersion = "screenshot-credits-v1"

// Admin settings (system_settings). Every one has a code default.
const (
	SettingRefInputTokens     = "screenshot_ref_input_tokens"
	SettingRefOutputTokens    = "screenshot_ref_output_tokens"
	SettingOutputWeight       = "screenshot_output_weight_x100"
	SettingRoundingGrace      = "screenshot_rounding_grace_percent"
	SettingMaxUnits           = "screenshot_max_units"
	SettingTypicalOutput      = "screenshot_typical_output_percent"
	SettingImageEnabled       = "screenshot_image_enabled"
	SettingImageBaseTokens    = "screenshot_image_base_tokens"
	SettingImageTileTokens    = "screenshot_image_tile_tokens"
	SettingUnlimitedImagesDay = "screenshot_unlimited_images_per_day"
	SettingShortcutURL        = "screenshot_shortcut_url"
)

// Code defaults (2026-11-19). The reference is an ordinary reply as built by
// ai.BuildPrompt: about 1 500 input tokens (rules, profile, message) and up to
// 180 output tokens (a 400-character reply, the default max_output_tokens).
const (
	DefaultRefInputTokens     = 1500
	DefaultRefOutputTokens    = 180
	DefaultOutputWeightX100   = 400
	DefaultRoundingGrace      = 50
	DefaultMaxUnits           = 10
	DefaultTypicalOutput      = 70
	DefaultImageBaseTokens    = 85
	DefaultImageTileTokens    = 170
	DefaultUnlimitedImagesDay = 50
)

// Range — an inclusive range the admin panel may choose from.
type Range struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Admin ranges: wide enough for any real price and narrow enough that a typo
// cannot make a reply cost 0 or 1 000 credits.
var (
	RefInputRange       = Range{Min: 200, Max: 20000}
	RefOutputRange      = Range{Min: 32, Max: 4000}
	OutputWeightRange   = Range{Min: 100, Max: 3000}
	RoundingGraceRange  = Range{Min: 0, Max: 90}
	MaxUnitsRange       = Range{Min: 1, Max: 50}
	TypicalOutputRange  = Range{Min: 10, Max: 100}
	ImageBaseRange      = Range{Min: 0, Max: 5000}
	ImageTileRange      = Range{Min: 1, Max: 5000}
	UnlimitedImageRange = Range{Min: 0, Max: 1000}
)

// Policy — the settings in effect.
type Policy struct {
	RefInputTokens  int `json:"ref_input_tokens"`
	RefOutputTokens int `json:"ref_output_tokens"`
	// OutputWeightX100 — output token price ÷ input token price × 100, used
	// only when the model has no price row.
	OutputWeightX100 int `json:"output_weight_x100"`
	// RoundingGrace — percent of a credit forgiven before rounding up
	// (50 = nearest credit, 0 = always up).
	RoundingGrace int `json:"rounding_grace_percent"`
	MaxUnits      int `json:"max_units"`
	// TypicalOutput — the share of the output limit an estimate assumes.
	TypicalOutput int  `json:"typical_output_percent"`
	ImageEnabled  bool `json:"image_enabled"`
	// ImageBaseTokens and ImageTileTokens — the high-detail image price
	// before the provider has counted the real image.
	ImageBaseTokens int `json:"image_base_tokens"`
	ImageTileTokens int `json:"image_tile_tokens"`
	// UnlimitedImagesPerDay — image analyses an unlimited plan gets per day
	// (0 = no cap).
	UnlimitedImagesPerDay int `json:"unlimited_images_per_day"`
	// ShortcutURL — the iCloud link of the ready-made capture shortcut ("" —
	// the app shows the manual steps).
	ShortcutURL string `json:"shortcut_url"`
}

// DefaultPolicy — the code defaults.
func DefaultPolicy() Policy {
	return Policy{
		RefInputTokens: DefaultRefInputTokens, RefOutputTokens: DefaultRefOutputTokens,
		OutputWeightX100: DefaultOutputWeightX100, RoundingGrace: DefaultRoundingGrace,
		MaxUnits: DefaultMaxUnits, TypicalOutput: DefaultTypicalOutput, ImageEnabled: true,
		ImageBaseTokens: DefaultImageBaseTokens, ImageTileTokens: DefaultImageTileTokens,
		UnlimitedImagesPerDay: DefaultUnlimitedImagesDay,
	}
}

// PolicyError — a value outside its range (admin panel).
type PolicyError struct {
	Field string
	Range Range
}

func (e *PolicyError) Error() string {
	if e.Field == SettingShortcutURL {
		return SettingShortcutURL + " must be an https://www.icloud.com/shortcuts/ link or empty"
	}
	return fmt.Sprintf("%s must be between %d and %d", e.Field, e.Range.Min, e.Range.Max)
}

// Unwrap — 400 INVALID_REQUEST in the admin API.
func (e *PolicyError) Unwrap() error { return domain.ErrInvalidRequest }

// Details — for the admin panel.
func (e *PolicyError) Details() map[string]any {
	if e.Field == SettingShortcutURL {
		return map[string]any{"field": e.Field}
	}
	return map[string]any{"field": e.Field, "min": e.Range.Min, "max": e.Range.Max}
}

type intField struct {
	key    string
	target *int
	rng    Range
}

func (p *Policy) intFields() []intField {
	return []intField{
		{SettingRefInputTokens, &p.RefInputTokens, RefInputRange},
		{SettingRefOutputTokens, &p.RefOutputTokens, RefOutputRange},
		{SettingOutputWeight, &p.OutputWeightX100, OutputWeightRange},
		{SettingRoundingGrace, &p.RoundingGrace, RoundingGraceRange},
		{SettingMaxUnits, &p.MaxUnits, MaxUnitsRange},
		{SettingTypicalOutput, &p.TypicalOutput, TypicalOutputRange},
		{SettingImageBaseTokens, &p.ImageBaseTokens, ImageBaseRange},
		{SettingImageTileTokens, &p.ImageTileTokens, ImageTileRange},
		{SettingUnlimitedImagesDay, &p.UnlimitedImagesPerDay, UnlimitedImageRange},
	}
}

// Ranges — every numeric setting's range, by setting key (admin panel).
func Ranges() map[string]Range {
	var p Policy
	out := map[string]Range{}
	for _, f := range p.intFields() {
		out[f.key] = f.rng
	}
	return out
}

// Validate — every value inside its range and a usable shortcut link.
func (p Policy) Validate() error {
	for _, f := range p.intFields() {
		if *f.target < f.rng.Min || *f.target > f.rng.Max {
			return &PolicyError{Field: f.key, Range: f.rng}
		}
	}
	if !ValidShortcutURL(p.ShortcutURL) {
		return &PolicyError{Field: SettingShortcutURL}
	}
	return nil
}

// ValidShortcutURL — empty, or an https link to a shared iCloud shortcut. The
// app opens it in Safari/Shortcuts, so nothing else is accepted.
func ValidShortcutURL(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "www.icloud.com" || len(raw) > 300 {
		return false
	}
	return strings.HasPrefix(u.Path, "/shortcuts/") && len(u.Path) > len("/shortcuts/")
}

func (p Policy) settings() map[string]string {
	out := map[string]string{}
	for _, f := range p.intFields() {
		out[f.key] = strconv.Itoa(*f.target)
	}
	out[SettingImageEnabled] = "0"
	if p.ImageEnabled {
		out[SettingImageEnabled] = "1"
	}
	out[SettingShortcutURL] = p.ShortcutURL
	return out
}

// ---------------------------------------------------------------- pricing

// Usage — what one generation used, in tokens.
type Usage struct {
	Input  int
	Output int
}

// Weight — output token price ÷ input token price: the model's price row when
// it has a usable one, else the admin value.
func (p Policy) Weight(inputPer1M, outputPer1M float64) float64 {
	if inputPer1M > 0 && outputPer1M > 0 && !math.IsInf(outputPer1M/inputPer1M, 0) {
		return outputPer1M / inputPer1M
	}
	return float64(p.OutputWeightX100) / 100
}

// Reference — the weighted tokens of one standard reply (one credit).
func (p Policy) Reference(weight float64) float64 {
	return float64(p.RefInputTokens) + weight*float64(p.RefOutputTokens)
}

// Units — credits for this usage: clamp(ceil(weighted/reference − grace), 1, max).
func (p Policy) Units(u Usage, weight float64) int {
	ref := p.Reference(weight)
	if ref <= 0 {
		return 1
	}
	weighted := float64(max(u.Input, 0)) + weight*float64(max(u.Output, 0))
	// The epsilon keeps 2.0000000001 (float noise) from becoming 3.
	units := int(math.Ceil(weighted/ref - float64(p.RoundingGrace)/100 - 1e-9))
	return min(max(units, 1), p.MaxUnits)
}

// TypicalOutputTokens — the output an estimate assumes for this limit.
func (p Policy) TypicalOutputTokens(maxOutput int) int {
	return max(1, maxOutput*p.TypicalOutput/100)
}

// ImageTokens — the provider's high-detail price of an image, before it has
// counted the real one: the image is fitted into 2048×2048, its shorter side
// scaled to 768, and every 512-pixel tile costs ImageTileTokens on top of
// ImageBaseTokens. Zero or negative sizes cost the base only.
func (p Policy) ImageTokens(width, height int) int {
	if width <= 0 || height <= 0 {
		return p.ImageBaseTokens
	}
	w, h := float64(width), float64(height)
	if longest := math.Max(w, h); longest > 2048 {
		w, h = w*2048/longest, h*2048/longest
	}
	if shortest := math.Min(w, h); shortest > 768 {
		w, h = w*768/shortest, h*768/shortest
	}
	tiles := int(math.Ceil(w/512)) * int(math.Ceil(h/512))
	return p.ImageBaseTokens + tiles*p.ImageTileTokens
}
