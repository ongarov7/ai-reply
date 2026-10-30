package iap

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
	"github.com/aireply/ai-reply-back-end/internal/iap/appstore"
	"github.com/aireply/ai-reply-back-end/internal/repository"
)

// appleRejected — JWS тексерісінің себебі → PURCHASE_INVALID себебі (журналға тексеру сөзі де).
func (s *Service) appleRejected(err error, userID string) error {
	var r *appstore.Reject
	if !errors.As(err, &r) {
		return err
	}
	s.log.Warn("app store signed data refused", "user_id", userID, "reason", r.Reason, "check", r.Check)
	if r.Reason == appstore.ReasonMalformed {
		return domain.PurchaseInvalid(domain.PurchaseReasonMalformed)
	}
	return domain.PurchaseInvalid(domain.PurchaseReasonSignature)
}

// appleEnvironment — бұл ортаның транзакциясы осы серверде қабылдана ма.
func (s *Service) appleEnvironment(env string) bool {
	switch env {
	case appstore.EnvironmentProduction:
		return !s.cfg.AppleAcceptSandbox
	case appstore.EnvironmentSandbox:
		return s.cfg.AppleAcceptSandbox
	}
	return false
}

// VerifyApple — StoreKit 2 transaction.jwsRepresentation: тексеріп, береді.
//
// Order of checks: Apple's signature and chain, the bundle id, the
// environment (Sandbox only where accepted), the transaction type, the
// account token, revocation, the product mapping. Only then the grant, once
// per transaction (repository.GrantPurchase).
func (s *Service) VerifyApple(ctx context.Context, user domain.User, signed string) (Result, error) {
	if s.apple == nil {
		return Result{}, domain.ErrIAPUnavailable
	}
	tx, err := s.apple.Transaction(signed)
	if err != nil {
		return Result{}, s.appleRejected(err, user.ID)
	}
	refuse := func(reason string) (Result, error) {
		s.log.Warn("app store transaction refused", "user_id", user.ID, "reason", reason, "product_id", tx.ProductID,
			"environment", tx.Environment, "transaction_id", tx.TransactionID)
		return Result{}, domain.PurchaseInvalid(reason)
	}
	switch {
	case tx.BundleID != s.cfg.AppleBundleID:
		return refuse(domain.PurchaseReasonBundle)
	case !s.appleEnvironment(tx.Environment):
		return refuse(domain.PurchaseReasonEnvironment)
	case tx.Type != appstore.TypeConsumable && tx.Type != appstore.TypeNonRenewing && tx.Type != appstore.TypeAutoRenewable:
		return refuse(domain.PurchaseReasonType)
	case tx.Quantity < 0:
		return refuse(domain.PurchaseReasonQuantity)
	case tx.AppAccountToken == "" || !strings.EqualFold(tx.AppAccountToken, s.AccountToken(user.ID)):
		return refuse(domain.PurchaseReasonAccount)
	}
	if tx.RevocationDate != 0 && tx.Type != appstore.TypeAutoRenewable {
		// The store already took it back: whatever this account still holds of it goes too.
		if _, err := s.repo.RevokePurchase(ctx, repository.PurchaseRevocation{
			Provider: domain.ProviderAppStore, TransactionID: tx.TransactionID, Status: domain.PurchaseRefunded,
			Reason: appleRevokeReason("refund", tx), At: time.UnixMilli(tx.RevocationDate), Now: s.clock.Now(),
			SignedAt: time.UnixMilli(tx.SignedDate),
		}); err != nil {
			return Result{}, err
		}
		return refuse(domain.PurchaseReasonRevoked)
	}
	product, err := s.repo.IAPProductByStoreID(ctx, domain.ProviderAppStore, tx.ProductID)
	if errors.Is(err, domain.ErrNotFound) {
		return refuse(domain.PurchaseReasonProduct)
	}
	if err != nil {
		return Result{}, err
	}
	if tx.Type == appstore.TypeAutoRenewable {
		if product.Kind != domain.ProductSubscription {
			return refuse(domain.PurchaseReasonType)
		}
		result, err := s.applyAppleSubscription(ctx, user, product, tx, nil, 0, false, tx.IsUpgraded)
		if err == nil && tx.RevocationDate > 0 {
			return refuse(domain.PurchaseReasonRevoked)
		}
		return result, err
	}
	if product.Kind == domain.ProductSubscription {
		return refuse(domain.PurchaseReasonType)
	}
	var purchased *time.Time
	if tx.PurchaseDate > 0 {
		t := time.UnixMilli(tx.PurchaseDate).UTC()
		purchased = &t
	}
	return s.grant(ctx, user, product, domain.IAPPurchase{
		Provider: domain.ProviderAppStore, Environment: tx.Environment, StoreProductID: tx.ProductID,
		TransactionID: tx.TransactionID, OriginalTransactionID: tx.OriginalTransactionID, Quantity: tx.Quantity,
		PurchasedAt:     purchased,
		PriceMilliunits: tx.Price, Currency: tx.Currency,
	}, "")
}

