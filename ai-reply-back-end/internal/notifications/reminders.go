package notifications

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// ReminderTTL — еске салу осыдан кейін жіберілмейді (кезекте қалып қойса).
const ReminderTTL = 12 * time.Hour

// maxPushTTL — FCM қабылдайтын ең ұзақ TTL.
const maxPushTTL = 28 * 24 * time.Hour

// reminderBatch — шолудың бір сұранысы алатын үміткерлер саны (курсормен жалғасады).
const reminderBatch = 200

// Push-тың мерзімі өтіп кеткені және құрылғы бар, бірақ жеткізуге болмайтыны (skipped жолдың коды).
const (
	SkipExpired           = "expired"
	SkipNoReachableDevice = "no_reachable_device"
)

// ReminderLink — еске салу ашатын экран. keyboard ескі build-терде де жұмыс істейді.
func ReminderLink(kind string) string {
	switch kind {
	case domain.TypeOnboardingIncomplete:
		return "aireply://onboarding"
	case domain.TypeKeyboardSetupIncomplete:
		return "aireply://keyboard"
	case domain.TypeClipboardHelp:
		return "aireply://paste_help"
	default:
		return "aireply://profile"
	}
}

// SweepReminders — дәлелі бар тіркелгілерге еске салу (әр 30 минут, барлық процесте).
//
// Evidence comes only from fresh diagnostics (≤ 14 days, app or keyboard
// check); unknown, stale or absent state never triggers anything. Each send
// is attempt N+1 of the ledger: the key "<type>:<N+1>" and the ledger CAS in
// the notification's transaction make a repeated sweep or a second process
// a no-op. Accounts in their quiet hours wait for a later sweep, and an
// account with no reachable device is never selected, so it uses no attempt.
func (e *Events) SweepReminders(ctx context.Context) {
	s := e.svc
	if !s.Ready() {
		return
	}
	set := s.ReminderSettings(ctx)
	if !set.Enabled {
		return
	}
	now := s.clock.Now()
	reach, _, err := s.audienceQuery(ctx, domain.AudienceFilter{}, domain.CategorySystem)
	if err != nil {
		s.log.Error("reminder sweep failed", "error", err.Error())
		return
	}
	for _, kind := range domain.ReminderTypes {
		t := set.Types[kind]
		if t.MaxSends <= 0 {
			continue
		}
		after := ""
		for ctx.Err() == nil {
			candidates, err := s.repo.ReminderCandidates(ctx, repository.ReminderQuery{
				Type: kind, Now: now, Cooldown: time.Duration(t.CooldownHours) * time.Hour, MaxSends: t.MaxSends,
				MinAccountAge: time.Duration(set.MinAccountAgeHours) * time.Hour, Reach: reach, After: after,
				Limit: reminderBatch,
			})
			if err != nil {
				s.log.Error("reminder sweep failed", "type", kind, "error", err.Error())
				break
			}
			for _, c := range candidates {
				if ctx.Err() != nil {
					return
				}
				after = c.UserID
				if !s.quietUntil(ctx, c.UserID, now, set).IsZero() {
					continue
				}
				e.notify(ctx, UserNotification{
					UserID:         c.UserID,
					IdempotencyKey: kind + ":" + strconv.Itoa(c.SendCount+1),
					Type:           kind,
					Category:       domain.CategorySystem,
					Link:           ReminderLink(kind),
					TitleKey:       "push." + kind + ".title",
					BodyKey:        "push." + kind + ".body",
					Defer:          true,
					Reminder: &repository.ReminderMark{
						Type: kind, Expected: c.SendCount, Source: repository.ReminderSourceAuto,
					},
				})
			}
			if len(candidates) < reminderBatch {
				break
			}
		}
	}
}

// quietUntil — қолданушының жергілікті тыныш сағаты болса, оның біткен сәті; әйтпесе нөл.
func (s *Service) quietUntil(ctx context.Context, userID string, now time.Time, set ReminderSettings) time.Time {
	loc := s.loc
	if tz, err := s.repo.LatestInstallationTimezone(ctx, userID); err == nil && tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return QuietUntil(now, loc, set.QuietStartHour, set.QuietEndHour)
}

