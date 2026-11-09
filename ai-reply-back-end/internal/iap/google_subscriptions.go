package iap

import (
	"context"
	"errors"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap/googleplay"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// Google Play subscription reads and retries.
const (
	subscriptionBatch = 100
	// renewalWindow — the worker starts reading a subscription this long before it ends.
	renewalWindow = 24 * time.Hour
	// renewalWatch — around the end of a period it is read every round, then hourly.
	renewalWatch = time.Hour
	// suspendedRecheck — on hold, paused, or long past its end: payment can still recover.
	suspendedRecheck = 6 * time.Hour
	maxReadBackoff   = 6 * time.Hour
	// googleKeepsExpired — Google answers about an expired subscription for 60 days.
	googleKeepsExpired = 60 * 24 * time.Hour
	// graceRead — a grace period Google reports without a future end is valid until the next read.
	graceRead = time.Hour
)

// grantsAccess — the states that give the plan while the period lasts.
func grantsAccess(state string) bool {
	return state == domain.GoogleSubActive || state == domain.GoogleSubGrace || state == domain.GoogleSubCanceled
}

// nextSubscriptionRead — when the worker should look at this subscription again.
func nextSubscriptionRead(state string, expires, now time.Time) *time.Time {
	var at time.Time
	left := expires.Sub(now)
	switch {
	case state == domain.GoogleSubExpired:
		return nil
	case state == domain.GoogleSubOnHold || state == domain.GoogleSubPaused:
		at = now.Add(suspendedRecheck)
	case left > renewalWindow:
		at = expires.Add(-renewalWindow)
	case left > renewalWatch:
		at = now.Add(renewalWatch)
	case left > -renewalWatch:
		at = now
	default:
		at = now.Add(suspendedRecheck)
	}
	return &at
}

// readBackoff — after n failed reads in a row.
func readBackoff(n int) time.Duration {
	if n > 16 {
		n = 16
	}
	return min(WorkerInterval<<n, maxReadBackoff)
}

// verifyGoogleSubscription — purchases.subscriptionsv2.get; the same order of
// checks as a one-time purchase. The grant comes first and the acknowledgement
// second: a failed acknowledgement never loses a paid purchase, the worker retries it.
func (s *Service) verifyGoogleSubscription(ctx context.Context, user domain.User, product domain.IAPProduct, productID, token string) (Result, error) {
	refuse := func(reason string) (Result, error) {
		s.log.Warn("google play subscription refused", "user_id", user.ID, "reason", reason, "product_id", productID)
		return Result{}, domain.PurchaseInvalid(reason)
	}
	p, err := s.google.GetSubscription(ctx, token)
	switch {
	case errors.Is(err, googleplay.ErrNotFound):
		return refuse(domain.PurchaseReasonNotFound)
	case err != nil:
		return Result{}, s.playUnavailable(err, user.ID)
	}
	item, ok := p.LineItem(productID)
	if !ok {
		return refuse(domain.PurchaseReasonProduct)
	}
	accountToken := s.AccountToken(user.ID)
	if accountToken == "" {
		return Result{}, domain.ErrStoreUnavailable
	}
	txn := tokenHash(token)
	bound, err := s.boundToAccount(ctx, p, user.ID, accountToken, txn)
	if err != nil {
		return Result{}, err
	}
	if !bound {
		return refuse(domain.PurchaseReasonAccount)
	}
	environment := domain.EnvironmentProduction
	if p.TestPurchase != nil {
		environment = domain.EnvironmentTest
		if !s.cfg.GoogleAcceptTest {
			return refuse(domain.PurchaseReasonEnvironment)
		}
	}
	plan, err := s.repo.Plan(ctx, product.PlanID)
	if err != nil {
		return Result{}, err
	}
	if plan.IsFree {
		return refuse(domain.PurchaseReasonProduct)
	}

	now := s.clock.Now()
	expires := item.Expires()
	state := p.SubscriptionState
	switch {
	case state == domain.GoogleSubPending:
		record := domain.IAPPurchase{UserID: user.ID, Provider: domain.ProviderGooglePlay, Environment: environment,
			StoreProductID: productID, ProductCode: product.Code, TransactionID: txn, OriginalTransactionID: txn,
			OrderID: p.OrderID(item), Quantity: 1}
		row, err := s.repo.RecordPendingPurchase(ctx, record, now)
		if err != nil {
			return Result{}, err
		}
		if row.Status == domain.PurchaseGranted {
			return Result{Status: StatusAlreadyGranted, Purchase: row}, nil
		}
		s.log.Info("google play subscription pending", "user_id", user.ID, "product", product.Code, "purchase_id", row.ID)
		return Result{Status: StatusPending, Purchase: row}, nil
	case state == domain.GoogleSubPendingCanceled:
		if err := s.repo.RejectPendingPurchase(ctx, domain.ProviderGooglePlay, txn, now); err != nil {
			return Result{}, err
		}
		return refuse(domain.PurchaseReasonCanceled)
	case grantsAccess(state) && expires.IsZero():
		return refuse(domain.PurchaseReasonMalformed)
	case state == domain.GoogleSubGrace && !expires.After(now):
		expires = now.Add(graceRead)
	case !grantsAccess(state):
		if err := s.storeGoogleState(ctx, txn, p, item, now); err != nil {
			return Result{}, err
		}
		switch state {
		case domain.GoogleSubOnHold:
			return refuse(domain.PurchaseReasonOnHold)
		case domain.GoogleSubPaused:
			return refuse(domain.PurchaseReasonPaused)
		case domain.GoogleSubExpired:
			return refuse(domain.PurchaseReasonExpired)
		}
		return refuse(domain.PurchaseReasonMalformed)
	case !expires.After(now):
		// CANCELED or ACTIVE, but this item's period is over: no access now,
		// and only Google's own EXPIRED ends the reading of it.
		if err := s.storeGoogleState(ctx, txn, p, item, now); err != nil {
			return Result{}, err
		}
		return refuse(domain.PurchaseReasonExpired)
	}

	sealed, err := s.sealer.seal(token)
	if err != nil {
		return Result{}, err
	}
	in := repository.GoogleSubscription{TokenHash: txn, SealedToken: sealed, UserID: user.ID, ProductID: productID, ProductCode: product.Code,
		PlanID: product.PlanID, Environment: environment, OrderID: p.OrderID(item), State: state, ExpiresAt: expires, StartedAt: p.Started(),
		AutoRenew: item.AutoRenews(), Acknowledged: p.Acknowledged(), NextCheckAt: nextSubscriptionRead(state, expires, now)}
	if in.StartedAt.IsZero() {
		in.StartedAt = now
	}
	if p.LinkedPurchaseToken != "" {
		in.LinkedTokenHash = tokenHash(p.LinkedPurchaseToken)
	}
	// As with a one-time grant, a client that disconnects must not leave a paid purchase half-recorded.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	res, err := s.repo.ApplyGoogleSubscription(writeCtx, in, now)
	if err != nil {
		if errors.Is(err, domain.ErrPurchaseOwnedElsewhere) {
			s.log.Warn("google play subscription belongs to another account", "user_id", user.ID, "product", product.Code)
		}
		return Result{}, err
	}
	if res.Superseded {
		return Result{Status: StatusAlreadyGranted, Purchase: res.Purchase}, nil
	}
	if res.Revoked {
		return refuse(domain.PurchaseReasonRevoked)
	}
	if !res.Acknowledged {
		s.acknowledgeSubscription(ctx, txn, productID, token)
	}
	status := StatusAlreadyGranted
	if res.Granted {
		status = StatusGranted
		s.log.Info("google play subscription granted", "user_id", user.ID, "product", product.Code, "environment", environment, "purchase_id", res.Purchase.ID)
	}
	return Result{Status: status, Purchase: res.Purchase}, nil
}

// boundToAccount — this purchase is this account's.
//
// A purchase made in the app names its account. A re-subscription from the Play
// subscriptions center does not: only the expired subscription it continues
// says whose it is, and Google stops saying so after the acknowledgement, when
// the row written for it holds the binding instead.
func (s *Service) boundToAccount(ctx context.Context, p googleplay.Subscription, userID, accountToken, txn string) (bool, error) {
	if id := p.AccountID(); id != "" {
		return id == accountToken, nil
	}
	if c := p.OutOfAppPurchaseContext; c != nil {
		if id := c.ExpiredAccountID(); id != "" {
			return id == accountToken, nil
		}
		if c.ExpiredPurchaseToken != "" {
			held, err := s.repo.GoogleSubscriptionHeldBy(ctx, tokenHash(c.ExpiredPurchaseToken), userID)
			if held || err != nil {
				return held, err
			}
		}
	}
	return s.repo.GoogleSubscriptionHeldBy(ctx, txn, userID)
}

// storeGoogleState — the subscription gives nothing now; a known row follows Google.
func (s *Service) storeGoogleState(ctx context.Context, txn string, p googleplay.Subscription, item googleplay.SubscriptionLineItem, now time.Time) error {
	if item.Expires().IsZero() {
		return nil
	}
	state := p.SubscriptionState
	return s.repo.UpdateGoogleSubscriptionState(ctx, repository.GoogleSubscriptionState{TokenHash: txn, State: state, ExpiresAt: item.Expires(),
		AutoRenew: item.AutoRenews(), NextCheckAt: nextSubscriptionRead(state, item.Expires(), now), DropToken: state == domain.GoogleSubExpired}, now)
}

// acknowledgeSubscription — right after the grant; on failure the worker retries.
func (s *Service) acknowledgeSubscription(ctx context.Context, txn, productID, token string) {
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := s.google.AcknowledgeSubscription(callCtx, productID, token); err != nil {
		s.log.Warn("google play acknowledgement failed; the worker retries", "error", err.Error())
		return
	}
	if err := s.repo.MarkGoogleSubscriptionAcknowledged(callCtx, txn, s.clock.Now()); err != nil {
		s.log.Error("google play acknowledgement could not be recorded", "error", err.Error())
	}
}

// RetryAcknowledges — granted subscriptions Google has not been told about.
//
// Google refunds a subscription that is not acknowledged within three days, so
// the worker keeps trying while that can help. Refused for good, the purchase
// is read again to see why. A transient failure stops the round.
func (s *Service) RetryAcknowledges(ctx context.Context) (int, error) {
	if s.google == nil {
		return 0, nil
	}
	now := s.clock.Now()
	rows, err := s.repo.UnacknowledgedGoogleSubscriptions(ctx, now.Add(-consumeGrace), consumeBatch)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, row := range rows {
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		if now.Sub(row.GrantedAt) > consumeGiveUp {
			s.log.Warn("google play subscription was never acknowledged; giving up")
			if err := s.repo.AbandonGoogleAcknowledge(ctx, row.TokenHash, now); err != nil {
				return done, err
			}
			continue
		}
		token, err := s.sealer.open(row.SealedToken)
		if err != nil {
			s.log.Error("google play subscription token cannot be opened")
			if err := s.repo.AbandonGoogleAcknowledge(ctx, row.TokenHash, now); err != nil {
				return done, err
			}
			continue
		}
		err = s.google.AcknowledgeSubscription(ctx, row.ProductID, token)
		if err == nil {
			if err := s.repo.MarkGoogleSubscriptionAcknowledged(ctx, row.TokenHash, now); err != nil {
				return done, err
			}
			done++
			continue
		}
		var apiErr *googleplay.APIError
		if !errors.As(err, &apiErr) || !apiErr.Permanent() {
			s.log.Warn("google play acknowledgement retry failed", "error", err.Error())
			break
		}
		p, getErr := s.google.GetSubscription(ctx, token)
		switch {
		case getErr == nil && p.Acknowledged():
			if err := s.repo.MarkGoogleSubscriptionAcknowledged(ctx, row.TokenHash, now); err != nil {
				return done, err
			}
			done++
		case getErr == nil && grantsAccess(p.SubscriptionState):
			s.log.Warn("google play acknowledgement refused; retrying later", "error", err.Error())
		case getErr != nil && !errors.Is(getErr, googleplay.ErrNotFound):
			s.log.Warn("google play subscription check failed", "error", getErr.Error())
		default:
			s.log.Warn("google play subscription cannot be acknowledged", "error", err.Error())
			if err := s.repo.AbandonGoogleAcknowledge(ctx, row.TokenHash, now); err != nil {
				return done, err
			}
		}
	}
	if done > 0 {
		s.log.Info("google play subscriptions acknowledged by the retry", "count", done)
	}
	return done, nil
}

// RefreshSubscriptions — renewal and loss. Google sends this server no push yet,
// so subscriptions near or past their end are read again.
//
// A bounded batch, oldest first. A read that fails transiently changes no access
// state, backs that row off and ends the round: while Google is down the worker
// makes one failed call per round and access is decided by the last good read.
func (s *Service) RefreshSubscriptions(ctx context.Context) (int, error) {
	if s.google == nil {
		return 0, nil
	}
	now := s.clock.Now()
	rows, err := s.repo.DueGoogleSubscriptions(ctx, now, subscriptionBatch)
	if err != nil {
		return 0, err
	}
	updated := 0
	for _, row := range rows {
		if ctx.Err() != nil {
			return updated, ctx.Err()
		}
		token, err := s.sealer.open(row.SealedToken)
		if err != nil {
			s.log.Error("google play subscription token cannot be opened")
			if err := s.deferRead(ctx, row, now); err != nil {
				return updated, err
			}
			continue
		}
		p, err := s.google.GetSubscription(ctx, token)
		if errors.Is(err, googleplay.ErrNotFound) {
			if now.Sub(row.ExpiresAt) > googleKeepsExpired {
				if err := s.repo.UpdateGoogleSubscriptionState(ctx, repository.GoogleSubscriptionState{TokenHash: row.TokenHash,
					State: domain.GoogleSubExpired, ExpiresAt: row.ExpiresAt, DropToken: true}, now); err != nil {
					return updated, err
				}
				continue
			}
			s.log.Warn("google play does not know a stored subscription", "product_id", row.ProductID)
			if err := s.deferRead(ctx, row, now); err != nil {
				return updated, err
			}
			continue
		}
		if err != nil {
			s.log.Warn("google play subscription read failed", "error", err.Error())
			if err := s.deferRead(ctx, row, now); err != nil {
				return updated, err
			}
			break
		}
		item, ok := p.LineItem(row.ProductID)
		if !ok || item.Expires().IsZero() {
			s.log.Warn("google play subscription has no line item for its product", "product_id", row.ProductID)
			if err := s.deferRead(ctx, row, now); err != nil {
				return updated, err
			}
			continue
		}
		if err := s.applyRefreshedSubscription(ctx, row, p, item, now); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func (s *Service) deferRead(ctx context.Context, row repository.GoogleDue, now time.Time) error {
	failures := row.Failures + 1
	return s.repo.DeferGoogleSubscription(ctx, row.TokenHash, failures, now.Add(readBackoff(failures)), now)
}

// applyRefreshedSubscription — the worker's side of what a post from the app does.
func (s *Service) applyRefreshedSubscription(ctx context.Context, row repository.GoogleDue, p googleplay.Subscription, item googleplay.SubscriptionLineItem, now time.Time) error {
	state, expires := p.SubscriptionState, item.Expires()
	if state == domain.GoogleSubGrace && !expires.After(now) {
		expires = now.Add(graceRead)
	}
	if !grantsAccess(state) || !expires.After(now) {
		return s.storeGoogleState(ctx, row.TokenHash, p, item, now)
	}
	in := repository.GoogleSubscription{TokenHash: row.TokenHash, UserID: row.UserID, ProductID: row.ProductID, ProductCode: row.ProductCode,
		PlanID: row.PlanID, Environment: row.Environment, OrderID: p.OrderID(item), State: state, ExpiresAt: expires, StartedAt: p.Started(),
		AutoRenew: item.AutoRenews(), Acknowledged: p.Acknowledged(), NextCheckAt: nextSubscriptionRead(state, expires, now)}
	if in.StartedAt.IsZero() {
		in.StartedAt = row.StartedAt
	}
	if p.LinkedPurchaseToken != "" {
		in.LinkedTokenHash = tokenHash(p.LinkedPurchaseToken)
	}
	res, err := s.repo.ApplyGoogleSubscription(ctx, in, now)
	if err != nil {
		var purchaseErr *domain.PurchaseError
		if errors.As(err, &purchaseErr) || errors.Is(err, domain.ErrPurchaseOwnedElsewhere) {
			s.log.Warn("google play subscription read refused", "error", err.Error())
			return s.deferRead(ctx, row, now)
		}
		return err
	}
	if res.Granted {
		s.log.Info("google play subscription renewed", "user_id", row.UserID, "product", row.ProductCode, "purchase_id", res.Purchase.ID)
	}
	return nil
}
