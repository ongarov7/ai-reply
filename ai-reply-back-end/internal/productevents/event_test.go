package productevents

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"
)

// The catalog holds exactly the events the iOS and Android apps record.
func TestCatalogMatchesTheAppEvents(t *testing.T) {
	want := []string{"autocorrect_disabled", "autocorrect_enabled", "full_access_enabled_detected", "gender_selected",
		"keyboard_enabled_detected", "onboarding_completed", "onboarding_keyboard_step_viewed",
		"onboarding_practice_completed", "onboarding_reopened", "onboarding_started", "onboarding_step_viewed",
		"paste_tutorial_viewed"}
	if got := Names(); !slices.Equal(got, want) {
		t.Fatalf("Names() = %v", got)
	}
}

func TestParseAcceptsCatalogEvents(t *testing.T) {
	cases := []struct {
		raw   string
		name  string
		props map[string]any
		at    time.Time
	}{
		{`{"name":"onboarding_started","ts":"2026-10-05T09:30:00Z","props":{"version":2,"trigger":"auto"}}`,
			"onboarding_started", map[string]any{"version": 2, "trigger": "auto"}, time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)},
		{`{"name":"onboarding_step_viewed","ts":"2026-10-05T14:30:00+05:00","props":{"step":"fullAccess"}}`,
			"onboarding_step_viewed", map[string]any{"step": "fullAccess"}, time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)},
		{`{"name":"onboarding_completed","props":{"version":0,"skipped":false}}`,
			"onboarding_completed", map[string]any{"version": 0, "skipped": false}, time.Time{}},
		{`{"name":"gender_selected","props":{"source":"settings","skipped":true}}`,
			"gender_selected", map[string]any{"source": "settings", "skipped": true}, time.Time{}},
		// Properties are optional; an event without any is fine.
		{`{"name":"keyboard_enabled_detected"}`, "keyboard_enabled_detected", map[string]any{}, time.Time{}},
		{`{"name":"autocorrect_enabled","props":{}}`, "autocorrect_enabled", map[string]any{}, time.Time{}},
		{`{"name":"onboarding_started","props":{"version":1000}}`, "onboarding_started", map[string]any{"version": 1000}, time.Time{}},
	}
	for _, c := range cases {
		event, err := Parse(json.RawMessage(c.raw))
		if err != nil {
			t.Errorf("%s: %v", c.raw, err)
			continue
		}
		if event.Name != c.name || !reflect.DeepEqual(event.Props, c.props) || !event.ClientTime.Equal(c.at) {
			t.Errorf("%s: got %+v", c.raw, event)
		}
	}
}

func TestParseRejectsAnythingOutsideTheCatalog(t *testing.T) {
	cases := map[string]error{
		// Unknown names, keys and shapes.
		`{"name":"message_copied"}`:     ErrUnknownEvent,
		`{"name":"Onboarding_Started"}`: ErrUnknownEvent,
		`{"name":""}`:                   ErrUnknownEvent,
		`null`:                          ErrUnknownEvent,
		`{"name":"onboarding_reopened","props":{"text":"привет"}}`:   ErrUnknownProperty,
		`{"name":"gender_selected","props":{"gender":"female"}}`:     ErrUnknownProperty,
		`{"name":"onboarding_reopened","label":"x"}`:                 ErrMalformedEvent,
		`{"name":"onboarding_reopened","props":"skipped"}`:           ErrMalformedEvent,
		`["onboarding_reopened"]`:                                    ErrMalformedEvent,
		`"onboarding_reopened"`:                                      ErrMalformedEvent,
		`{"name":"onboarding_reopened","props":{"step":{"a":1}}}`:    ErrUnknownProperty,
		`{"name":"onboarding_step_viewed","props":{"step":{"a":1}}}`: ErrInvalidValue,
		// Free text, wrong types and out-of-range numbers.
		`{"name":"onboarding_step_viewed","props":{"step":"Айгерим, перезвони"}}`: ErrInvalidValue,
		`{"name":"onboarding_step_viewed","props":{"step":"FullAccess"}}`:         ErrInvalidValue,
		`{"name":"onboarding_step_viewed","props":{"step":null}}`:                 ErrInvalidValue,
		`{"name":"onboarding_step_viewed","props":{"step":3}}`:                    ErrInvalidValue,
		`{"name":"onboarding_started","props":{"version":"2"}}`:                   ErrInvalidValue,
		`{"name":"onboarding_started","props":{"version":1.5}}`:                   ErrInvalidValue,
		`{"name":"onboarding_started","props":{"version":1e2}}`:                   ErrInvalidValue,
		`{"name":"onboarding_started","props":{"version":-1}}`:                    ErrInvalidValue,
		`{"name":"onboarding_started","props":{"version":1001}}`:                  ErrInvalidValue,
		`{"name":"onboarding_started","props":{"version":true}}`:                  ErrInvalidValue,
		`{"name":"onboarding_completed","props":{"skipped":"true"}}`:              ErrInvalidValue,
		`{"name":"onboarding_completed","props":{"skipped":1}}`:                   ErrInvalidValue,
		`{"name":"onboarding_completed","props":{"skipped":null}}`:                ErrInvalidValue,
		// The time must be RFC 3339.
		`{"name":"onboarding_reopened","ts":"yesterday"}`:  ErrInvalidTime,
		`{"name":"onboarding_reopened","ts":"2026-10-05"}`: ErrInvalidTime,
		`{"name":"onboarding_reopened","ts":1759656600}`:   ErrMalformedEvent,
	}
	for raw, want := range cases {
		if _, err := Parse(json.RawMessage(raw)); !errors.Is(err, want) {
			t.Errorf("Parse(%s) = %v, want %v", raw, err, want)
		}
	}
}
