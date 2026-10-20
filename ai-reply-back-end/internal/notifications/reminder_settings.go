package notifications

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Еске салу баптауларының кілттері (system_settings). Әр түрдің кілті:
// reminders.<type>.cooldown_hours және reminders.<type>.max_sends.
const (
	SettingRemindersEnabled  = "reminders.enabled"
	SettingReminderMinAge    = "reminders.min_account_age_hours"
	SettingReminderQuietFrom = "reminders.quiet_start_hour"
	SettingReminderQuietTo   = "reminders.quiet_end_hour"
)

// ReminderRange — әкімші таңдай алатын ауқым (шеттерін қоса).
type ReminderRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Ауқымдар: кең, бірақ қате басу хабарламаны спамға айналдыра алмайды.
var (
	ReminderCooldownRange = ReminderRange{Min: 1, Max: 2160}
	ReminderMaxSendsRange = ReminderRange{Min: 0, Max: 10}
	ReminderMinAgeRange   = ReminderRange{Min: 0, Max: 720}
	ReminderHourRange     = ReminderRange{Min: 0, Max: 23}
)

// ReminderTypeSettings — бір түрдің үзілісі және ең көп жіберу саны (0 — түр өшірулі).
type ReminderTypeSettings struct {
	CooldownHours int `json:"cooldown_hours"`
	MaxSends      int `json:"max_sends"`
}

// ReminderSettings — еске салулардың әкімші баптаулары. Әдепкіде өшірулі.
//
// Quiet hours are the recipient's local hours (the timezone of the account's
// most recently seen device, else the server's): reminders and sweep notices
// wait until they end. Start equal to end means no quiet hours.
type ReminderSettings struct {
	Enabled            bool                            `json:"enabled"`
	Types              map[string]ReminderTypeSettings `json:"types"`
	MinAccountAgeHours int                             `json:"min_account_age_hours"`
	QuietStartHour     int                             `json:"quiet_start_hour"`
	QuietEndHour       int                             `json:"quiet_end_hour"`
}

// DefaultReminderSettings — код әдепкілері.
func DefaultReminderSettings() ReminderSettings {
	return ReminderSettings{
		Enabled: false,
		Types: map[string]ReminderTypeSettings{
			domain.TypeOnboardingIncomplete:    {CooldownHours: 72, MaxSends: 2},
			domain.TypeKeyboardSetupIncomplete: {CooldownHours: 48, MaxSends: 3},
			domain.TypeClipboardHelp:           {CooldownHours: 168, MaxSends: 1},
		},
		MinAccountAgeHours: 24,
		QuietStartHour:     22,
		QuietEndHour:       9,
	}
}

func inRange(field string, v int, r ReminderRange) error {
	if v < r.Min || v > r.Max {
		return domain.InvalidField(field, "between "+strconv.Itoa(r.Min)+" and "+strconv.Itoa(r.Max))
	}
	return nil
}

// Validate — әр мән өз ауқымында, түрлер дәл ReminderTypes.
func (r ReminderSettings) Validate() error {
	if len(r.Types) != len(domain.ReminderTypes) {
		return domain.InvalidField("types", "give every reminder type")
	}
	for _, kind := range domain.ReminderTypes {
		t, ok := r.Types[kind]
		if !ok {
			return domain.InvalidField("types", "give every reminder type")
		}
		if err := inRange("types."+kind+".cooldown_hours", t.CooldownHours, ReminderCooldownRange); err != nil {
			return err
		}
		if err := inRange("types."+kind+".max_sends", t.MaxSends, ReminderMaxSendsRange); err != nil {
			return err
		}
	}
	if err := inRange("min_account_age_hours", r.MinAccountAgeHours, ReminderMinAgeRange); err != nil {
		return err
	}
	if err := inRange("quiet_start_hour", r.QuietStartHour, ReminderHourRange); err != nil {
		return err
	}
	return inRange("quiet_end_hour", r.QuietEndHour, ReminderHourRange)
}

