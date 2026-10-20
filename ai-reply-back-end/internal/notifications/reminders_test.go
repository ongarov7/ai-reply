package notifications

import (
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Тыныш сағат: түн ортасынан өтетін терезе, белдеулер және «өшірулі» (start == end).
func TestQuietUntil(t *testing.T) {
	almaty, err := time.LoadLocation("Asia/Almaty")
	if err != nil {
		t.Skip("no tz database")
	}
	at := func(h, m int) time.Time { return time.Date(2026, 3, 10, h, m, 0, 0, almaty) }
	cases := []struct {
		now        time.Time
		start, end int
		want       time.Time
	}{
		{at(23, 30), 22, 9, time.Date(2026, 3, 11, 9, 0, 0, 0, almaty)},
		{at(3, 0), 22, 9, at(9, 0)},
		{at(9, 0), 22, 9, time.Time{}},
		{at(21, 59), 22, 9, time.Time{}},
		{at(22, 0), 22, 9, time.Date(2026, 3, 11, 9, 0, 0, 0, almaty)},
		{at(1, 0), 0, 7, at(7, 0)},
		{at(8, 0), 0, 7, time.Time{}},
		{at(3, 0), 5, 5, time.Time{}},
	}
	for _, c := range cases {
		got := QuietUntil(c.now.UTC(), almaty, c.start, c.end)
		if !got.Equal(c.want) {
			t.Errorf("%s [%d,%d): %s, want %s", c.now.Format("15:04"), c.start, c.end, got, c.want)
		}
	}
	// The same instant is quiet in one zone and not in another.
	instant := time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC)
	if !QuietUntil(instant, time.UTC, 22, 9).IsZero() {
		t.Error("14:00 UTC is not quiet")
	}
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	if got := QuietUntil(instant, tokyo, 22, 9); !got.Equal(time.Date(2026, 3, 11, 9, 0, 0, 0, tokyo)) {
		t.Errorf("23:00 in Tokyo: %s", got)
	}
}

// Квота хабары күн (не ай) соңына дейін, еске салу 12 сағат, қалғаны тәулік; кешіктірілгені сол сәттен.
func TestPushDeadline(t *testing.T) {
	almaty, err := time.LoadLocation("Asia/Almaty")
	if err != nil {
		t.Skip("no tz database")
	}
	s := &Service{loc: almaty}
	created := time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC) // 15:00 in Almaty
	cases := []struct {
		n    domain.Notification
		want time.Time
	}{
		{domain.Notification{Type: domain.TypeQuotaLow, IdempotencyKey: "quota_low:day:2026-03-10", CreatedAt: created},
			time.Date(2026, 3, 11, 0, 0, 0, 0, almaty)},
		{domain.Notification{Type: domain.TypeQuotaExhausted, IdempotencyKey: "quota_exhausted:month:2026-03", CreatedAt: created},
			time.Date(2026, 4, 1, 0, 0, 0, 0, almaty)},
		{domain.Notification{Type: domain.TypeClipboardHelp, CreatedAt: created}, created.Add(ReminderTTL)},
		{domain.Notification{Type: domain.TypeSubscriptionExpired, CreatedAt: created}, created.Add(DefaultTTL)},
	}
	for _, c := range cases {
		if got := s.pushDeadline(c.n); !got.Equal(c.want) {
			t.Errorf("%s: %s, want %s", c.n.Type, got, c.want)
		}
	}
	deferred := created.Add(9 * time.Hour)
	n := domain.Notification{Type: domain.TypeSubscriptionExpiring, CreatedAt: created, NotBefore: &deferred}
	if got := s.pushDeadline(n); !got.Equal(deferred.Add(DefaultTTL)) {
		t.Errorf("deferred: %s", got)
	}
	if ttl := s.pushTTL(n, deferred.Add(DefaultTTL)); ttl != 0 {
		t.Errorf("ttl at the deadline = %s", ttl)
	}
}

// Баптау ауқымдары: әр түр берілуі керек, мәндер шекте.
func TestReminderSettingsValidate(t *testing.T) {
	if err := DefaultReminderSettings().Validate(); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	for name, mutate := range map[string]func(*ReminderSettings){
		"missing type":  func(r *ReminderSettings) { delete(r.Types, domain.TypeClipboardHelp) },
		"extra type":    func(r *ReminderSettings) { r.Types["quota_low"] = ReminderTypeSettings{CooldownHours: 1} },
		"zero cooldown": func(r *ReminderSettings) { r.Types[domain.TypeClipboardHelp] = ReminderTypeSettings{MaxSends: 1} },
		"too many sends": func(r *ReminderSettings) {
			r.Types[domain.TypeClipboardHelp] = ReminderTypeSettings{CooldownHours: 1, MaxSends: 11}
		},
		"hour 24":      func(r *ReminderSettings) { r.QuietEndHour = 24 },
		"negative age": func(r *ReminderSettings) { r.MinAccountAgeHours = -1 },
	} {
		r := DefaultReminderSettings()
		mutate(&r)
		if err := r.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
