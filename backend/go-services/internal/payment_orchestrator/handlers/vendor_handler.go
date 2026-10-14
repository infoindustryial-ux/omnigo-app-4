package handlers

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omnigo/backend/internal/escrow"
	"github.com/omnigo/backend/internal/ledger"
	"github.com/omnigo/backend/internal/payment/safepay"
	"github.com/omnigo/backend/internal/shared/middleware"
)

// VendorHandler handles vendor wallet and payout queries.
type VendorHandler struct {
	db      *pgxpool.Pool
	escrow  *escrow.Repository
	ledger  *ledger.Service
	safepay *safepay.Client
}

func NewVendorHandler(db *pgxpool.Pool, ledgerSvc *ledger.Service, safepayClient *safepay.Client) *VendorHandler {
	if safepayClient == nil {
		safepayClient = safepay.NewClientFromEnv()
	}
	return &VendorHandler{
		db:      db,
		escrow:  escrow.NewRepository(db),
		ledger:  ledgerSvc,
		safepay: safepayClient,
	}
}

// GetWallet handles GET /api/v1/vendor/wallet/:vendor_id
// Returns comprehensive wallet breakdown including escrow status.
func (h *VendorHandler) GetWallet(c *gin.Context) {
	vendorID := c.Param("vendor_id")
	callerID := middleware.GetTrackingID(c)
	callerRole := middleware.GetRole(c)
	if callerRole != "admin" && callerID != "" && callerID != vendorID {
		c.JSON(http.StatusForbidden, gin.H{"error": "unauthorized access to vendor wallet"})
		return
	}
	ctx := c.Request.Context()

	var balancePaisa, lifetimeEarningsPaisa, totalPayoutsPaisa int64
	err := h.db.QueryRow(ctx,
		`SELECT COALESCE(balance_paisa, 0), COALESCE(lifetime_earnings_paisa, 0), COALESCE(total_payouts_paisa, 0)
		 FROM vendor_wallet WHERE vendor_tracking_id = $1`,
		vendorID,
	).Scan(&balancePaisa, &lifetimeEarningsPaisa, &totalPayoutsPaisa)
	if err != nil {
		balancePaisa = 0
		lifetimeEarningsPaisa = 0
		totalPayoutsPaisa = 0
	}

	// Escrow held: status='held' (48h hold period)
	var escrowHeldPaisa int64
	_ = h.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_paisa), 0) FROM escrow_holds WHERE vendor_tracking_id = $1 AND status = 'held'`,
		vendorID,
	).Scan(&escrowHeldPaisa)

	// Ready to release: status='released' (hold expired, pending payout worker)
	var readyToReleasePaisa int64
	_ = h.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_paisa), 0) FROM escrow_holds WHERE vendor_tracking_id = $1 AND status = 'released'`,
		vendorID,
	).Scan(&readyToReleasePaisa)

	c.JSON(http.StatusOK, gin.H{
		"vendor_tracking_id":    vendorID,
		"withdrawable_paisa":    balancePaisa,
		"withdrawable_rupees":   float64(balancePaisa) / 100.0,
		"escrow_held_paisa":     escrowHeldPaisa,
		"escrow_held_rupees":    float64(escrowHeldPaisa) / 100.0,
		"ready_to_release_paisa": readyToReleasePaisa,
		"ready_to_release_rupees": float64(readyToReleasePaisa) / 100.0,
		"lifetime_earnings_paisa": lifetimeEarningsPaisa,
		"lifetime_earnings_rupees": float64(lifetimeEarningsPaisa) / 100.0,
		"total_payouts_paisa":   totalPayoutsPaisa,
		"total_payouts_rupees":  float64(totalPayoutsPaisa) / 100.0,
		"currency":              "PKR",
	})
}