func (r ReminderSettings) values() map[string]string {
	out := map[string]string{
		SettingRemindersEnabled:  strconv.FormatBool(r.Enabled),
		SettingReminderMinAge:    strconv.Itoa(r.MinAccountAgeHours),
		SettingReminderQuietFrom: strconv.Itoa(r.QuietStartHour),
		SettingReminderQuietTo:   strconv.Itoa(r.QuietEndHour),
	}
	for kind, t := range r.Types {
		out["reminders."+kind+".cooldown_hours"] = strconv.Itoa(t.CooldownHours)
		out["reminders."+kind+".max_sends"] = strconv.Itoa(t.MaxSends)
	}
	return out
}

// reminderStore — system_settings (repository.Store).
type reminderStore interface {
	Setting(ctx context.Context, key string) (string, error)
	SetSettings(ctx context.Context, values map[string]string) error
}

// reminderSettings — internal/limits сияқты қысқа кэшпен оқитын баптаулар.
type reminderSettings struct {
	store reminderStore
	ttl   time.Duration

	mu       sync.Mutex
	current  ReminderSettings
	loadedAt time.Time
	loaded   bool
}

func (c *reminderSettings) get(ctx context.Context) ReminderSettings {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded && time.Since(c.loadedAt) < c.ttl {
		return c.current
	}
	fresh, err := c.read(ctx)
	if err != nil {
		// A database hiccup keeps the last known values; without any, reminders stay off.
		if c.loaded {
			return c.current
		}
		return DefaultReminderSettings()
	}
	c.current, c.loadedAt, c.loaded = fresh, time.Now(), true
	return fresh
}

func (c *reminderSettings) read(ctx context.Context) (ReminderSettings, error) {
	out := DefaultReminderSettings()
	number := func(key string, target *int, r ReminderRange) error {
		raw, err := c.store.Setting(ctx, key)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if v, convErr := strconv.Atoi(raw); convErr == nil && v >= r.Min && v <= r.Max {
			*target = v
		}
		return nil
	}
	raw, err := c.store.Setting(ctx, SettingRemindersEnabled)
	switch {
	case err == nil:
		out.Enabled = raw == "true"
	case !errors.Is(err, domain.ErrNotFound):
		return out, err
	}
	for _, kind := range domain.ReminderTypes {
		t := out.Types[kind]
		if err := number("reminders."+kind+".cooldown_hours", &t.CooldownHours, ReminderCooldownRange); err != nil {
			return out, err
		}
		if err := number("reminders."+kind+".max_sends", &t.MaxSends, ReminderMaxSendsRange); err != nil {
			return out, err
		}
		out.Types[kind] = t
	}
	for key, target := range map[string]*int{
		SettingReminderMinAge: &out.MinAccountAgeHours, SettingReminderQuietFrom: &out.QuietStartHour,
		SettingReminderQuietTo: &out.QuietEndHour,
	} {
		r := ReminderHourRange
		if key == SettingReminderMinAge {
			r = ReminderMinAgeRange
		}
		if err := number(key, target, r); err != nil {
			return out, err
		}
	}
	return out, nil
}

// ReminderSettings — қолданыстағы еске салу баптаулары (бірнеше секунд кэштеледі).
func (s *Service) ReminderSettings(ctx context.Context) ReminderSettings {
	return s.reminders.get(ctx)
}

// RemindersEnabled — еске салулар қосулы ма (/api/v1/config features.push_reminders).
func (s *Service) RemindersEnabled(ctx context.Context) bool { return s.ReminderSettings(ctx).Enabled }

// UpdateReminderSettings — тексеріп, бір транзакцияда сақтайды және кэшті жаңартады.
func (s *Service) UpdateReminderSettings(ctx context.Context, next ReminderSettings) (ReminderSettings, error) {
	if err := next.Validate(); err != nil {
		return ReminderSettings{}, err
	}
	if err := s.reminders.store.SetSettings(ctx, next.values()); err != nil {
		return ReminderSettings{}, err
	}
	s.reminders.mu.Lock()
	s.reminders.current, s.reminders.loadedAt, s.reminders.loaded = next, time.Now(), true
	s.reminders.mu.Unlock()
	return next, nil
}