// appleRevokeReason — «refund» / «revoke» және Apple-дің себеп коды (revocationReason: 0 басқа, 1 ақау).
func appleRevokeReason(kind string, tx appstore.Transaction) string {
	if tx.RevocationReason != nil {
		return kind + ":" + strconv.Itoa(*tx.RevocationReason)
	}
	return kind
}

// Хабарлама нәтижелері (iap_notifications.outcome).
const (
	OutcomeRevoked    = "revoked"
	OutcomeRestored   = "restored"
	OutcomeCleared    = "cleared" // REFUND_REVERSED of a transaction the app had not posted: its tombstone is gone
	OutcomeLogged     = "logged"
	OutcomeIgnored    = "ignored"
	OutcomeUnknown    = "unknown_transaction"
	OutcomeUnchanged  = "unchanged"
	OutcomeDuplicate  = "duplicate"
	outcomeInvalidTxn = "invalid_transaction"
)

// HandleAppleNotification — App Store Server Notifications V2 (signedPayload).
//
// Verified with the same chain as a transaction; anything that does not
// verify is refused. After verification the answer is always a success
// (Apple stops retrying), and a notification UUID is processed once.
// REFUND / REVOKE revoke what is left of the purchase, REFUND_REVERSED gives
// it back (or removes the tombstone an early REFUND left, so the app's post
// is granted), TEST and CONSUMPTION_REQUEST are logged (no App Store Server
// API call is made). The outcome is returned for the log.
func (s *Service) HandleAppleNotification(ctx context.Context, signedPayload string) (string, error) {
	if s.apple == nil {
		return "", domain.ErrIAPUnavailable
	}
	n, err := s.apple.Notification(signedPayload)
	if err != nil {
		return "", s.appleRejected(err, "")
	}
	seen, err := s.repo.IAPNotificationSeen(ctx, domain.ProviderAppStore, n.NotificationUUID)
	if err != nil {
		return "", err
	}
	if seen {
		return OutcomeDuplicate, nil
	}
	outcome, transactionID, err := s.appleNotification(ctx, n)
	if err != nil {
		return "", err
	}
	if _, err := s.repo.InsertIAPNotification(ctx, domain.IAPNotification{
		Provider: domain.ProviderAppStore, NotificationID: n.NotificationUUID, Type: n.NotificationType,
		Subtype: n.Subtype, TransactionID: transactionID, Outcome: outcome, ReceivedAt: s.clock.Now(),
	}); err != nil {
		return "", err
	}
	s.log.Info("app store notification", "type", n.NotificationType, "subtype", n.Subtype, "outcome", outcome,
		"environment", n.Data.Environment, "transaction_id", transactionID)
	return outcome, nil
}

