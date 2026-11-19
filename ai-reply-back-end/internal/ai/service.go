package ai

import (
	"context"
	"errors"
	"github.com/aireply/ai-reply-back-end/internal/logging"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aireply/ai-reply-back-end/config"
	"github.com/aireply/ai-reply-back-end/internal/aicontext"
	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/limits"
	"github.com/aireply/ai-reply-back-end/internal/repository"
	"github.com/aireply/ai-reply-back-end/internal/subscriptions"
	"github.com/aireply/ai-reply-back-end/internal/traits"
)

// LimitsSource — әкімші панелі басқаратын шектеулер (limits.Service).
type LimitsSource interface {
	Current(ctx context.Context) limits.Limits
}

// QuotaEvents — квота шегіне жеткенде хабарлайтын қабат (notifications.Events).
//
// usage carries the counters after this request (or at the limit when the
// reservation was refused); date and month are the quota period keys. It is
// called on the reply path: the implementation must decide cheaply and log
// its own errors.
type QuotaEvents interface {
	QuotaUsed(ctx context.Context, user domain.User, usage domain.Entitlement, date, month string)
}

// ProDevices is evaluated against the existing server-resolved entitlement.
type ProDevices interface {
	Authorize(context.Context, domain.User, string, domain.Entitlement) error
}

// Service — AI шлюзі: квота → провайдер → сапа → есеп.
type Service struct {
	repo       *repository.Store
	subs       *subscriptions.Service
	provider   Provider
	limits     LimitsSource
	log        *slog.Logger
	clock      traits.Clock
	repair     bool
	quota      QuotaEvents
	proDevices ProDevices
	macContext config.MacContext
	// polishPerDay — бір қолданушының сервер күніндегі polish шегі (0 — шексіз).
	polishPerDay int
	// transcriber — дауыс → мәтін провайдері (nil — мүмкіндік жоқ).
	transcriber Transcriber
	// transcribePerDay — бір қолданушының сервер күніндегі дауыс шегі (0 — шексіз).
	transcribePerDay int
	// voice — request_id бойынша қайталанбайтын дауыс шақырулары (жадта).
	voice *voiceCache
	// A bounded live turn per account; never hold a mutex during network I/O.
	liveUsers sync.Map
	// screenshots — the screenshot credit policy (nil — code defaults).
	screenshots ScreenshotPolicies
	// countRetryAt — unix nanoseconds before which the provider's input-token
	// count is not asked again after a failure.
	countRetryAt atomic.Int64
}

// New — шлюз. Тексеруден өтпеген жауапты түзету әдепкіде қосулы.
func New(repo *repository.Store, subs *subscriptions.Service, provider Provider, limits LimitsSource, log *slog.Logger) *Service {
	s := &Service{repo: repo, subs: subs, provider: provider, limits: limits, log: log,
		clock: traits.SystemClock{}, repair: true}
	// The cache reads the service's clock on every call, so WithClock covers it too.
	s.voice = newVoiceCache(voiceCacheTTL, voiceCacheEntries, func() time.Time { return s.clock.Now() })
	return s
}

func (s *Service) WithProDevices(p ProDevices) *Service { s.proDevices = p; return s }

// WithClock — тестке.
func (s *Service) WithClock(c traits.Clock) *Service { s.clock = c; return s }

// WithRepair — бір реттік түзету сұранысы (AI_REPAIR_ENABLED).
func (s *Service) WithRepair(enabled bool) *Service { s.repair = enabled; return s }

// WithQuotaEvents — квота хабарламалары (nil — жоқ).
func (s *Service) WithQuotaEvents(q QuotaEvents) *Service { s.quota = q; return s }

// WithPolishDailyLimit — polish шегі бір қолданушыға күніне (LIMIT_POLISH_PER_DAY).
func (s *Service) WithPolishDailyLimit(n int) *Service { s.polishPerDay = n; return s }

