package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omnigo/backend/internal/escrow"
	"github.com/omnigo/backend/internal/ledger"
	"github.com/omnigo/backend/internal/payment/safepay"
	"github.com/omnigo/backend/internal/payment_orchestrator"
	"github.com/omnigo/backend/internal/payment_orchestrator/fraud"
	"github.com/omnigo/backend/internal/shared/telemetry"
)

// SafepayService handles customer payments via Safepay (hosted checkout & Cybersource card 3DS).
type SafepayService struct {
	db              *pgxpool.Pool
	ledger          *ledger.Service
	escrow          *escrow.Service
	calculator      *payment_orchestrator.CommissionCalculator
	safepay         *safepay.Client
	fraud           *fraud.Detector
	callbackBaseURL string
	defaultCurrency string
}

// NewSafepayService constructs a new SafepayService.
func NewSafepayService(
	db *pgxpool.Pool,
	ledgerSvc *ledger.Service,
	escrowSvc *escrow.Service,
	calc *payment_orchestrator.CommissionCalculator,
	safepayClient *safepay.Client,
) *SafepayService {
	if safepayClient == nil {
		safepayClient = safepay.NewClientFromEnv()
	}

	publicBase := strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/")
	if publicBase == "" {
		publicBase = "https://omnigo-app-4-production.up.railway.app"
	}
	cbURL := os.Getenv("SAFEPAY_3DS_CALLBACK_URL")
	if cbURL == "" {
		cbURL = publicBase + "/api/v1/payments/safepay/3ds/callback"
	}

	currency := os.Getenv("DEFAULT_CURRENCY")
	if currency == "" {
		currency = "PKR"
	}

	return &SafepayService{
		db:              db,
		ledger:          ledgerSvc,
		escrow:          escrowSvc,
		calculator:      calc,
		safepay:         safepayClient,
		fraud:           fraud.NewDetector(nil, db),
		callbackBaseURL: cbURL,
		defaultCurrency: currency,
	}
}

