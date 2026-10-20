// Package credits — тарифтен тыс генерациялар: сәлемдесу бонусы, әкімші кредиттері, журнал.
//
// The plan's daily allowance is counted in usage_daily; everything else a
// person can spend lives in credit_grants and moves only through the ledger
// (repository.ReserveGeneration consumes, RefundGeneration gives back). This
// service decides who gets the welcome bonus, validates the administrator's
// manual grants, and refunds generations a crashed process left reserved.
package credits

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// Баптау кілттері (system_settings). Мәндерін 0015_credits жазады.
const (
	SettingWelcomeUnits = "welcome_bonus_units"
	SettingWelcomeSince = "welcome_bonus_since"
)

// DefaultWelcomeUnits — жаңа тіркелгінің бонусы, әкімші өзгертпесе.
const DefaultWelcomeUnits = 50

// Range — рұқсат етілген ауқым (шеттерімен қоса).
type Range struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Ауқымдар: бонус 0 (өшірулі) … 1000, қолмен берілетін кредит 1 … 10000.
var (
	WelcomeRange = Range{Min: 0, Max: 1000}
	GrantRange   = Range{Min: 1, Max: 10000}
)

// ReasonMax — әкімшінің түсініктемесінің шегі (таңбамен).
const ReasonMax = 200

// LedgerLimit — /me/credits пен әкімші картасындағы соңғы жазбалар саны.
const LedgerLimit = 50

// staleBatch — бір өтуде қайтарылатын брондар; staleRounds — бір өтудегі топтар саны.
const (
	staleBatch  = 50
	staleRounds = 10
)

// StaleAfter — «reserved» күйде қалған брон қашан иесіз саналады.
//
// A generation is one provider call and at most one repair call, each bounded
// by the provider timeout, plus a few seconds of bookkeeping. Four timeouts
// leave a wide margin; never less than five minutes, so a slow but live
// request is never refunded under its feet.
func StaleAfter(providerTimeout time.Duration) time.Duration {
	return max(5*time.Minute, 4*providerTimeout)
}

// Welcome — бонус баптауы: қанша және қай сәттен бастап ашылған тіркелгілерге.
type Welcome struct {
	Units int
	// Since — осы сәттен бұрын ашылған тіркелгілер бонус алмайды (0015 жазған уақыт).
	Since time.Time
}

// Service — кредиттер.
type Service struct {
	repo       *repository.Store
	log        *slog.Logger
	clock      traits.Clock
	ttl        time.Duration
	now        func() time.Time // кэш уақыты: сынақ сағатына тәуелсіз
	staleAfter time.Duration

	mu       sync.RWMutex
	welcome  Welcome
	loadedAt time.Time
	loaded   bool
}

// New — қызмет. Баптау 5 секунд кэште тұрады (limits.Service сияқты).
func New(repo *repository.Store, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log, clock: traits.SystemClock{}, ttl: 5 * time.Second, now: time.Now,
		staleAfter: StaleAfter(0)}
}

// WithClock — тестке.
func (s *Service) WithClock(c traits.Clock) *Service { s.clock = c; return s }

// WithTTL — баптау кэшінің ұзақтығы (тестте 0).
func (s *Service) WithTTL(ttl time.Duration) *Service { s.ttl = ttl; return s }

// WithStaleAfter — иесіз брон шегі (StaleAfter(OPENAI_TIMEOUT)).
func (s *Service) WithStaleAfter(d time.Duration) *Service { s.staleAfter = d; return s }

// ---------------------------------------------------------------- welcome bonus

// WelcomeSettings — бонустың ағымдағы баптауы.
//
// A failed read keeps the last known value; before the first successful read
// it answers "off", so a database hiccup can never hand out bonuses.
func (s *Service) WelcomeSettings(ctx context.Context) Welcome {
	s.mu.RLock()
	if s.loaded && s.now().Sub(s.loadedAt) < s.ttl {
		w := s.welcome
		s.mu.RUnlock()
		return w
	}
	s.mu.RUnlock()

	fresh, err := s.readWelcome(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.log.Error("welcome bonus settings read failed", "error", err.Error())
		if s.loaded {
			return s.welcome
		}
		return Welcome{}
	}
	s.welcome, s.loadedAt, s.loaded = fresh, s.now(), true
	return fresh
}