// QuietUntil — now loc белдеуінде [start, end) сағаттарына түссе, end-ке дейінгі сәт (UTC); әйтпесе нөл.
// The window may cross midnight (22 → 9); start == end means no quiet hours.
func QuietUntil(now time.Time, loc *time.Location, start, end int) time.Time {
	if start == end {
		return time.Time{}
	}
	local := now.In(loc)
	h := local.Hour()
	quiet := h >= start && h < end
	if start > end {
		quiet = h >= start || h < end
	}
	if !quiet {
		return time.Time{}
	}
	until := time.Date(local.Year(), local.Month(), local.Day(), end, 0, 0, 0, loc)
	if !until.After(local) {
		until = time.Date(local.Year(), local.Month(), local.Day()+1, end, 0, 0, 0, loc)
	}
	return until.UTC()
}

// pushDeadline — осы сәттен кейін push жіберілмейді (expired).
//
// A quota notice is true only until its day (or month) ends in the app
// timezone; a reminder is worth 12 hours; a support reply and anything else
// a day. The window counts from the time the push was first allowed to go out
// (after quiet hours), not from when it was written.
func (s *Service) pushDeadline(n domain.Notification) time.Time {
	start := n.CreatedAt
	if n.NotBefore != nil && n.NotBefore.After(start) {
		start = *n.NotBefore
	}
	switch {
	case n.Type == domain.TypeQuotaLow || n.Type == domain.TypeQuotaExhausted:
		local := n.CreatedAt.In(s.loc)
		if strings.Contains(n.IdempotencyKey, ":month:") {
			return time.Date(local.Year(), local.Month()+1, 1, 0, 0, 0, 0, s.loc)
		}
		return time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, s.loc)
	case domain.IsReminderType(n.Type):
		return start.Add(ReminderTTL)
	case n.Type == domain.TypeSupportReply:
		return start.Add(SupportReplyTTL)
	default:
		return start.Add(DefaultTTL)
	}
}

// pushTTL — провайдерге берілетін қалған TTL (жоқ болса 0: өтіп кеткен).
func (s *Service) pushTTL(n domain.Notification, now time.Time) time.Duration {
	ttl := s.pushDeadline(n).Sub(now)
	if ttl < time.Second {
		return 0
	}
	return min(ttl, maxPushTTL)
}

// CreateReminderCampaign — әкімшінің еске салуы: түрдің төрт тілдегі мәтінімен науқан.
//
// The audience must be narrowed by a diagnostics filter or name people: a
// reminder never goes to everyone. The campaign's notifications carry the
// reminder type, so delivery history filters by it, and its recipients'
// ledger rows count the send (the automatic sweep keeps its cooldown).
func (s *Service) CreateReminderCampaign(ctx context.Context, adminID, kind string, audience domain.AudienceFilter, idempotencyKey string) (CampaignResult, error) {
	if !domain.IsReminderType(kind) {
		return CampaignResult{}, domain.InvalidField("type", "unknown")
	}
	if strings.TrimSpace(audience.Diagnostics) == "" && !audience.Specific() {
		return CampaignResult{}, domain.InvalidField("audience.diagnostics", "required")
	}
	if s.translate == nil {
		return CampaignResult{}, domain.InvalidField("type", "no translations are loaded")
	}
	in := CampaignInput{
		Category: domain.CategorySystem, FallbackLocale: DefaultFallbackLocale, Link: ReminderLink(kind),
		Title: map[string]string{}, Body: map[string]string{}, Audience: audience, NotificationType: kind,
	}
	for _, l := range ContentLocales {
		in.Title[l] = s.translate(l, "push."+kind+".title")
		in.Body[l] = s.translate(l, "push."+kind+".body")
	}
	return s.CreateCampaign(ctx, adminID, in, idempotencyKey, true)
}
