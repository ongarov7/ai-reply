package iap

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap/googleplay"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// Google Play жұмысшысының шектері.
const (
	// consumeGrace — сұраныс өзі тұтынып үлгерсін: жұмысшы бұдан жаңа жолды алмайды.
	consumeGrace = time.Minute
	// consumeGiveUp — Google тұтынылмаған сатып алуды 3 күннен кейін қайтарады; одан кейін қайталау мағынасыз.
	consumeGiveUp = 4 * 24 * time.Hour
	consumeBatch  = 50
	// voidedWindow — Google қайтарылғандарды 30 күн сақтайды; voidedOverlap — әр сұраудың артқа қарауы.
	voidedWindow  = 30*24*time.Hour - time.Hour
	voidedOverlap = 72 * time.Hour
	voidedPages   = 20
	// WorkerInterval — тұтынуды қайталау жиілігі.
	WorkerInterval = 15 * time.Minute
)

// playUnavailable — Google жауап бермеді: клиент кейін қайта жібереді (503 STORE_UNAVAILABLE).
func (s *Service) playUnavailable(err error, userID string) error {
	s.log.Warn("google play verification unavailable", "user_id", userID, "error", err.Error())
	return domain.ErrStoreUnavailable
}

// VerifyGoogle — Play Billing сатып алуы: purchases.products.get, береді, содан кейін тұтынады.
//
// Rules: the token and product id are well-formed, the product is one an
// administrator configured, Google knows the purchase, its
// obfuscatedExternalAccountId is this account's token, it is purchased
// (state 0; pending gives 202 and a pending row bound to the account) and not
// consumed by someone else. The grant comes first and the consume second: a
// failed consume never loses a paid purchase, the worker retries it.
func (s *Service) VerifyGoogle(ctx context.Context, user domain.User, productID, token string) (Result, error) {
	if s.google == nil {
		return Result{}, domain.ErrIAPUnavailable
	}
	if !googlePattern.MatchString(productID) || !validToken(token) {
		return Result{}, domain.PurchaseInvalid(domain.PurchaseReasonMalformed)
	}
	refuse := func(reason string) (Result, error) {
		s.log.Warn("google play purchase refused", "user_id", user.ID, "reason", reason, "product_id", productID)
		return Result{}, domain.PurchaseInvalid(reason)
	}
	product, err := s.repo.IAPProductByStoreID(ctx, domain.ProviderGooglePlay, productID)
	if errors.Is(err, domain.ErrNotFound) {
		return refuse(domain.PurchaseReasonProduct)
	}
	if err != nil {
		return Result{}, err
	}
	p, err := s.google.GetProductPurchase(ctx, productID, token)
	switch {
	case errors.Is(err, googleplay.ErrNotFound):
		return refuse(domain.PurchaseReasonNotFound)
	case err != nil:
		return Result{}, s.playUnavailable(err, user.ID)
	}
	if p.ObfuscatedExternalAccountID == "" || p.ObfuscatedExternalAccountID != s.AccountToken(user.ID) {
		return refuse(domain.PurchaseReasonAccount)
	}
	if p.PurchaseType != nil && *p.PurchaseType == googleplay.TypeRewarded {
		return refuse(domain.PurchaseReasonPurchaseType)
	}
	txn := tokenHash(token)
	record := domain.IAPPurchase{
		UserID: user.ID, Provider: domain.ProviderGooglePlay, Environment: googleEnvironment(p),
		StoreProductID: productID, ProductCode: product.Code, TransactionID: txn, OrderID: p.OrderID,
		Quantity: p.Units(), PurchasedAt: p.PurchasedAt(),
	}
	now := s.clock.Now()
	switch p.PurchaseState {
	case googleplay.StatePurchased:
	case googleplay.StatePending:
		row, err := s.repo.RecordPendingPurchase(ctx, record, now)
		if err != nil {
			return Result{}, err
		}
		if row.Status == domain.PurchaseGranted {
			return Result{Status: StatusAlreadyGranted, Purchase: row}, nil
		}
		s.log.Info("google play purchase pending", "user_id", user.ID, "product", product.Code, "purchase_id", row.ID)
		return Result{Status: StatusPending, Purchase: row}, nil
	case googleplay.StateCanceled:
		if err := s.repo.RejectPendingPurchase(ctx, domain.ProviderGooglePlay, txn, now); err != nil {
			return Result{}, err
		}
		return refuse(domain.PurchaseReasonCanceled)
	default:
		return refuse(domain.PurchaseReasonMalformed)
	}

	if p.ConsumptionState == googleplay.Consumed {
		// Consumed already: by this server after an earlier grant, or by someone else.
		existing, err := s.repo.IAPPurchaseByTransaction(ctx, domain.ProviderGooglePlay, txn)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			return refuse(domain.PurchaseReasonConsumed)
		case err != nil:
			return Result{}, err
		case existing.Status == domain.PurchaseRefunded || existing.Status == domain.PurchaseRevoked:
			return refuse(domain.PurchaseReasonRevoked)
		case existing.UserID != user.ID:
			return Result{}, domain.ErrPurchaseOwnedElsewhere
		case existing.Status != domain.PurchaseGranted:
			return refuse(domain.PurchaseReasonConsumed)
		}
		if existing.ConsumedAt == nil {
			if err := s.repo.MarkPurchaseConsumed(ctx, existing.ID, now); err != nil {
				return Result{}, err
			}
		}
		return Result{Status: StatusAlreadyGranted, Purchase: existing}, nil
	}

	sealed, err := s.sealer.seal(token)
	if err != nil {
		return Result{}, err
	}
	res, err := s.grant(ctx, user, product, record, sealed)
	if err != nil {
		return Result{}, err
	}
	if res.Purchase.ConsumedAt == nil {
		s.consume(ctx, res.Purchase.ID, productID, token)
	}
	return res, nil
}