// ListPayouts handles GET /api/v1/vendor/payouts/:vendor_id
func (h *VendorHandler) ListPayouts(c *gin.Context) {
	vendorID := c.Param("vendor_id")
	callerID := middleware.GetTrackingID(c)
	callerRole := middleware.GetRole(c)
	if callerRole != "admin" && callerID != "" && callerID != vendorID {
		c.JSON(http.StatusForbidden, gin.H{"error": "unauthorized access to vendor payouts"})
		return
	}
	ctx := c.Request.Context()

	rows, err := h.db.Query(ctx,
		`SELECT id, amount, COALESCE(method, ''), status, COALESCE(created_at, NOW()), COALESCE(completed_at, '0001-01-01')
		 FROM vendor_payouts WHERE vendor_tracking_id = $1 ORDER BY created_at DESC LIMIT 20`,
		vendorID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type PayoutResponse struct {
		ID          string  `json:"id"`
		Amount      float64 `json:"amount"`
		Method      string  `json:"method"`
		Status      string  `json:"status"`
		CreatedAt   string  `json:"created_at"`
		CompletedAt string  `json:"completed_at"`
	}

	var payouts []PayoutResponse
	for rows.Next() {
		var p PayoutResponse
		if err := rows.Scan(&p.ID, &p.Amount, &p.Method, &p.Status, &p.CreatedAt, &p.CompletedAt); err != nil {
			continue
		}
		payouts = append(payouts, p)
	}

	c.JSON(http.StatusOK, gin.H{"payouts": payouts})
}

// EscrowHoldItem is one row in the vendor's escrow breakdown.
type EscrowHoldItem struct {
	ID              string  `json:"id"`
	OrderTrackingID string  `json:"order_tracking_id"`
	AmountPaisa     int64   `json:"amount_paisa"`
	AmountRupees    float64 `json:"amount_rupees"`
	Status          string  `json:"status"`
	PaymentGateway  string  `json:"payment_gateway"`
	Currency        string  `json:"currency"`
	OrderStatus     string  `json:"order_status"`
	HoldUntil       string  `json:"hold_until"`
	ReleasedAt      *string `json:"released_at,omitempty"`
	CreatedAt       string  `json:"created_at"`
	// Computed: time remaining until release, or "released"/"stuck" hint
	ReleaseHint string `json:"release_hint"`
}

// ListEscrowHolds handles GET /api/v1/payments/vendor/escrow-holds
// Returns per-order escrow breakdown for the authenticated vendor.
//
// Authorization: vendor can only view their own holds. Admin can view any.
//
// This is a READ-ONLY endpoint — no state change, no double-spend risk.
// All amounts are in paisa (int64) for precision. JSON includes both paisa
// (internal) and rupees (display) for convenience.
//
// "release_hint" provides a human-readable status:
//   - "held_remaining_Xh_Ym" — escrow is held, Xh Ym until release
//   - "ready_to_release" — hold period expired, awaiting payout worker
//   - "released" — funds have been moved to withdrawable balance
//   - "paid_out" — funds have been paid to bank account
//   - "refunded" — order was refunded, escrow returned to customer
//   - "disputed" — escrow is on hold due to open dispute
//   - "cancelled" — order was cancelled, escrow released to customer
func (h *VendorHandler) ListEscrowHolds(c *gin.Context) {
	vendorID := c.Param("vendor_id")
	if vendorID == "" {
		vendorID = c.Query("vendor_id")
	}
	callerID := middleware.GetTrackingID(c)
	callerRole := middleware.GetRole(c)
	if callerRole != "admin" && callerID != "" && callerID != vendorID {
		c.JSON(http.StatusForbidden, gin.H{"error": "unauthorized access to escrow holds"})
		return
	}
	if vendorID == "" {
		// Fall back to caller's own vendor ID
		vendorID = callerID
	}
	if vendorID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "vendor_id required"})
		return
	}

	ctx := c.Request.Context()
	holds, err := h.escrow.GetHoldsByVendorWithOrder(ctx, vendorID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch escrow holds: " + err.Error()})
		return
	}

	now := time.Now()
	items := make([]EscrowHoldItem, 0, len(holds))
	for _, h := range holds {
		var releasedAt *string
		if h.ReleasedAt != nil {
			s := h.ReleasedAt.Format(time.RFC3339)
			releasedAt = &s
		}

		hint := computeReleaseHint(h.Status, h.HoldUntil, h.ReleasedAt, now)

		items = append(items, EscrowHoldItem{
			ID:              h.ID,
			OrderTrackingID: h.OrderTrackingID,
			AmountPaisa:     h.AmountPaisa,
			AmountRupees:    float64(h.AmountPaisa) / 100.0,
			Status:          h.Status,
			PaymentGateway:  h.PaymentGateway,
			Currency:        h.Currency,
			OrderStatus:     h.OrderStatus,
			HoldUntil:       h.HoldUntil.Format(time.RFC3339),
			ReleasedAt:      releasedAt,
			CreatedAt:       h.CreatedAt.Format(time.RFC3339),
			ReleaseHint:     hint,
		})
	}

	// Summary buckets
	summary := map[string]int64{
		"held_paisa":             0,
		"released_paisa":         0,
		"paid_out_paisa":         0,
		"refunded_paisa":         0,
		"disputed_paisa":         0,
		"cancelled_paisa":        0,
		"releasing_paisa":        0,
		"ready_to_release_paisa": 0, // released but not yet paid out
	}
	for _, h := range holds {
		switch h.Status {
		case "held":
			summary["held_paisa"] += h.AmountPaisa
		case "released":
			summary["released_paisa"] += h.AmountPaisa
			summary["ready_to_release_paisa"] += h.AmountPaisa
		case "paid_out":
			summary["paid_out_paisa"] += h.AmountPaisa
		case "refunded":
			summary["refunded_paisa"] += h.AmountPaisa
		case "disputed":
			summary["disputed_paisa"] += h.AmountPaisa
		case "cancelled":
			summary["cancelled_paisa"] += h.AmountPaisa
		case "releasing":
			summary["releasing_paisa"] += h.AmountPaisa
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"vendor_tracking_id": vendorID,
		"holds":              items,
		"summary":            summary,
		"count":              len(items),
		"currency":           "PKR",
		"generated_at":       now.Format(time.RFC3339),
	})
}

