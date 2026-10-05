package admin

import (
	"context"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// Range — есеп беру кезеңі.
type Range struct {
	Key  string
	From time.Time
	To   time.Time
}

// ParseRange — today | 7d | 30d | month | prev_month | custom.
func ParseRange(key, fromRaw, toRaw string, loc *time.Location, now time.Time) Range {
	local := now.In(loc)
	startOfDay := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)

	switch key {
	case "today":
		return Range{Key: key, From: startOfDay.UTC(), To: now}
	case "7d":
		return Range{Key: key, From: startOfDay.AddDate(0, 0, -6).UTC(), To: now}
	case "month":
		return Range{Key: key, From: time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc).UTC(), To: now}
	case "prev_month":
		first := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
		return Range{Key: key, From: first.AddDate(0, -1, 0).UTC(), To: first.Add(-time.Second).UTC()}
	case "custom":
		from, errFrom := time.ParseInLocation("2006-01-02", fromRaw, loc)
		to, errTo := time.ParseInLocation("2006-01-02", toRaw, loc)
		if errFrom == nil && errTo == nil && !to.Before(from) {
			return Range{Key: key, From: from.UTC(), To: to.AddDate(0, 0, 1).Add(-time.Second).UTC()}
		}
	}
	return Range{Key: "30d", From: startOfDay.AddDate(0, 0, -29).UTC(), To: now}
}

// Dashboard — басқару тақтасының деректері (тек метадерек).
type Dashboard struct {
	Range         Range
	Stats         repository.Stats
	Registrations []repository.Point
	Generations   []repository.Point
	Tokens        []repository.Point
	Cost          []repository.Point
	ActiveUsers   []repository.Point
	PlanMix       []repository.Point
	PlatformMix   []repository.Point
	Errors        []repository.Point
	TopCost       []repository.Point
	AppVersions   []repository.Point
	// ProductEvents — қолданба оқиғаларының саны атауы бойынша.
	ProductEvents []repository.Point
	CostUSD       float64
}

// Dashboard — барлық көрсеткішті жинау.
func (s *Service) Dashboard(ctx context.Context, rng Range) (Dashboard, error) {
	today, _ := s.subs.TodayKeys()
	stats, err := s.repo.Stats(ctx, rng.From, rng.To, today)
	if err != nil {
		return Dashboard{}, err
	}
	d := Dashboard{Range: rng, Stats: stats, CostUSD: float64(stats.CostMicros) / 1_000_000}

	if d.Registrations, err = s.repo.SeriesRegistrations(ctx, rng.From, rng.To); err != nil {
		return Dashboard{}, err
	}
	if d.Generations, err = s.repo.SeriesGenerations(ctx, rng.From, rng.To); err != nil {
		return Dashboard{}, err
	}
	if d.Tokens, err = s.repo.SeriesTokens(ctx, rng.From, rng.To); err != nil {
		return Dashboard{}, err
	}
	if d.Cost, err = s.repo.SeriesCost(ctx, rng.From, rng.To); err != nil {
		return Dashboard{}, err
	}
	if d.ActiveUsers, err = s.repo.SeriesActiveUsers(ctx, rng.From, rng.To); err != nil {
		return Dashboard{}, err
	}
	if d.PlanMix, err = s.repo.DistributionPlans(ctx); err != nil {
		return Dashboard{}, err
	}
	if d.PlatformMix, err = s.repo.DistributionPlatforms(ctx); err != nil {
		return Dashboard{}, err
	}
	if d.Errors, err = s.repo.ErrorBreakdown(ctx, rng.From, rng.To); err != nil {
		return Dashboard{}, err
	}
	if d.TopCost, err = s.repo.TopUsersByCost(ctx, rng.From, rng.To, 10); err != nil {
		return Dashboard{}, err
	}
	if d.AppVersions, err = s.repo.AppVersionBreakdown(ctx); err != nil {
		return Dashboard{}, err
	}
	if d.ProductEvents, err = s.repo.ProductEventCounts(ctx, rng.From, rng.To); err != nil {
		return Dashboard{}, err
	}
	return d, nil
}