// Request — бір жауап сұранысы. Мәтін тек жадта, тек осы шақыру ішінде болады.
type Request struct {
	User     domain.User
	DeviceID string
	// SessionPlatform comes from signed authentication, never the body.
	SessionPlatform string
	Context         *GenerationContext
	OutputCharLimit int
	SourceText      string
	Instruction     string
	// Voice — the instruction was dictated in AI Reply Voice: it may use the
	// room of a 40-second turn (limits.VoiceInstructionChars).
	Voice    bool
	Language string
	// InputLanguage — тексерілген пернетақта коды не бос.
	InputLanguage string
	TemplateID    string
	Profile       Profile
	Template      Template
	Business      WorkingHours
	Platform      string
	AppVersion    string
	// IdempotencyKey — Idempotency-Key тақырыбы ("" — кілтсіз): бір әрекетке бір төлем.
	IdempotencyKey string
	// Account and AccountExtra — the account AI context for this request and
	// the extra replies it costs on a limited plan (docs/web-cabinet-ai-context.md §3).
	Account      aicontext.Rendered
	AccountExtra int
}

// Result — клиентке қайтатын нәтиже.
type Result struct {
	DailyCreditGrant   int
	CreditsNeverExpire bool
	Text               string
	DetectedLanguage   string
	Model              string
	InputTokens        int
	OutputTokens       int
	DailyLimit         int
	Unlimited          bool
	UsedToday          int
	Remaining          int
	// MonthlyLimit — 0 болса айлық шек жоқ; UsedMonth — осы айда жұмсалғаны.
	MonthlyLimit int
	UsedMonth    int
	ResetsAt     time.Time
	LatencyMS    int
	// SourceLimit — ErrSourceTooLong кезінде клиентке нақты шекті айту үшін.
	SourceLimit int
	// InstructionLimit — compose нұсқауы шектен асқанда (ErrInstructionTooLong).
	InstructionLimit int
	// Balance — тарифтен тыс қалдық осы сұраныстан кейін (лимит қатесінде — бос).
	Balance domain.CreditBalance
	// Bucket — генерацияны не төледі: daily | pass | welcome | admin | pack
	// (кілт қайталанғанда — алғашқы төлемнің бөлігі).
	Bucket string
	// Units — replies this request charged: 1 + the AI-context surcharge, 0 for
	// a free replay of a paid Idempotency-Key.
	Units int
	// ContextApplied — the account AI context was in the prompt.
	ContextApplied bool
}

// Сұраныс режимдері. Usage оқиғасында сақталады; reply мен compose бір
// квотаны жұмсайды, polish пен transcribe квотаға кірмейді.
const (
	ModeReply      = "reply"
	ModeCompose    = "compose"
	ModePolish     = "polish"
	ModeTranscribe = "transcribe"
	// ModeContext — an extra reservation paying the account AI-context
	// surcharge of a reply or compose (usage_reservations.mode only).
	ModeContext = "context"
)

// bookkeepingTimeout — провайдерден кейінгі есепке (квотаны қайтару, токендер, оқиға) берілетін уақыт.
const bookkeepingTimeout = 5 * time.Second

// bookkeeping — провайдер жауап бергеннен кейінгі жазбаларға арналған контекст.
//
// The request context is cancelled when the client goes away — exactly what
// happens when a keyboard is closed while the provider is still writing. The
// refund, the token counters and the usage event must still be written, so
// they run on a context that keeps the request's values but not its
// cancellation, bounded by a short timeout of its own.
func bookkeeping(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
}