// SafepayCheckoutRequest parameters for initiating Safepay checkout.
type SafepayCheckoutRequest struct {
	OrderID        string `json:"order_id" binding:"required"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// SafepayCardPaymentRequest contains card details and billing for direct 3DS processing.
type SafepayCardPaymentRequest struct {
	OrderID        string `json:"order_id" binding:"required"`
	CardNumber     string `json:"card_number" binding:"required"`
	ExpiryMonth    string `json:"expiry_month" binding:"required"`
	ExpiryYear     string `json:"expiry_year" binding:"required"`
	CVV            string `json:"cvv" binding:"required"`
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	Email          string `json:"email"`
	Phone          string `json:"phone"`
	Address        string `json:"address"`
	City           string `json:"city"`
	PostalCode     string `json:"postal_code"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// Validate checks card formatting.
func (r *SafepayCardPaymentRequest) Validate() error {
	cleanPan := strings.ReplaceAll(r.CardNumber, " ", "")
	if len(cleanPan) < 13 || len(cleanPan) > 19 {
		return errors.New("invalid card number length")
	}
	if len(r.CVV) < 3 || len(r.CVV) > 4 {
		return errors.New("invalid CVV (must be 3 or 4 digits)")
	}
	m, err := strconv.Atoi(r.ExpiryMonth)
	if err != nil || m < 1 || m > 12 {
		return errors.New("invalid expiry month (must be 01 to 12)")
	}
	y, err := strconv.Atoi(r.ExpiryYear)
	if err != nil {
		return errors.New("invalid expiry year")
	}
	if len(r.ExpiryYear) == 2 {
		y += 2000
	}
	now := time.Now()
	if y < now.Year() || (y == now.Year() && m < int(now.Month())) {
		return errors.New("card is expired")
	}
	return nil
}

// InitiateHostedCheckout starts a hosted checkout tracker and returns the hosted URL for WebView / redirect.
func (s *SafepayService) InitiateHostedCheckout(ctx context.Context, merchantUserID, clientIP string, req SafepayCheckoutRequest) (*PaymentResponse, error) {
	if !s.safepay.IsConfigured() {
		return nil, errors.New("safepay: service not configured")
	}

	if s.fraud != nil {
		if err := s.fraud.CheckVelocity(ctx, merchantUserID, clientIP); err != nil {
			telemetry.RecordFraudBlock("velocity_limit_exceeded")
			return nil, err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var expectedAmountPaisa int64
	var orderStatus string
	var customerTrackingID string
	var storeID string
	err = tx.QueryRow(ctx,
		`SELECT total_amount_paisa, status, customer_tracking_id, store_tracking_id 
		 FROM orders WHERE order_tracking_id = $1 FOR UPDATE`, req.OrderID,
	).Scan(&expectedAmountPaisa, &orderStatus, &customerTrackingID, &storeID)
	if err != nil {
		return nil, fmt.Errorf("%w: order not found: %v", ErrNotFound, err)
	}
	if merchantUserID != customerTrackingID {
		return nil, fmt.Errorf("%w: order belongs to a different user", ErrForbidden)
	}
	if orderStatus != "pending" && orderStatus != "unpaid" {
		return nil, fmt.Errorf("%w: order is not in a payable status", ErrConflict)
	}

	internalTxnID := "sp_" + uuid.New().String()
	idempotencyKey := req.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = "sp:auto:" + internalTxnID
	}

	tracker, err := s.safepay.CreateTracker(ctx, safepay.TrackerRequest{
		Currency:  "PKR",
		Amount:    expectedAmountPaisa,
		EntryMode: "hosted",
		Metadata: map[string]string{
			"order_id":        req.OrderID,
			"customer_id":     customerTrackingID,
			"internal_txn_id": internalTxnID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("safepay tracker creation failed: %w", err)
	}

	checkoutURL := s.safepay.BuildCheckoutURL(tracker.Token)

	metadata := map[string]interface{}{
		"tracker_token": tracker.Token,
		"checkout_url":  checkoutURL,
		"gateway":       "safepay",
	}
	metaBytes, _ := json.Marshal(metadata)

	_, err = tx.Exec(ctx,
		`INSERT INTO payment_transactions (transaction_id, order_tracking_id, gateway, amount, currency, status, kind, idempotency_key, metadata)
		 VALUES ($1, $2, 'safepay', $3, $4, 'pending', 'payment', $5, $6::jsonb)`,
		internalTxnID, req.OrderID, float64(expectedAmountPaisa)/100.0, s.defaultCurrency, idempotencyKey, string(metaBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to record payment transaction: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit payment transaction: %w", err)
	}

	return &PaymentResponse{
		Status:        "hosted_redirect",
		Action:        "redirect",
		RedirectURL:   checkoutURL,
		OrderID:       req.OrderID,
		TransactionID: internalTxnID,
		Message:       "Safepay checkout initialized",
	}, nil
}

// ProcessCardPayment processes a direct credit/debit card charge through Cybersource 3DS.
func (s *SafepayService) ProcessCardPayment(ctx context.Context, merchantUserID, clientIP string, req SafepayCardPaymentRequest) (*PaymentResponse, error) {
	if !s.safepay.IsConfigured() {
		return nil, errors.New("safepay: service not configured")
	}

	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrValidation, err)
	}

	if s.fraud != nil {
		if err := s.fraud.CheckVelocity(ctx, merchantUserID, clientIP); err != nil {
			telemetry.RecordFraudBlock("velocity_limit_exceeded")
			return nil, err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var expectedAmountPaisa int64
	var orderStatus string
	var customerTrackingID string
	var storeID string
	err = tx.QueryRow(ctx,
		`SELECT total_amount_paisa, status, customer_tracking_id, store_tracking_id 
		 FROM orders WHERE order_tracking_id = $1 FOR UPDATE`, req.OrderID,
	).Scan(&expectedAmountPaisa, &orderStatus, &customerTrackingID, &storeID)
	if err != nil {
		return nil, fmt.Errorf("%w: order not found: %v", ErrNotFound, err)
	}
	if merchantUserID != customerTrackingID {
		return nil, fmt.Errorf("%w: order belongs to a different user", ErrForbidden)
	}
	if orderStatus != "pending" && orderStatus != "unpaid" {
		return nil, fmt.Errorf("%w: order is not in a payable status", ErrConflict)
	}

	internalTxnID := "sp_" + uuid.New().String()
	idempotencyKey := req.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = "sp:auto:" + internalTxnID
	}

	// 1. Create tracker
	tracker, err := s.safepay.CreateTracker(ctx, safepay.TrackerRequest{
		Currency:  "PKR",
		Amount:    expectedAmountPaisa,
		EntryMode: "raw",
		Metadata: map[string]string{
			"order_id":        req.OrderID,
			"customer_id":     customerTrackingID,
			"internal_txn_id": internalTxnID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("safepay tracker creation failed: %w", err)
	}

	// 2. Setup card details
	_, err = s.safepay.PayerAuthSetup(ctx, tracker.Token, safepay.CardDetails{
		CardNumber:      req.CardNumber,
		ExpirationMonth: req.ExpiryMonth,
		ExpirationYear:  req.ExpiryYear,
		CVV:             req.CVV,
	})
	if err != nil {
		return nil, fmt.Errorf("safepay payer auth setup failed: %w", err)
	}

	// 3. Payer auth enrollment
	cbURL := fmt.Sprintf("%s?tracker=%s&order_id=%s", s.callbackBaseURL, tracker.Token, req.OrderID)
	enrollRes, err := s.safepay.PayerAuthEnrollment(ctx, tracker.Token, safepay.BillingDetails{
		FirstName:  req.FirstName,
		LastName:   req.LastName,
		Email:      req.Email,
		Phone:      req.Phone,
		Address1:   req.Address,
		Locality:   req.City,
		PostalCode: req.PostalCode,
		Country:    "PK",
	}, safepay.RedirectURLs{
		SuccessURL: cbURL,
		FailureURL: cbURL + "&failed=true",
	})
	if err != nil {
		return nil, fmt.Errorf("safepay payer auth enrollment failed: %w", err)
	}

	metadata := map[string]interface{}{
		"tracker_token": tracker.Token,
		"auth_status":   enrollRes.AuthStatus,
		"gateway":       "safepay",
	}
	metaBytes, _ := json.Marshal(metadata)

	_, err = tx.Exec(ctx,
		`INSERT INTO payment_transactions (transaction_id, order_tracking_id, gateway, amount, currency, status, kind, idempotency_key, metadata)
		 VALUES ($1, $2, 'safepay', $3, $4, 'pending', 'payment', $5, $6::jsonb)`,
		internalTxnID, req.OrderID, float64(expectedAmountPaisa)/100.0, s.defaultCurrency, idempotencyKey, string(metaBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to record payment transaction: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit payment transaction: %w", err)
	}

	// If 3DS step-up challenge is required by issuing bank
	if enrollRes.AuthStatus == "REQUIRED" && enrollRes.StepUpURL != "" {
		return &PaymentResponse{
			Status:        "3ds_redirect",
			Action:        "challenge",
			RedirectURL:   enrollRes.StepUpURL,
			OrderID:       req.OrderID,
			TransactionID: internalTxnID,
			Message:       "3-D Secure OTP verification required",
		}, nil
	}

	// Frictionless / authenticated -> proceed to authorize & capture
	authRes, err := s.safepay.Authorize(ctx, tracker.Token, true)
	if err != nil {
		return nil, fmt.Errorf("safepay payment authorization failed: %w", err)
	}

	if authRes.State == "CAPTURED" || authRes.State == "PAID" {
		if err := s.VerifyAndSettle(ctx, internalTxnID, req.OrderID, tracker.Token, expectedAmountPaisa); err != nil {
			return nil, err
		}
		return &PaymentResponse{
			Status:        "settlement_pending",
			OrderID:       req.OrderID,
			TransactionID: internalTxnID,
			Message:       "Payment completed successfully",
		}, nil
	}

	return &PaymentResponse{
		Status:        "gateway_pending",
		OrderID:       req.OrderID,
		TransactionID: internalTxnID,
		Message:       "Payment pending at Safepay gateway",
	}, nil
}

// Handle3DSCallback processes returns from Safepay 3DS challenges.
func (s *SafepayService) Handle3DSCallback(ctx context.Context, trackerToken, orderID string) (*PaymentResponse, error) {
	ctx = context.WithoutCancel(ctx)

	// Fetch transaction
	var internalTxnID string
	var amountRupees float64
	err := s.db.QueryRow(ctx,
		`SELECT transaction_id, amount FROM payment_transactions 
		 WHERE order_tracking_id = $1 AND gateway = 'safepay' AND status = 'pending'
		 ORDER BY created_at DESC LIMIT 1`,
		orderID,
	).Scan(&internalTxnID, &amountRupees)
	if err != nil {
		return nil, fmt.Errorf("pending transaction not found for order: %w", err)
	}

	amountPaisa := int64(amountRupees * 100)

	// Authorize & capture
	authRes, err := s.safepay.Authorize(ctx, trackerToken, true)
	if err != nil {
		return nil, fmt.Errorf("safepay authorization after 3DS failed: %w", err)
	}

	if authRes.State == "CAPTURED" || authRes.State == "PAID" {
		if err := s.VerifyAndSettle(ctx, internalTxnID, orderID, trackerToken, amountPaisa); err != nil {
			return nil, err
		}
		return &PaymentResponse{
			Status:        "settlement_pending",
			OrderID:       orderID,
			TransactionID: internalTxnID,
			Message:       "3DS payment captured and settled successfully",
		}, nil
	}

	return &PaymentResponse{
		Status:        "gateway_pending",
		OrderID:       orderID,
		TransactionID: internalTxnID,
		Message:       "Safepay state: " + authRes.State,
	}, nil
}

// VerifyAndSettle performs order split calculation, commission deduction, and outbox event publishing for automated settlement.
func (s *SafepayService) VerifyAndSettle(ctx context.Context, internalTxnID, orderID, trackerToken string, expectedAmountPaisa int64) error {
	ctx = context.WithoutCancel(ctx)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin settlement transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Lock order
	var dbAmountPaisa int64
	var storeID string
	var vendorTrackingID string
	var orderStatus, paymentStatus string
	err = tx.QueryRow(ctx,
		`SELECT total_amount_paisa, store_tracking_id, vendor_tracking_id, status, COALESCE(payment_status, '') 
		 FROM orders WHERE order_tracking_id = $1 FOR UPDATE`, orderID,
	).Scan(&dbAmountPaisa, &storeID, &vendorTrackingID, &orderStatus, &paymentStatus)
	if err != nil {
		return fmt.Errorf("order lookup failed: %w", err)
	}
	if orderStatus == "paid" || paymentStatus == "paid" || paymentStatus == "settlement_pending" {
		return nil // Already settled idempotently
	}

	var deliveryTrackingID string
	if scanErr := tx.QueryRow(ctx, `SELECT COALESCE(tracking_id, '') FROM deliveries WHERE order_tracking_id = $1`, orderID).Scan(&deliveryTrackingID); scanErr != nil {
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			log.Printf("ERROR: delivery lookup error for order %s: %v", orderID, scanErr)
		}
	}

	// Calculate commission and escrow split
	var split *payment_orchestrator.SplitResult
	if s.calculator != nil {
		var calcErr error
		split, calcErr = s.calculator.CalculateSplit(ctx, dbAmountPaisa, storeID, deliveryTrackingID)
		if calcErr != nil {
			return fmt.Errorf("split calculation failed: %w", calcErr)
		}
	} else {
		// Fallback: 10% platform, 90% vendor
		adminRev := dbAmountPaisa / 10
		vendorEsc := dbAmountPaisa - adminRev
		split = &payment_orchestrator.SplitResult{
			AdminRevenue:   adminRev,
			VendorEscrow:   vendorEsc,
			DeliveryEscrow: 0,
		}
	}

	idempotencyKey := fmt.Sprintf("safepay:split:%s", trackerToken)
	if trackerToken == "" {
		idempotencyKey = fmt.Sprintf("safepay:split:%s", orderID)
	}

	// 2. Mark payment transaction as settlement_pending
	_, err = tx.Exec(ctx,
		`UPDATE payment_transactions 
		 SET status = 'settlement_pending', gateway_txn_id = $1, updated_at = NOW() 
		 WHERE transaction_id = $2`,
		trackerToken, internalTxnID,
	)
	if err != nil {
		return fmt.Errorf("failed to update payment_transactions: %w", err)
	}

	// 3. Update orders split columns and payment status
	_, err = tx.Exec(ctx,
		`UPDATE orders 
		 SET admin_commission_paisa = $1, vendor_escrow_paisa = $2, delivery_escrow_paisa = $3, payment_status = 'settlement_pending', updated_at = NOW() 
		 WHERE order_tracking_id = $4`,
		split.AdminRevenue, split.VendorEscrow, split.DeliveryEscrow, orderID,
	)
	if err != nil {
		return fmt.Errorf("failed to update order: %w", err)
	}

	transfers := []map[string]interface{}{
		{
			"debit_account":  string(ledger.AccountSafepayHolding),
			"credit_account": string(ledger.AccountAdminRevenue),
			"amount_paisa":   split.AdminRevenue,
			"idempotency":    idempotencyKey + ":admin",
		},
		{
			"debit_account":  string(ledger.AccountSafepayHolding),
			"credit_account": string(ledger.AccountVendorLockedEscrow),
			"amount_paisa":   split.VendorEscrow,
			"idempotency":    idempotencyKey + ":vendor",
		},
	}
	if split.DeliveryEscrow > 0 {
		transfers = append(transfers, map[string]interface{}{
			"debit_account":  string(ledger.AccountSafepayHolding),
			"credit_account": string(ledger.AccountCentralEscrow),
			"amount_paisa":   split.DeliveryEscrow,
			"idempotency":    idempotencyKey + ":delivery",
		})
	}

	outboxPayload, err := json.Marshal(map[string]interface{}{
		"internal_txn_id":       internalTxnID,
		"order_id":              orderID,
		"gateway_txn_id":        trackerToken,
		"store_id":              storeID,
		"vendor_tracking_id":    vendorTrackingID,
		"delivery_tracking_id":  deliveryTrackingID,
		"total_amount_paisa":    dbAmountPaisa,
		"currency":              s.defaultCurrency,
		"admin_revenue_paisa":   split.AdminRevenue,
		"vendor_escrow_paisa":   split.VendorEscrow,
		"delivery_escrow_paisa": split.DeliveryEscrow,
		"idempotency_key":       idempotencyKey,
		"transfers":             transfers,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal outbox payload: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO outbox_events (aggregate_id, topic, payload, status, created_at, updated_at) 
		 VALUES ($1, 'payment_settlement', $2, 'PENDING', NOW(), NOW())`,
		orderID, string(outboxPayload),
	)
	if err != nil {
		return fmt.Errorf("failed to insert outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit settlement transaction: %w", err)
	}

	log.Printf("✓ [SafepayService] Order %s queued for settlement via Safepay (%d paisa)", orderID, expectedAmountPaisa)
	return nil
}

// CreateQuickLink generates a hosted invoice link for an order.
func (s *SafepayService) CreateQuickLink(ctx context.Context, merchantUserID, orderID, note, recipientEmail string) (*safepay.QuickLinkResponse, error) {
	if !s.safepay.IsConfigured() {
		return nil, errors.New("safepay: service not configured")
	}

	var totalAmountPaisa int64
	var customerTrackingID string
	err := s.db.QueryRow(ctx,
		`SELECT total_amount_paisa, customer_tracking_id FROM orders WHERE order_tracking_id = $1`,
		orderID,
	).Scan(&totalAmountPaisa, &customerTrackingID)
	if err != nil {
		return nil, fmt.Errorf("order not found: %w", err)
	}

	if note == "" {
		note = fmt.Sprintf("Payment for OMNIGO Order %s", orderID)
	}

	return s.safepay.CreateQuickLink(ctx, safepay.QuickLinkRequest{
		Amount:         totalAmountPaisa,
		Currency:       "PKR",
		Note:           note,
		Workflow:       "MANUAL",
		RecipientEmail: recipientEmail,
		Metadata: map[string]string{
			"order_id":    orderID,
			"customer_id": customerTrackingID,
		},
	})
}

// GetPaymentStatus checks transaction status via Safepay Reporter API.
func (s *SafepayService) GetPaymentStatus(ctx context.Context, trackerToken string) (*safepay.PaymentDetails, error) {
	return s.safepay.GetPaymentDetails(ctx, trackerToken)
}