func (s *Service) appleNotification(ctx context.Context, n appstore.Notification) (outcome, transactionID string, err error) {
	if n.Data.BundleID != "" && n.Data.BundleID != s.cfg.AppleBundleID {
		return OutcomeIgnored, "", nil
	}
	if n.Data.Environment != "" && !s.appleEnvironment(n.Data.Environment) {
		return OutcomeIgnored, "", nil
	}
	switch n.NotificationType {
	case appstore.NotificationRefund, appstore.NotificationRevoke, appstore.NotificationRefundReversed:
	case appstore.NotificationTest, appstore.NotificationConsumption, appstore.NotificationRefundDeclined:
		return OutcomeLogged, "", nil
	case appstore.NotificationSubscribed, appstore.NotificationRenewed, appstore.NotificationFailedRenewal,
		appstore.NotificationExpired, appstore.NotificationGraceExpired, appstore.NotificationRenewalStatus, appstore.NotificationRenewalPreference:
	default:
		return OutcomeIgnored, "", nil
	}
	tx, err := s.apple.Transaction(n.Data.SignedTransactionInfo)
	if err != nil {
		_ = s.appleRejected(err, "")
		return outcomeInvalidTxn, "", nil
	}
	if tx.BundleID != s.cfg.AppleBundleID {
		return OutcomeIgnored, tx.TransactionID, nil
	}
	if !s.appleEnvironment(tx.Environment) {
		return OutcomeIgnored, tx.TransactionID, nil
	}
	if tx.Type == appstore.TypeAutoRenewable {
		return s.appleSubscriptionNotification(ctx, n, tx)
	}
	if n.NotificationType != appstore.NotificationRefund && n.NotificationType != appstore.NotificationRevoke && n.NotificationType != appstore.NotificationRefundReversed {
		return OutcomeIgnored, tx.TransactionID, nil
	}
	now := s.clock.Now()
	if n.NotificationType == appstore.NotificationRefundReversed {
		res, err := s.repo.RestoreApplePurchase(ctx, tx.TransactionID, time.UnixMilli(n.SignedDate), now)
		if err != nil {
			return "", "", err
		}
		switch {
		case res.Cleared:
			s.log.Info("iap refund reversed before the transaction was posted", "provider", domain.ProviderAppStore,
				"transaction_id", tx.TransactionID)
			return OutcomeCleared, tx.TransactionID, nil
		case !res.Restored:
			return OutcomeUnchanged, tx.TransactionID, nil
		}
		s.log.Info("iap refund reversed", "provider", domain.ProviderAppStore, "transaction_id", tx.TransactionID,
			"units_restored", res.Units)
		return OutcomeRestored, tx.TransactionID, nil
	}

	status, kind := domain.PurchaseRefunded, "refund"
	if n.NotificationType == appstore.NotificationRevoke {
		status, kind = domain.PurchaseRevoked, "revoke"
	}
	at := now
	if tx.RevocationDate > 0 {
		at = time.UnixMilli(tx.RevocationDate).UTC()
	}
	tombstone := &domain.IAPPurchase{Environment: tx.Environment, StoreProductID: tx.ProductID,
		OriginalTransactionID: tx.OriginalTransactionID, Quantity: tx.Quantity}
	if product, err := s.repo.IAPProductByStoreID(ctx, domain.ProviderAppStore, tx.ProductID); err == nil {
		tombstone.ProductCode = product.Code
	}
	if tx.PurchaseDate > 0 {
		t := time.UnixMilli(tx.PurchaseDate).UTC()
		tombstone.PurchasedAt = &t
	}
	res, err := s.repo.RevokePurchase(ctx, repository.PurchaseRevocation{
		Provider: domain.ProviderAppStore, TransactionID: tx.TransactionID, Status: status,
		Reason: appleRevokeReason(kind, tx), At: at, Now: now, Tombstone: tombstone,
		SignedAt: time.UnixMilli(n.SignedDate),
	})
	if err != nil {
		return "", "", err
	}
	switch {
	case res.Ignored:
		return OutcomeUnchanged, tx.TransactionID, nil
	case !res.Found:
		return OutcomeUnknown, tx.TransactionID, nil
	case !res.Revoked:
		return OutcomeUnchanged, tx.TransactionID, nil
	}
	s.log.Info("iap purchase revoked", "provider", domain.ProviderAppStore, "user_id", res.UserID,
		"transaction_id", tx.TransactionID, "status", status, "units_removed", res.UnitsRemoved)
	return OutcomeRevoked, tx.TransactionID, nil
}