// call — генерацияның метадерегі: кім, қай режим, қай құрылғыдан. Мәтін емес.
type call struct {
	Mode       string
	User       domain.User
	DeviceID   string
	Language   string
	Platform   string
	AppVersion string
	// Chars — пайдаланушы мәтінінің ұзындығы (хабарлама не нұсқау), тек сан.
	Chars int
	// Key — Idempotency-Key ("" — кілтсіз).
	Key string
	// Fallback — the same request without the account AI context, used when the
	// balance cannot pay the context surcharge; nil when there is no context.
	Fallback *Prompt
	// Extra — extra replies the account context costs on a limited plan.
	Extra int
	// ContextChars — characters of the account context in the prompt.
	ContextChars int
	// Units — what the usage event records as charged (set by generate).
	Units int
	// MaxUnits and Policy — a screenshot reply's approved maximum and the
	// credit policy that priced it.
	MaxUnits int
	Policy   string
}

// Reply — негізгі сценарий.
//
// Order matters: quota is reserved BEFORE the provider call, so parallel
// requests cannot oversubscribe a plan, and refunded when the provider fails,
// so a user is never charged for a reply they did not receive. Neither the
// source text nor the generated reply is written anywhere — not to the
// database, not to the log, not to the usage event.
func (s *Service) Reply(ctx context.Context, req Request) (Result, error) {
	started := s.clock.Now()

	// One read per request: the whole call works against the same limits
	// even if an administrator changes them while it is in flight.
	lim := s.limits.Current(ctx)
	mac, err := s.validateMacGeneration(req.SessionPlatform, req.Context, req.OutputCharLimit, "")
	if err != nil {
		return Result{}, err
	}

	source := strings.TrimSpace(req.SourceText)
	if source == "" {
		return Result{}, domain.ErrInvalidRequest
	}
	if traits.RuneLen(source) > lim.SourceChars {
		return Result{SourceLimit: lim.SourceChars}, domain.ErrSourceTooLong
	}
	req.Instruction = traits.Clamp(req.Instruction, lim.InstructionLimit(req.Voice))

	input := PromptInput{
		Message:       source,
		Instruction:   req.Instruction,
		TemplateID:    req.TemplateID,
		AppLanguage:   req.Language,
		InputLanguage: req.InputLanguage,
		Profile:       req.Profile,
		Template:      req.Template,
		WorkingHours:  req.Business,
	}
	base := BuildPrompt(input)
	base.MaxOutputTokens = lim.MaxOutputTokens
	var full Prompt
	if req.Account.Present() {
		input.Account = req.Account
		full = BuildPrompt(input)
		full.MaxOutputTokens = lim.MaxOutputTokens
	}
	prompt, fallback, err := s.withAccount(base, full, req.Account.Present(), mac, req.Context, req.OutputCharLimit)
	if err != nil {
		return Result{}, err
	}

	return s.generate(ctx, call{
		Mode:         ModeReply,
		User:         req.User,
		DeviceID:     req.DeviceID,
		Language:     req.Language,
		Platform:     req.Platform,
		AppVersion:   req.AppVersion,
		Chars:        traits.RuneLen(source),
		Key:          req.IdempotencyKey,
		Fallback:     fallback,
		Extra:        req.AccountExtra,
		ContextChars: req.Account.Chars,
	}, prompt, started)
}

// withAccount — the prompt to send and, when it carries the account AI
// context, the fallback without it. base is the request's prompt without the
// account context, full the same with it (only when present). A desktop's own
// context is added to both; its byte budget counts the desktop's text only.
func (s *Service) withAccount(base, full Prompt, present, mac bool, c *GenerationContext, output int) (Prompt, *Prompt, error) {
	var err error
	if !present {
		if mac {
			base, err = s.addMacGeneration(base, c, output, 0)
		}
		return base, nil, err
	}
	if mac {
		accountBytes := len(full.User) - len(base.User)
		if base, err = s.addMacGeneration(base, c, output, 0); err != nil {
			return Prompt{}, nil, err
		}
		if full, err = s.addMacGeneration(full, c, output, accountBytes); err != nil {
			return Prompt{}, nil, err
		}
	}
	return full, &base, nil
}