// googleEnvironment — purchaseType: 0 лицензия тестері, 1 промокод, жоқ — ақылы сатып алу.
func googleEnvironment(p googleplay.ProductPurchase) string {
	if p.PurchaseType != nil {
		switch *p.PurchaseType {
		case googleplay.TypeTest:
			return domain.EnvironmentTest
		case googleplay.TypePromo:
			return domain.EnvironmentPromo
		}
	}
	return domain.EnvironmentProduction
}

// consume — берілгеннен кейін бірден тұтыну; сәтсіз болса жұмысшы қайталайды.
func (s *Service) consume(ctx context.Context, purchaseID, productID, token string) {
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := s.google.Consume(callCtx, productID, token); err != nil {
		s.log.Warn("google play consume failed; the worker retries", "purchase_id", purchaseID, "error", err.Error())
		return
	}
	if err := s.repo.MarkPurchaseConsumed(callCtx, purchaseID, s.clock.Now()); err != nil {
		s.log.Error("google play consume could not be recorded", "purchase_id", purchaseID, "error", err.Error())
	}
}

// RetryConsumes — берілген, бірақ тұтынылмаған Google сатып алуларын тұтынады.
//
// Google refunds a one-time purchase that is neither consumed nor
// acknowledged within three days, so the worker keeps trying while that can
// help. A purchase Google no longer lets anyone consume (refunded, voided) is
// left to PollVoided; its sealed token is dropped. A transient failure stops
// the round: Google is down, the next round tries again.
func (s *Service) RetryConsumes(ctx context.Context) (int, error) {
	if s.google == nil {
		return 0, nil
	}
	now := s.clock.Now()
	rows, err := s.repo.UnconsumedPurchases(ctx, domain.ProviderGooglePlay, now.Add(-consumeGrace), consumeBatch)
	if err != nil {
		return 0, err
	}
	consumed := 0
	for _, row := range rows {
		if ctx.Err() != nil {
			return consumed, ctx.Err()
		}
		if now.Sub(row.CreatedAt) > consumeGiveUp {
			s.log.Warn("google play purchase was never consumed; giving up", "purchase_id", row.ID)
			if err := s.repo.ForgetPurchaseToken(ctx, row.ID, now); err != nil {
				return consumed, err
			}
			continue
		}
		token, err := s.sealer.open(row.SealedToken)
		if err != nil {
			s.log.Error("google play purchase token cannot be opened", "purchase_id", row.ID)
			if err := s.repo.ForgetPurchaseToken(ctx, row.ID, now); err != nil {
				return consumed, err
			}
			continue
		}
		err = s.google.Consume(ctx, row.StoreProductID, token)
		if err == nil {
			if err := s.repo.MarkPurchaseConsumed(ctx, row.ID, now); err != nil {
				return consumed, err
			}
			consumed++
			continue
		}
		var apiErr *googleplay.APIError
		if !errors.As(err, &apiErr) || !apiErr.Permanent() {
			s.log.Warn("google play consume retry failed", "purchase_id", row.ID, "error", err.Error())
			break
		}
		// Refused for good: maybe it was consumed after all, maybe it is gone.
		p, getErr := s.google.GetProductPurchase(ctx, row.StoreProductID, token)
		switch {
		case getErr == nil && p.ConsumptionState == googleplay.Consumed:
			if err := s.repo.MarkPurchaseConsumed(ctx, row.ID, now); err != nil {
				return consumed, err
			}
			consumed++
		case getErr == nil && p.PurchaseState == googleplay.StatePurchased:
			s.log.Warn("google play consume refused; retrying later", "purchase_id", row.ID, "error", err.Error())
		case getErr != nil && !errors.Is(getErr, googleplay.ErrNotFound):
			s.log.Warn("google play purchase check failed", "purchase_id", row.ID, "error", getErr.Error())
		default:
			s.log.Warn("google play purchase cannot be consumed; left to the voided check", "purchase_id", row.ID,
				"error", err.Error())
			if err := s.repo.ForgetPurchaseToken(ctx, row.ID, now); err != nil {
				return consumed, err
			}
		}
	}
	if consumed > 0 {
		s.log.Info("google play purchases consumed by the retry", "count", consumed)
	}
	return consumed, nil
}

