// Package productevents — қолданбалардың өнім оқиғалары: онбординг пен
// баптаудағы әрекеттер, тек санау үшін.
//
// The server decides what an event may carry. Every event name, every
// property key and every value type is on the catalog below: codes from a
// fixed set, small integers and booleans. Anything else rejects that one
// event, never the whole batch, so no free text can reach the database even
// from a broken or hostile client. Only the apps send events; the keyboards
// send nothing.
package productevents

import (
	"slices"
	"sort"
)

// kind — the type a property value must have.
type kind int

const (
	kindInteger kind = iota // 0…MaxInteger
	kindFlag                // true | false
	kindCode                // one of property.codes
)

// MaxInteger — the largest integer a property may hold (a version number).
const MaxInteger = 1000

type property struct {
	kind  kind
	codes []string
}

func integer() property              { return property{kind: kindInteger} }
func flag() property                 { return property{kind: kindFlag} }
func code(values ...string) property { return property{kind: kindCode, codes: values} }

// catalog — every event the server accepts and the properties it may carry.
// A property may be left out; one that is not listed rejects the event. The
// gender itself is never a property: gender_selected says only where it was
// chosen and whether the user skipped.
var catalog = map[string]map[string]property{
	"onboarding_started":              {"version": integer(), "trigger": code("auto", "settings")},
	"onboarding_step_viewed":          {"step": code("welcome", "gender", "keyboard", "fullAccess", "copyReply", "practice", "done")},
	"onboarding_keyboard_step_viewed": {},
	"keyboard_enabled_detected":       {},
	"full_access_enabled_detected":    {},
	"paste_tutorial_viewed":           {},
	"onboarding_practice_completed":   {},
	"onboarding_completed":            {"version": integer(), "skipped": flag()},
	"onboarding_reopened":             {},
	"gender_selected":                 {"source": code("onboarding", "settings"), "skipped": flag()},
	"autocorrect_enabled":             {},
	"autocorrect_disabled":            {},
}

// Names — рұқсат етілген оқиға атаулары, әліпби бойынша.
func Names() []string {
	names := make([]string, 0, len(catalog))
	for name := range catalog {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (p property) allows(code string) bool { return slices.Contains(p.codes, code) }