// generate — reply мен compose-қа ортақ жол: квота брондау → провайдер, тазарту,
// тексеру, қажет болса түзету (Complete) → токен есебі → оқиға → журнал.
// Түзету болса да квота бір рет жұмсалады, ал екі шақырудың токені есептеледі.
//
// The reservation picks the bucket (plan, pass, welcome, admin, pack) and is
// committed once the text is ready or refunded to the same bucket when it is
// not. A key that already paid generates again without a charge (Replay); its
// failure only gives the free regeneration back. Neither the text nor the
// key's outcome is cached: a replay is a fresh generation.
func (s *Service) generate(ctx context.Context, c call, prompt Prompt, started time.Time) (Result, error) {
	entitlement, err := s.subs.Entitlement(ctx, c.User.ID)
	if err != nil {
		return Result{}, err
	}
	// Pro pays no AI-context surcharge, also on an extra device that falls
	// back to the account's purchased packs (the cabinet promises 1 a reply).
	unlimitedAccount := entitlement.Unlimited
	var deviceLimit error
	if s.proDevices != nil {
		if err := s.proDevices.Authorize(ctx, c.User, c.DeviceID, entitlement); err != nil {
			if !purchasedDeviceFallback(entitlement, err) {
				return Result{}, err
			}
			// An occupied Pro slot restricts only unlimited benefits. Explicitly
			// purchased replies stay shared across the account's devices.
			deviceLimit = err
			entitlement = purchasedOnlyEntitlement(entitlement)
		}
	}
	date, month := s.subs.Keys(started)

	res, err := s.repo.ReserveGeneration(ctx, repository.ReserveInput{
		UserID: c.User.ID, Key: c.Key, Mode: c.Mode, Date: date, Month: month,
		DailyLimit: entitlement.DailyLimit, MonthlyLimit: entitlement.MonthlyLimit, Unlimited: entitlement.Unlimited, Now: started,
		PurchasedOnly: deviceLimit != nil,
	})
	if err != nil {
		if !isQuotaRefusal(err) {
			// A duplicate key or a database error: nothing was charged and
			// nothing reached the provider.
			return Result{}, err
		}
		if deviceLimit != nil {
			// Preserve the device denial when no purchased reply (or bounded
			// retry of an already paid pack reply) is available. This is not an
			// exhausted Pro subscription and must not send quota notices.
			return Result{}, deviceLimit
		}
		// Every bucket was empty at that moment; only the pass window remains.
		entitlement.Balance = entitlement.Balance.Spent()
		s.record(ctx, c, entitlement, "error", errorCode(err), prompt.Version, Completion{}, 0)
		s.quotaRefused(ctx, c.User, entitlement, date, month, err)
		return Result{
			DailyLimit:   entitlement.DailyLimit,
			UsedToday:    entitlement.UsedToday,
			MonthlyLimit: entitlement.MonthlyLimit,
			UsedMonth:    entitlement.UsedMonth,
			ResetsAt:     entitlement.ResetsAt,
			Balance:      entitlement.Balance,
		}, err
	}

	// The account AI-context surcharge: c.Extra more units, reserved all or
	// none in one transaction by the normal bucket order. Pro and free replays
	// pay none. When the balance cannot pay it, the reply is written without
	// the account context for the one reply already reserved. A replay uses
	// the context only if its key's first generation paid for it.
	applied := c.Fallback != nil
	last := res
	var extras []repository.Reservation
	switch {
	case !applied || c.Extra == 0 || unlimitedAccount:
	case res.Replay:
		if res.ContextUnits < c.Extra {
			prompt, applied = *c.Fallback, false
		}
	default:
		extras, err = s.repo.ReserveContextUnits(ctx, res.ID, repository.ReserveInput{
			UserID: c.User.ID, Mode: ModeContext, Date: date, Month: month,
			DailyLimit: entitlement.DailyLimit, MonthlyLimit: entitlement.MonthlyLimit, Unlimited: entitlement.Unlimited,
			Now: started, PurchasedOnly: deviceLimit != nil,
		}, c.Extra)
		switch {
		case err == nil:
			last = extras[len(extras)-1]
		case isQuotaRefusal(err):
			prompt, applied = *c.Fallback, false
		default:
			// A database error or a client that went away: give the one
			// reserved reply back on a context that outlives the request.
			bctx, cancel := bookkeeping(ctx)
			s.release(bctx, c, res)
			cancel()
			return Result{}, err
		}
	}
	c.Units = 1 + len(extras)
	if res.Replay {
		c.Units = 0
	}
	if !applied {
		c.ContextChars = 0
	}

	outcome, providerErr := Complete(ctx, s.provider, prompt, s.repair)
	latency := int(s.clock.Now().Sub(started).Milliseconds())

	// From here on the request may already be cancelled: what is written
	// below must be written anyway.
	ctx, cancel := bookkeeping(ctx)
	defer cancel()

	if providerErr != nil {
		// Жауап алынбады — бронды қайтарамыз (қайталау кезінде екі рет есептелмейді).
		s.release(ctx, c, res)
		s.refundExtras(ctx, c, extras)
		c.Units = 0
		completion := Completion{Model: outcome.Model, InputTokens: outcome.InputTokens, OutputTokens: outcome.OutputTokens, ProviderMS: outcome.ProviderMS}
		if completion.InputTokens > 0 || completion.OutputTokens > 0 {
			if err := s.repo.AddTokens(ctx, c.User.ID, date, month, completion.InputTokens, completion.OutputTokens, s.estimateCostMicros(ctx, completion)); err != nil {
				s.log.Error("token accounting failed", "user_id", c.User.ID, "error", err.Error())
			}
		}
		s.record(ctx, c, entitlement, "error", errorCode(providerErr), prompt.Version, completion, latency)
		s.logFailure(c, prompt.Version, providerErr, latency)
		return Result{}, providerErr
	}
	if !res.Replay {
		for _, reservation := range append([]repository.Reservation{res}, extras...) {
			if ok, err := s.repo.CommitGeneration(ctx, reservation.ID, s.clock.Now()); err != nil {
				s.log.Error("generation commit failed", "user_id", c.User.ID, "error", err.Error())
			} else if !ok {
				// The stale sweep gave it back already: the person got this one for free.
				s.log.Warn("generation commit found no reservation", "user_id", c.User.ID, "bucket", reservation.Bucket)
			}
		}
	}
	if outcome.RepairAttempted {
		s.log.Info("ai_reply_repaired", "user_id", c.User.ID, "mode", c.Mode,
			"issues", issueCodes(outcome.Issues), "accepted", outcome.Repaired, "code", outcome.RepairError)
	}

	completion := Completion{Model: outcome.Model, InputTokens: outcome.InputTokens,
		OutputTokens: outcome.OutputTokens, ProviderMS: outcome.ProviderMS}
	if err := s.repo.AddTokens(ctx, c.User.ID, date, month, completion.InputTokens,
		completion.OutputTokens, s.estimateCostMicros(ctx, completion)); err != nil {
		s.log.Error("token accounting failed", "user_id", c.User.ID, "error", err.Error())
	}
	s.record(ctx, c, entitlement, "success", "", outcome.Version, completion, latency)
	s.log.Info("ai_reply_generated", "user_id", c.User.ID, "mode", c.Mode, "prompt_version", outcome.Version,
		"target_language", prompt.Quality.Target.Lang, "language_source", prompt.Quality.Target.Source,
		"platform", c.Platform, "app_version", c.AppVersion, "latency_ms", latency,
		"input_tokens", completion.InputTokens, "output_tokens", completion.OutputTokens,
		"repaired", outcome.Repaired, "truncated", outcome.Truncated,
		"bucket", res.Bucket, "replay", res.Replay)

	usage := entitlement
	usage.UsedToday, usage.UsedMonth, usage.Balance = last.UsedToday, last.UsedMonth, last.Balance
	if s.quota != nil && !res.Replay && !entitlement.Unlimited && deviceLimit == nil {
		s.quota.QuotaUsed(ctx, c.User, usage, date, month)
	}

	return Result{
		Text:               outcome.Text,
		DailyCreditGrant:   entitlement.DailyCreditGrant,
		CreditsNeverExpire: entitlement.CreditsNeverExpire,
		DetectedLanguage:   DetectLanguage(outcome.Text),
		Model:              outcome.Model,
		InputTokens:        outcome.InputTokens,
		OutputTokens:       outcome.OutputTokens,
		DailyLimit:         entitlement.DailyLimit,
		Unlimited:          entitlement.Unlimited,
		UsedToday:          usage.UsedToday,
		Remaining:          usage.Remaining(),
		MonthlyLimit:       entitlement.MonthlyLimit,
		UsedMonth:          usage.UsedMonth,
		ResetsAt:           entitlement.ResetsAt,
		LatencyMS:          latency,
		Balance:            usage.Balance,
		Bucket:             res.Bucket,
		Units:              c.Units,
		ContextApplied:     applied,
	}, nil
}