// PollVoided — Google қайтарған (voided) сатып алулар: қалдығы алынады.
//
// The first round after a start reads the last 30 days; later rounds look
// back 72 hours, because Google may list a purchase some time after its
// voided time. Revoking is idempotent, so reading a purchase twice changes
// nothing. A voided token this server never saw is recorded as a tombstone
// and never granted.
func (s *Service) PollVoided(ctx context.Context) (int, error) {
	if s.google == nil {
		return 0, nil
	}
	s.pollMu.Lock()
	defer s.pollMu.Unlock()
	now := s.clock.Now()
	since := s.voidedSince
	if floor := now.Add(-voidedWindow); since.IsZero() || since.Before(floor) {
		since = floor
	}
	revoked, page := 0, ""
	for i := 0; i < voidedPages; i++ {
		res, err := s.google.VoidedPurchases(ctx, since, page)
		if err != nil {
			return revoked, err
		}
		for _, v := range res.Purchases {
			if v.PurchaseToken == "" {
				continue
			}
			out, err := s.repo.RevokePurchase(ctx, repository.PurchaseRevocation{
				Provider: domain.ProviderGooglePlay, TransactionID: tokenHash(v.PurchaseToken),
				Status: domain.PurchaseRefunded, Reason: "voided:" + strconv.Itoa(v.VoidedReason),
				At: v.VoidedAt(), Now: now, Tombstone: &domain.IAPPurchase{OrderID: v.OrderID, Quantity: 1},
			})
			if err != nil {
				return revoked, err
			}
			if out.Revoked {
				revoked++
				s.log.Info("iap purchase revoked", "provider", domain.ProviderGooglePlay, "user_id", out.UserID,
					"order_id", v.OrderID, "units_removed", out.UnitsRemoved)
			}
		}
		if res.Next == "" {
			break
		}
		page = res.Next
	}
	s.voidedSince, s.lastPoll = now.Add(-voidedOverlap), now
	return revoked, nil
}

// Run — Google Play фон жұмысшысы: 15 минут сайын тұтынуды қайталау, IAP_GOOGLE_VOIDED_POLL сайын
// қайтарылғандарды тексеру. Google бапталмаса бірден қайтады.
func (s *Service) Run(ctx context.Context) {
	if s.google == nil {
		return
	}
	ticker := time.NewTicker(WorkerInterval)
	defer ticker.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick — жұмысшының бір айналымы (тестте тікелей шақырылады).
func (s *Service) Tick(ctx context.Context) {
	if _, err := s.RetryConsumes(ctx); err != nil && ctx.Err() == nil {
		s.log.Error("google play consume retry failed", "error", err.Error())
	}
	if s.cfg.GoogleVoidedPoll <= 0 {
		return
	}
	s.pollMu.Lock()
	due := s.lastPoll.IsZero() || s.clock.Now().Sub(s.lastPoll) >= s.cfg.GoogleVoidedPoll
	s.pollMu.Unlock()
	if !due {
		return
	}
	if _, err := s.PollVoided(ctx); err != nil && ctx.Err() == nil {
		s.log.Error("google play voided purchases check failed", "error", err.Error())
	}
}
