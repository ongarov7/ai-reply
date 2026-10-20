package credits

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

// Иесіз брон шегі: кемінде 5 минут, ұзын провайдер уақытында — төрт есесі.
func TestStaleAfterNeverCutsALiveRequest(t *testing.T) {
	for timeout, want := range map[time.Duration]time.Duration{
		0: 5 * time.Minute, 20 * time.Second: 5 * time.Minute, 75 * time.Second: 5 * time.Minute,
		2 * time.Minute: 8 * time.Minute,
	} {
		if got := StaleAfter(timeout); got != want {
			t.Errorf("StaleAfter(%s) = %s, want %s", timeout, got, want)
		}
	}
}

// Түсініктеме бір жолға келтіріледі; бос не 200 таңбадан ұзын болса — өріс қатесі.
func TestReasonIsOneLineAndBounded(t *testing.T) {
	got, err := cleanReason("  Компенсация\n за\tсбой  ", true)
	if err != nil || got != "Компенсация за сбой" {
		t.Fatalf("clean = %q (%v)", got, err)
	}
	if got, err := cleanReason(strings.Repeat("ә", ReasonMax), true); err != nil || len([]rune(got)) != ReasonMax {
		t.Fatalf("200 characters: %v", err)
	}
	for name, c := range map[string]struct {
		reason   string
		required bool
		want     string
	}{
		"empty":    {" \n ", true, "required"},
		"too long": {strings.Repeat("ә", ReasonMax+1), false, "too_long"},
	} {
		_, err := cleanReason(c.reason, c.required)
		var field *domain.FieldError
		if !errors.As(err, &field) || field.Field != "reason" || field.Reason != c.want {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got, err := cleanReason("", false); err != nil || got != "" {
		t.Fatalf("optional empty: %q %v", got, err)
	}
}

// Бонус баптауы: шегі жоқ не бұзылған болса — өшірулі; бұзылған сан — әдепкі мән.
func TestWelcomeSettingsWithoutACutoffStayOff(t *testing.T) {
	since := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	at := strconv.FormatInt(since.UnixMilli(), 10)
	for name, c := range map[string]struct {
		stored map[string]string
		want   Welcome
	}{
		"stored":       {map[string]string{SettingWelcomeUnits: "20", SettingWelcomeSince: at}, Welcome{20, since}},
		"off":          {map[string]string{SettingWelcomeUnits: "0", SettingWelcomeSince: at}, Welcome{0, since}},
		"no units row": {map[string]string{SettingWelcomeSince: at}, Welcome{DefaultWelcomeUnits, since}},
		"hand edit":    {map[string]string{SettingWelcomeUnits: "many", SettingWelcomeSince: at}, Welcome{DefaultWelcomeUnits, since}},
		"out of range": {map[string]string{SettingWelcomeUnits: "5000", SettingWelcomeSince: at}, Welcome{DefaultWelcomeUnits, since}},
		"no cutoff":    {map[string]string{SettingWelcomeUnits: "20"}, Welcome{}},
		"bad cutoff":   {map[string]string{SettingWelcomeUnits: "20", SettingWelcomeSince: "yesterday"}, Welcome{}},
	} {
		if got := parseWelcome(c.stored); got.Units != c.want.Units || !got.Since.Equal(c.want.Since) {
			t.Errorf("%s: %+v, want %+v", name, got, c.want)
		}
	}
}
