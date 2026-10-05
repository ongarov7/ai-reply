package ai

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

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

// Service — AI шлюзі: квота → провайдер → сапа → есеп.
type Service struct {
	repo     *repository.Store
	subs     *subscriptions.Service
	provider Provider
	limits   LimitsSource
	log      *slog.Logger
	clock    traits.Clock
	repair   bool
}

// New — шлюз. Тексеруден өтпеген жауапты түзету әдепкіде қосулы.
func New(repo *repository.Store, subs *subscriptions.Service, provider Provider, limits LimitsSource, log *slog.Logger) *Service {
	return &Service{repo: repo, subs: subs, provider: provider, limits: limits, log: log,
		clock: traits.SystemClock{}, repair: true}
}

// WithClock — тестке.
func (s *Service) WithClock(c traits.Clock) *Service { s.clock = c; return s }

// WithRepair — бір реттік түзету сұранысы (AI_REPAIR_ENABLED).
func (s *Service) WithRepair(enabled bool) *Service { s.repair = enabled; return s }

// Request — бір жауап сұранысы. Мәтін тек жадта, тек осы шақыру ішінде болады.
type Request struct {
	User        domain.User
	DeviceID    string
	SourceText  string
	Instruction string
	Language    string
	// InputLanguage — тексерілген пернетақта коды не бос.
	InputLanguage string
	TemplateID    string
	Profile       Profile
	Template      Template
	Business      WorkingHours
	Platform      string
	AppVersion    string
}

// Result — клиентке қайтатын нәтиже.
type Result struct {
	Text             string
	DetectedLanguage string
	Model            string
	InputTokens      int
	OutputTokens     int
	DailyLimit       int
	UsedToday        int
	Remaining        int
	ResetsAt         time.Time
	LatencyMS        int
	// SourceLimit — ErrSourceTooLong кезінде клиентке нақты шекті айту үшін.
	SourceLimit int
	// InstructionLimit — compose нұсқауы шектен асқанда (ErrInstructionTooLong).
	InstructionLimit int
}

// Сұраныс режимдері. Usage оқиғасында сақталады; reply мен compose бір
// квотаны жұмсайды, polish квотаға кірмейді.
const (
	ModeReply   = "reply"
	ModeCompose = "compose"
	ModePolish  = "polish"
)

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

	source := strings.TrimSpace(req.SourceText)
	if source == "" {
		return Result{}, domain.ErrInvalidRequest
	}
	if traits.RuneLen(source) > lim.SourceChars {
		return Result{SourceLimit: lim.SourceChars}, domain.ErrSourceTooLong
	}
	req.Instruction = traits.Clamp(req.Instruction, lim.InstructionChars)

	prompt := BuildPrompt(PromptInput{
		Message:       source,
		Instruction:   req.Instruction,
		TemplateID:    req.TemplateID,
		AppLanguage:   req.Language,
		InputLanguage: req.InputLanguage,
		Profile:       req.Profile,
		Template:      req.Template,
		WorkingHours:  req.Business,
	})
	prompt.MaxOutputTokens = lim.MaxOutputTokens

	return s.generate(ctx, call{
		Mode:       ModeReply,
		User:       req.User,
		DeviceID:   req.DeviceID,
		Language:   req.Language,
		Platform:   req.Platform,
		AppVersion: req.AppVersion,
		Chars:      traits.RuneLen(source),
	}, prompt, started)
}

// generate — reply мен compose-қа ортақ жол: квота брондау → провайдер, тазарту,
// тексеру, қажет болса түзету (Complete) → токен есебі → оқиға → журнал.
// Түзету болса да квота бір рет жұмсалады, ал екі шақырудың токені есептеледі.
func (s *Service) generate(ctx context.Context, c call, prompt Prompt, started time.Time) (Result, error) {
	entitlement, err := s.subs.Entitlement(ctx, c.User.ID)
	if err != nil {
		return Result{}, err
	}
	date, month := s.subs.Keys(started)

	if err := s.repo.ReserveQuota(ctx, c.User.ID, date, month,
		entitlement.DailyLimit, entitlement.MonthlyLimit); err != nil {
		s.record(ctx, c, entitlement, "error", errorCode(err), prompt.Version, Completion{}, 0)
		return Result{
			DailyLimit: entitlement.DailyLimit,
			UsedToday:  entitlement.UsedToday,
			ResetsAt:   entitlement.ResetsAt,
		}, err
	}

	outcome, providerErr := Complete(ctx, s.provider, prompt, s.repair)
	latency := int(s.clock.Now().Sub(started).Milliseconds())

	if providerErr != nil {
		// Жауап алынбады — бронды қайтарамыз (қайталау кезінде екі рет есептелмейді).
		if err := s.repo.RefundQuota(ctx, c.User.ID, date, month); err != nil {
			s.log.Error("quota refund failed", "user_id", c.User.ID, "error", err.Error())
		}
		s.record(ctx, c, entitlement, "error", errorCode(providerErr), prompt.Version, Completion{}, latency)
		s.logFailure(c, prompt.Version, errorCode(providerErr), latency)
		return Result{}, providerErr
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
		"repaired", outcome.Repaired, "truncated", outcome.Truncated)

	usedToday := entitlement.UsedToday + 1
	remaining := entitlement.DailyLimit - usedToday
	if remaining < 0 {
		remaining = 0
	}

	return Result{
		Text:             outcome.Text,
		DetectedLanguage: DetectLanguage(outcome.Text),
		Model:            outcome.Model,
		InputTokens:      outcome.InputTokens,
		OutputTokens:     outcome.OutputTokens,
		DailyLimit:       entitlement.DailyLimit,
		UsedToday:        usedToday,
		Remaining:        remaining,
		ResetsAt:         entitlement.ResetsAt,
		LatencyMS:        latency,
	}, nil
}

// logFailure — сәтсіз генерация: тек режим, нұсқа, код және кідіріс.
func (s *Service) logFailure(c call, version, code string, latency int) {
	s.log.Warn("ai_reply_failed", "user_id", c.User.ID, "mode", c.Mode, "prompt_version", version,
		"code", code, "latency_ms", latency)
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
	case errors.Is(err, domain.ErrProviderDown):
		return "AI_PROVIDER_UNAVAILABLE"
	case err == nil:
		return ""
	default:
		return "INTERNAL_ERROR"
	}
}