// computeReleaseHint returns a human-readable hint about the hold's status.
func computeReleaseHint(status string, holdUntil time.Time, releasedAt *time.Time, now time.Time) string {
	switch status {
	case "held":
		remaining := holdUntil.Sub(now)
		if remaining <= 0 {
			return "ready_to_release" // hold expired, awaiting worker
		}
		hours := int(remaining.Hours())
		minutes := int(remaining.Minutes()) % 60
		if hours > 0 {
			return fmt.Sprintf("held_remaining_%dh_%dm", hours, minutes)
		}
		return fmt.Sprintf("held_remaining_%dm", minutes)
	case "released":
		return "ready_to_release"
	case "paid_out":
		return "released"
	case "refunded":
		return "refunded"
	case "disputed":
		return "disputed"
	case "cancelled":
		return "cancelled"
	case "releasing":
		return "releasing"
	default:
		return status
	}
}

type WithdrawRequest struct {
	VendorTrackingID string  `json:"vendor_tracking_id"`
	Amount           float64 `json:"amount" binding:"required,gt=0"`
	Method           string  `json:"method"` // bank_transfer or raast
	BeneficiaryName  string  `json:"beneficiary_name"`
	BeneficiaryIBAN  string  `json:"beneficiary_iban"`
	RaastID          string  `json:"raast_id"`
	BankName         string  `json:"bank_name"`
}

type BankAccountRequest struct {
	VendorTrackingID string `json:"vendor_tracking_id"`
	BeneficiaryName  string `json:"beneficiary_name" binding:"required"`
	BeneficiaryIBAN  string `json:"beneficiary_iban" binding:"required"`
	RaastID          string `json:"raast_id"`
	BankName         string `json:"bank_name"`
}