func (s *Service) readWelcome(ctx context.Context) (Welcome, error) {
	w := Welcome{Units: DefaultWelcomeUnits}
	raw, err := s.repo.Setting(ctx, SettingWelcomeUnits)
	switch {
	case err == nil:
		// A row that does not parse or is out of range can only come from a
		// hand edit; the default applies, as with the AI limits.
		if v, convErr := strconv.Atoi(strings.TrimSpace(raw)); convErr == nil && v >= WelcomeRange.Min && v <= WelcomeRange.Max {
			w.Units = v
		}
	case !errors.Is(err, domain.ErrNotFound):
		return Welcome{}, err
	}
	raw, err = s.repo.Setting(ctx, SettingWelcomeSince)
	switch {
	case err == nil:
		if v, convErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); convErr == nil && v > 0 {
			w.Since = traits.FromMillis(v)
		}
	case !errors.Is(err, domain.ErrNotFound):
		return Welcome{}, err
	}
	// Without a cutoff every existing account would qualify: the bonus stays off.
	if w.Since.IsZero() {
		w.Units = 0
	}
	return w, nil
}

// SetWelcomeUnits — жаңа тіркелгілердің бонусы (0 — өшірулі). Берілген бонустар өзгермейді.
func (s *Service) SetWelcomeUnits(ctx context.Context, units int) (Welcome, error) {
	if units < WelcomeRange.Min || units > WelcomeRange.Max {
		return Welcome{}, domain.InvalidField("units", "range")
	}
	if err := s.repo.SetSetting(ctx, SettingWelcomeUnits, strconv.Itoa(units)); err != nil {
		return Welcome{}, err
	}
	s.mu.Lock()
	s.loaded = false
	s.mu.Unlock()
	return s.WelcomeSettings(ctx), nil
}

// eligible — бонус осы тіркелгіге тиесілі ме (тек жаңа, кәдімгі тіркелгі).
//
// Legacy install tokens and the simulator account never qualify, and neither
// does an account created before the release cutoff.
func (s *Service) eligible(ctx context.Context, user domain.User) (int, bool) {
	if user.ID == "" || user.Kind != "account" {
		return 0, false
	}
	w := s.WelcomeSettings(ctx)
	if w.Units <= 0 || w.Since.IsZero() || user.CreatedAt.Before(w.Since) {
		return 0, false
	}
	return w.Units, true
}

// GrantWelcome — жаңа тіркелгіге бонус (бір рет қана). true — осы шақыру берді.
func (s *Service) GrantWelcome(ctx context.Context, user domain.User) (bool, error) {
	units, ok := s.eligible(ctx, user)
	if !ok {
		return false, nil
	}
	granted, err := s.repo.InsertWelcomeGrant(ctx, user.ID, units, s.clock.Now())
	if err != nil {
		return false, err
	}
	if granted {
		s.log.Info("welcome bonus granted", "user_id", user.ID, "units", units)
	}
	return granted, nil
}

// EnsureWelcome — кірген кезде бонус берілмей қалса, оны қалпына келтіреді (/me, сессия).
//
// Cheap on the hot path: the eligibility check is in memory, and only an
// eligible account costs one indexed read. Errors are logged, never returned:
// a profile or a sign-in must not fail because of the bonus.
func (s *Service) EnsureWelcome(ctx context.Context, user domain.User) {
	if _, ok := s.eligible(ctx, user); !ok {
		return
	}
	has, err := s.repo.HasWelcomeGrant(ctx, user.ID)
	if err != nil {
		s.log.Error("welcome bonus check failed", "user_id", user.ID, "error", err.Error())
		return
	}
	if has {
		return
	}
	if _, err := s.GrantWelcome(ctx, user); err != nil {
		s.log.Error("welcome bonus repair failed", "user_id", user.ID, "error", err.Error())
	}
}