func (s *Service) applyAppleSubscription(ctx context.Context, user domain.User, product domain.IAPProduct, tx appstore.Transaction, autoRenew *bool, grace int64, restore bool, mayShorten bool) (Result, error) {
	if tx.OriginalTransactionID == "" || tx.ExpiresDate <= 0 || tx.PurchaseDate <= 0 || tx.ExpiresDate < tx.PurchaseDate {
		return Result{}, domain.PurchaseInvalid(domain.PurchaseReasonMalformed)
	}
	plan, err := s.repo.Plan(ctx, product.PlanID)
	if err != nil {
		return Result{}, err
	}
	if plan.IsFree {
		return Result{}, domain.PurchaseInvalid(domain.PurchaseReasonProduct)
	}
	expires := time.UnixMilli(tx.ExpiresDate)
	if tx.IsUpgraded {
		expires = time.UnixMilli(tx.PurchaseDate)
	}
	if grace > tx.ExpiresDate {
		expires = time.UnixMilli(grace)
	}
	var revoked *time.Time
	if tx.RevocationDate > 0 {
		t := time.UnixMilli(tx.RevocationDate)
		revoked = &t
	}
	res, err := s.repo.ApplyAppleSubscription(ctx, repository.AppleSubscription{
		UserID: user.ID, OriginalTransactionID: tx.OriginalTransactionID, TransactionID: tx.TransactionID, ProductID: tx.ProductID,
		PlanID: product.PlanID, Environment: tx.Environment, PurchasedAt: time.UnixMilli(tx.PurchaseDate), ExpiresAt: expires,
		SignedAt: time.UnixMilli(tx.SignedDate), RevokedAt: revoked, AutoRenew: autoRenew, PriceMilliunits: tx.Price, Currency: tx.Currency, Restore: restore, MayShorten: mayShorten,
	}, s.clock.Now())
	if err != nil {
		return Result{}, err
	}
	status := StatusAlreadyGranted
	if res.Granted {
		status = StatusGranted
	}
	return Result{Status: status, Purchase: res.Purchase}, nil
}

func (s *Service) appleSubscriptionNotification(ctx context.Context, n appstore.Notification, tx appstore.Transaction) (string, string, error) {
	user, err := s.repo.UserByIAPAccountToken(ctx, tx.AppAccountToken)
	if errors.Is(err, domain.ErrNotFound) {
		return OutcomeUnknown, tx.TransactionID, nil
	}
	if err != nil {
		return "", "", err
	}
	product, err := s.repo.IAPProductByStoreID(ctx, domain.ProviderAppStore, tx.ProductID)
	if err != nil {
		return "", "", err
	}
	if product.Kind != domain.ProductSubscription {
		return OutcomeIgnored, tx.TransactionID, nil
	}
	var autoRenew *bool
	var grace int64
	if n.Data.SignedRenewalInfo != "" {
		renewal, err := s.apple.Renewal(n.Data.SignedRenewalInfo)
		if err != nil {
			return "", "", s.appleRejected(err, user.ID)
		}
		if renewal.OriginalTransactionID != tx.OriginalTransactionID || !s.appleEnvironment(renewal.Environment) {
			return OutcomeIgnored, tx.TransactionID, nil
		}
		auto := renewal.AutoRenewStatus == 1
		autoRenew = &auto
		if n.NotificationType == appstore.NotificationFailedRenewal && n.Subtype == "GRACE_PERIOD" {
			grace = renewal.GracePeriodExpiresDate
		}
		// Renewal notifications may have a fresher signedDate than their transaction.
		if renewal.SignedDate > tx.SignedDate {
			tx.SignedDate = renewal.SignedDate
		}
	}
	if n.SignedDate > tx.SignedDate {
		tx.SignedDate = n.SignedDate
	}
	if n.NotificationType == appstore.NotificationRefund || n.NotificationType == appstore.NotificationRevoke {
		if tx.RevocationDate == 0 {
			return OutcomeIgnored, tx.TransactionID, nil
		}
	}
	restore := n.NotificationType == appstore.NotificationRefundReversed
	if restore {
		tx.RevocationDate = 0
	}
	_, err = s.applyAppleSubscription(ctx, user, product, tx, autoRenew, grace, restore, tx.IsUpgraded || n.NotificationType == appstore.NotificationGraceExpired || n.NotificationType == appstore.NotificationExpired)
	if err != nil {
		return "", "", err
	}
	return "subscription_updated", tx.TransactionID, nil
}