// GetBankAccount handles GET /api/v1/payments/vendor/bank-account/:vendor_id
func (h *VendorHandler) GetBankAccount(c *gin.Context) {
	vendorID := c.Param("vendor_id")
	callerID := middleware.GetTrackingID(c)
	callerRole := middleware.GetRole(c)
	if callerRole != "admin" && callerID != "" && callerID != vendorID {
		c.JSON(http.StatusForbidden, gin.H{"error": "unauthorized access to bank account"})
		return
	}
	ctx := c.Request.Context()

	var account struct {
		ID              string `json:"id"`
		BeneficiaryName string `json:"beneficiary_name"`
		BeneficiaryIBAN string `json:"beneficiary_iban"`
		RaastID         string `json:"raast_id"`
		BankName        string `json:"bank_name"`
		IsVerified      bool   `json:"is_verified"`
	}

	err := h.db.QueryRow(ctx,
		`SELECT id::text, beneficiary_name, COALESCE(beneficiary_iban, ''), COALESCE(raast_id, ''), COALESCE(bank_name, ''), is_verified
		 FROM user_bank_accounts
		 WHERE user_tracking_id = $1 AND user_role = 'vendor' AND is_default = true
		 ORDER BY created_at DESC LIMIT 1`,
		vendorID,
	).Scan(&account.ID, &account.BeneficiaryName, &account.BeneficiaryIBAN, &account.RaastID, &account.BankName, &account.IsVerified)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"bank_account": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"bank_account": account})
}

// SaveBankAccount handles POST /api/v1/payments/vendor/bank-account
func (h *VendorHandler) SaveBankAccount(c *gin.Context) {
	var req BankAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	callerID := middleware.GetTrackingID(c)
	if callerID != "" {
		req.VendorTrackingID = callerID
	}
	if req.VendorTrackingID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: missing vendor identity"})
		return
	}

	sanitizedIBAN := safepay.SanitizeIBAN(req.BeneficiaryIBAN)
	if err := safepay.ValidatePakistaniIBAN(sanitizedIBAN); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid Pakistani IBAN: %v", err)})
		return
	}
	req.BeneficiaryIBAN = sanitizedIBAN

	ctx := c.Request.Context()
	_, _ = h.db.Exec(ctx,
		`UPDATE user_bank_accounts SET is_default = false WHERE user_tracking_id = $1 AND user_role = 'vendor'`,
		req.VendorTrackingID,
	)

	var accountID string
	err := h.db.QueryRow(ctx,
		`INSERT INTO user_bank_accounts (
			user_tracking_id, user_role, beneficiary_name, beneficiary_iban, raast_id, bank_name, is_default, updated_at
		 ) VALUES ($1, 'vendor', $2, $3, $4, $5, true, NOW())
		 RETURNING id::text`,
		req.VendorTrackingID, req.BeneficiaryName, req.BeneficiaryIBAN, req.RaastID, req.BankName,
	).Scan(&accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save bank account: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":          "Bank account saved successfully",
		"id":               accountID,
		"beneficiary_iban": req.BeneficiaryIBAN,
		"beneficiary_name": req.BeneficiaryName,
	})
}