// refundExtras — gives back the context-surcharge reservations of a request
// that delivered nothing (or could not pay the whole surcharge).
func (s *Service) refundExtras(ctx context.Context, c call, extras []repository.Reservation) {
	for _, extra := range extras {
		if _, err := s.repo.RefundGeneration(ctx, extra.ID, s.clock.Now()); err != nil {
			s.log.Error("context surcharge refund failed", "user_id", c.User.ID, "bucket", extra.Bucket, "error", err.Error())
		}
	}
}

// release — жауап жоқ: брон төлеген бөлікке қайтады, тегін қайталау кілтке қайтарылады.
func (s *Service) release(ctx context.Context, c call, res repository.Reservation) {
	if res.Replay {
		if err := s.repo.ReleaseReplay(ctx, res.ID, s.clock.Now()); err != nil {
			s.log.Error("replay release failed", "user_id", c.User.ID, "error", err.Error())
		}
		return
	}
	if _, err := s.repo.RefundGeneration(ctx, res.ID, s.clock.Now()); err != nil {
		s.log.Error("quota refund failed", "user_id", c.User.ID, "bucket", res.Bucket, "error", err.Error())
	}
}

// isQuotaRefusal — барлық бөлік бос (DAILY_LIMIT_REACHED / MONTHLY_LIMIT_REACHED).
func isQuotaRefusal(err error) bool {
	return errors.Is(err, domain.ErrDailyLimit) || errors.Is(err, domain.ErrMonthlyLimit)
}