// ---------------------------------------------------------------- balances

// Balance — тарифтен тыс қалдық.
func (s *Service) Balance(ctx context.Context, userID string) (domain.CreditBalance, error) {
	return s.repo.CreditBalance(ctx, userID, s.clock.Now())
}

// Ledger — соңғы журнал жазбалары, жаңасы бірінші.
func (s *Service) Ledger(ctx context.Context, userID string, limit int) ([]domain.LedgerEntry, error) {
	return s.repo.CreditLedger(ctx, userID, limit)
}

// Grants — қолданушының кредиттері, жаңасы бірінші.
func (s *Service) Grants(ctx context.Context, userID string, limit int) ([]domain.CreditGrant, error) {
	return s.repo.CreditGrants(ctx, userID, limit)
}

// ---------------------------------------------------------------- admin

// cleanReason — бір жолға келтірілген түсініктеме; required — бос болмауы керек.
func cleanReason(reason string, required bool) (string, error) {
	reason = traits.CollapseSpaces(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, reason))
	switch {
	case required && reason == "":
		return "", domain.InvalidField("reason", "required")
	case traits.RuneLen(reason) > ReasonMax:
		return "", domain.InvalidField("reason", "too_long")
	}
	return reason, nil
}

// AdminGrant — әкімші қолмен берген кредит (kind=admin): мерзімсіз, журналда «grant».
func (s *Service) AdminGrant(ctx context.Context, userID string, units int, reason, adminID string) (domain.CreditGrant, error) {
	if units < GrantRange.Min || units > GrantRange.Max {
		return domain.CreditGrant{}, domain.InvalidField("units", "range")
	}
	reason, err := cleanReason(reason, true)
	if err != nil {
		return domain.CreditGrant{}, err
	}
	if _, err := s.repo.UserByID(ctx, userID); err != nil {
		return domain.CreditGrant{}, err
	}
	return s.repo.InsertCreditGrant(ctx, domain.CreditGrant{
		UserID: userID, Kind: domain.GrantAdmin, UnitsTotal: units, UnitsRemaining: units,
		GrantedBy: adminID, Reason: reason,
	}, s.clock.Now())
}

// Revoke — кредитті тоқтату (кез келген түрі). revoked — қалдықтан алынғаны.
func (s *Service) Revoke(ctx context.Context, userID, grantID, reason, adminID string) (domain.CreditGrant, int, error) {
	reason, err := cleanReason(reason, false)
	if err != nil {
		return domain.CreditGrant{}, 0, err
	}
	return s.repo.RevokeCreditGrant(ctx, userID, grantID, adminID, reason, s.clock.Now())
}

// ---------------------------------------------------------------- stale reservations

// RefundStale — иесіз қалған брондарды қайтарады (процесс құлады не қайта іске қосылды).
//
// Small batches, each refund its own short transaction, so the sweep never
// holds the single writer during AI traffic. A reservation is refunded once:
// RefundGeneration only moves rows that are still reserved.
func (s *Service) RefundStale(ctx context.Context) (int, error) {
	now := s.clock.Now()
	before := now.Add(-s.staleAfter)
	refunded := 0
	for round := 0; round < staleRounds; round++ {
		ids, err := s.repo.StaleReservations(ctx, before, staleBatch)
		if err != nil {
			return refunded, err
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return refunded, ctx.Err()
			}
			ok, err := s.repo.RefundGeneration(ctx, id, now)
			if err != nil {
				return refunded, fmt.Errorf("refund reservation %s: %w", id, err)
			}
			if ok {
				refunded++
			}
		}
		if len(ids) < staleBatch {
			break
		}
	}
	if refunded > 0 {
		s.log.Warn("stale generation reservations refunded", "count", refunded)
	}
	return refunded, nil
}