func (h *VendorHandler) resolveBeneficiaryIBAN(ctx context.Context, req *WithdrawRequest) error {
	if req.BeneficiaryIBAN == "" {
		_ = h.db.QueryRow(ctx,
			`SELECT beneficiary_name, COALESCE(beneficiary_iban, ''), COALESCE(raast_id, ''), COALESCE(bank_name, '')
			 FROM user_bank_accounts
			 WHERE user_tracking_id = $1 AND user_role = 'vendor' AND is_default = true
			 ORDER BY created_at DESC LIMIT 1`,
			req.VendorTrackingID,
		).Scan(&req.BeneficiaryName, &req.BeneficiaryIBAN, &req.RaastID, &req.BankName)
	}

	if req.BeneficiaryIBAN == "" {
		return fmt.Errorf("beneficiary IBAN is required for bank withdrawal")
	}

	sanitizedIBAN := safepay.SanitizeIBAN(req.BeneficiaryIBAN)
	if err := safepay.ValidatePakistaniIBAN(sanitizedIBAN); err != nil {
		return fmt.Errorf("invalid Pakistani IBAN: %w", err)
	}
	req.BeneficiaryIBAN = sanitizedIBAN
	return nil
}

// RequestWithdraw handles POST /api/v1/payments/vendor/withdraw
// Deducts withdrawal amount immediately from vendor wallet and schedules fulfillment via Safepay after 7 days.
// Transaction fee (e.g. Rs. 20) is deducted from the vendor's withdrawal amount.
func (h *VendorHandler) RequestWithdraw(c *gin.Context) {
	var req WithdrawRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	callerID := middleware.GetTrackingID(c)
	if callerID != "" {
		req.VendorTrackingID = callerID
	}
	if req.VendorTrackingID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: missing vendor identity"})
		return
	}

	if req.Method == "" {
		req.Method = "bank_transfer"
	}

	ctx := c.Request.Context()
	if err := h.resolveBeneficiaryIBAN(ctx, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	payoutFeePaisa := int64(2000)
	if h.safepay != nil {
		payoutFeePaisa = h.safepay.PayoutFeePaisa()
	}

	amountPaisa := int64(math.Round(float64(req.Amount) * 100))
	if amountPaisa <= payoutFeePaisa {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("withdrawal amount (PKR %.2f) must exceed transaction fee of PKR %.2f",
				req.Amount, float64(payoutFeePaisa)/100.0),
		})
		return
	}

	netDisbursementPaisa := amountPaisa - payoutFeePaisa

	tx, err := h.db.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to begin transaction: %v", err)})
		return
	}
	defer tx.Rollback(ctx)

	var balancePaisa int64
	err = tx.QueryRow(ctx,
		`SELECT COALESCE(balance_paisa, 0) FROM vendor_wallet WHERE vendor_tracking_id = $1 FOR UPDATE`,
		req.VendorTrackingID,
	).Scan(&balancePaisa)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "vendor wallet not found or initialized"})
		return
	}

	// CLAWBACK FIX: Deduct pending clawback from available balance
	var clawbackPaisa int64
	_ = tx.QueryRow(ctx,
		`SELECT COALESCE(vendor_clawback_paisa, 0) FROM vendor_wallet WHERE vendor_tracking_id = $1`,
		req.VendorTrackingID,
	).Scan(&clawbackPaisa)

	availableBalance := balancePaisa - clawbackPaisa
	if availableBalance < 0 {
		availableBalance = 0
	}
	if availableBalance < amountPaisa {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("insufficient withdrawable balance (available: PKR %.2f, requested: PKR %.2f, pending clawback: PKR %.2f)",
				float64(availableBalance)/100.0, req.Amount, float64(clawbackPaisa)/100.0),
		})
		return
	}

	tag, err := tx.Exec(ctx,
		`UPDATE vendor_wallet
		 SET balance_paisa = balance_paisa - $1, total_payouts_paisa = total_payouts_paisa + $1
		 WHERE vendor_tracking_id = $2 AND balance_paisa >= $1`,
		amountPaisa, req.VendorTrackingID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update wallet balance: %v", err)})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "wallet balance changed concurrently — please retry"})
		return
	}

	// 7-day scheduled fulfillment rule: funds are released to Safepay after exactly 7 days
	scheduledAt := time.Now().Add(7 * 24 * time.Hour)
	payoutID := uuid.New()

	_, err = tx.Exec(ctx,
		`INSERT INTO vendor_payouts (
			id, vendor_tracking_id, amount, amount_paisa, method, status,
			beneficiary_name, beneficiary_iban, raast_id, bank_name,
			gateway, payout_fee_paisa, scheduled_at, created_at, updated_at
		 ) VALUES ($1, $2, $3, $4, $5, 'scheduled', $6, $7, $8, $9, 'safepay', $10, $11, NOW(), NOW())`,
		payoutID, req.VendorTrackingID, req.Amount, amountPaisa, req.Method,
		req.BeneficiaryName, req.BeneficiaryIBAN, req.RaastID, req.BankName,
		payoutFeePaisa, scheduledAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to insert payout entry: %v", err)})
		return
	}

	// Upsert bank account record for future convenience
	if req.BeneficiaryIBAN != "" {
		_, _ = tx.Exec(ctx,
			`INSERT INTO user_bank_accounts (
				user_tracking_id, user_role, beneficiary_name, beneficiary_iban, raast_id, bank_name, is_default, updated_at
			 ) VALUES ($1, 'vendor', $2, $3, $4, $5, true, NOW())
			 ON CONFLICT DO NOTHING`,
			req.VendorTrackingID, req.BeneficiaryName, req.BeneficiaryIBAN, req.RaastID, req.BankName,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to commit transaction: %v", err)})
		return
	}

	// Double-entry ledger movement: vendor_wallet → vendor_bank_payout_account
	if h.ledger != nil {
		idempotencyKey := fmt.Sprintf("vendor:payout:%s", payoutID)
		if _, lErr := h.ledger.Transfer(ctx, ledger.TransferRequest{
			DebitAccount:   ledger.AccountVendorWallet,
			CreditAccount:  ledger.AccountVendorBankPayout,
			Amount:         amountPaisa,
			Currency:       "PKR",
			ReferenceType:  "vendor_payout",
			ReferenceID:    payoutID.String(),
			Description:    fmt.Sprintf("Vendor payout scheduled for %s: %d paisa (fee: %d paisa)", req.VendorTrackingID, amountPaisa, payoutFeePaisa),
			IdempotencyKey: idempotencyKey,
		}); lErr != nil {
			fmt.Printf("[VendorHandler] Warning: ledger transfer failed for payout %s: %v\n", payoutID, lErr)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":               "Vendor withdrawal request scheduled successfully. Disbursement will be executed in 7 days via Safepay Raastwire.",
		"payout_id":             payoutID.String(),
		"status":                "scheduled",
		"amount_requested":      req.Amount,
		"amount_paisa":          amountPaisa,
		"fee_rupees":            float64(payoutFeePaisa) / 100.0,
		"fee_paisa":             payoutFeePaisa,
		"net_disbursement":      float64(netDisbursementPaisa) / 100.0,
		"net_disbursement_paisa": netDisbursementPaisa,
		"scheduled_at":          scheduledAt.Format(time.RFC3339),
		"beneficiary_iban":      req.BeneficiaryIBAN,
		"beneficiary_name":      req.BeneficiaryName,
		"bank_name":             req.BankName,
	})
}

// RegisterRoutes registers vendor wallet/payout endpoints.
func (h *VendorHandler) RegisterRoutes(router *gin.Engine) {
	vendor := router.Group("/api/v1/payments/vendor", middleware.JWTAuth(), middleware.RoleRequired("vendor", "admin"))
	{
		vendor.GET("/wallet/:vendor_id", h.GetWallet)
		vendor.GET("/payouts/:vendor_id", h.ListPayouts)
		vendor.GET("/escrow-holds/:vendor_id", h.ListEscrowHolds)
		vendor.GET("/bank-account/:vendor_id", h.GetBankAccount)
		vendor.POST("/bank-account", middleware.RoleRequired("vendor"), h.SaveBankAccount)
		vendor.POST("/withdraw", middleware.RoleRequired("vendor"), h.RequestWithdraw)
	}
}