// quotaRefused — брон лимитке тірелді: квота бітті деген хабарлама (сол күннің кілтімен,
// сондықтан сәтті соңғы генерациядан кейінгі хабармен қайталанбайды).
func (s *Service) quotaRefused(ctx context.Context, user domain.User, ent domain.Entitlement, date, month string, err error) {
	if s.quota == nil {
		return
	}
	switch {
	case errors.Is(err, domain.ErrDailyLimit):
		ent.UsedToday = max(ent.UsedToday, ent.DailyLimit)
	case errors.Is(err, domain.ErrMonthlyLimit):
		ent.UsedMonth = max(ent.UsedMonth, ent.MonthlyLimit)
	default:
		return
	}
	s.quota.QuotaUsed(ctx, user, ent, date, month)
}

// logFailure — сәтсіз генерация: тек режим, нұсқа, код және кідіріс.
func (s *Service) logFailure(c call, version string, err error, latency int) {
	s.log.Warn("ai_reply_failed", "user_id", c.User.ID, "mode", c.Mode, "prompt_version", version,
		"code", errorCode(err), "latency_ms", latency, providerAlert(err))
}

// providerAlert — провайдер жағындағы сәтсіздік (таймаут, 429, 5xx, бос жауап) операторға
// WARNING ретінде жетеді; квота мен пайдаланушы қателері жетпейді.
func providerAlert(err error) slog.Attr {
	if errors.Is(err, context.Canceled) {
		return slog.Attr{} // the client went away; OpenAI is fine
	}
	switch errorCode(err) {
	case "AI_TIMEOUT", "RATE_LIMITED", "AI_PROVIDER_UNAVAILABLE", "AI_EMPTY_RESPONSE", "INTERNAL_ERROR":
		return logging.Alert()
	}
	return slog.Attr{}
}

// record — оқиға метадерегі. Пайдаланушы мәтіні де, жауап та жазылмайды: тек ұзындығы.
func (s *Service) record(ctx context.Context, c call, ent domain.Entitlement,
	status, code, version string, completion Completion, latency int) {
	model := completion.Model
	if model == "" {
		model = s.provider.Model()
	}
	event := domain.UsageEvent{
		UserID:        c.User.ID,
		DeviceID:      c.DeviceID,
		PlanID:        ent.Plan.ID,
		Model:         model,
		Status:        status,
		ErrorCode:     code,
		InputTokens:   completion.InputTokens,
		OutputTokens:  completion.OutputTokens,
		TotalTokens:   completion.InputTokens + completion.OutputTokens,
		CostMicros:    s.estimateCostMicros(ctx, completion),
		LatencyMS:     latency,
		ProviderMS:    completion.ProviderMS,
		Platform:      c.Platform,
		AppVersion:    c.AppVersion,
		Language:      domain.NormalizeLocale(c.Language),
		SourceChars:   c.Chars,
		Mode:          c.Mode,
		PromptVersion: version,
		CreatedAt:     s.clock.Now(),
	}
	event.CachedTokens, event.ReasoningTokens = completion.CachedTokens, completion.ReasoningTokens
	event.MaxUnits, event.CreditPolicy = c.MaxUnits, c.Policy
	if status == "success" {
		event.Units, event.ContextChars = c.Units, c.ContextChars
	}
	if err := s.repo.InsertUsageEvent(ctx, event); err != nil {
		s.log.Error("usage event insert failed", "error", err.Error())
	}
}

// estimateCostMicros — болжамды құн (микро-АҚШ доллары). Баға дерекқорда.
func (s *Service) estimateCostMicros(ctx context.Context, c Completion) int64 {
	if c.InputTokens == 0 && c.OutputTokens == 0 {
		return 0
	}
	model := c.Model
	if model == "" {
		model = s.provider.Model()
	}
	pricing, err := s.repo.PricingFor(ctx, model, s.clock.Now())
	if err != nil {
		return 0
	}
	usd := float64(c.InputTokens)/1_000_000*pricing.InputPer1M +
		float64(c.OutputTokens)/1_000_000*pricing.OutputPer1M
	return int64(usd * 1_000_000)
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrDailyLimit):
		return "DAILY_LIMIT_REACHED"
	case errors.Is(err, domain.ErrMonthlyLimit):
		return "MONTHLY_LIMIT_REACHED"
	case errors.Is(err, domain.ErrProviderTimeout):
		return "AI_TIMEOUT"
	case errors.Is(err, domain.ErrRateLimited):
		return "RATE_LIMITED"
	case errors.Is(err, domain.ErrEmptyCompletion):
		return "AI_EMPTY_RESPONSE"
	case errors.Is(err, domain.ErrOutputCharacterLimit):
		return "AI_OUTPUT_LIMIT_EXCEEDED"
	case errors.Is(err, domain.ErrProviderDown):
		return "AI_PROVIDER_UNAVAILABLE"
	case err == nil:
		return ""
	default:
		return "INTERNAL_ERROR"
	}
}
