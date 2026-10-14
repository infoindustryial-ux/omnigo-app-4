package workers

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omnigo/backend/internal/ledger"
	"github.com/omnigo/backend/internal/payment/safepay"
	"github.com/redis/go-redis/v9"
)

// SafepayPayoutWorker sweeps matured vendor payouts (7-day rule), rider payouts,
// and customer bank payouts, executing disbursements via Safepay Raastwire API.
type SafepayPayoutWorker struct {
	db      *pgxpool.Pool
	ledger  *ledger.Service
	safepay *safepay.Client
	redis   redis.UniversalClient
}

func NewSafepayPayoutWorker(db *pgxpool.Pool, ledgerSvc *ledger.Service, safepayClient *safepay.Client, rdb redis.UniversalClient) *SafepayPayoutWorker {
	if safepayClient == nil {
		safepayClient = safepay.NewClientFromEnv()
	}
	return &SafepayPayoutWorker{
		db:      db,
		ledger:  ledgerSvc,
		safepay: safepayClient,
		redis:   rdb,
	}
}

// Start initiates the payout execution loop.
func (w *SafepayPayoutWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	log.Println("[SafepayPayoutWorker] Started — sweeping matured payouts every 2 minutes")

	// Run sweep immediately on start
	w.sweepAllPayouts(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("[SafepayPayoutWorker] Shutting down")
			return
		case <-ticker.C:
			w.sweepAllPayouts(ctx)
		}
	}
}

func (w *SafepayPayoutWorker) sweepAllPayouts(ctx context.Context) {
	if w.redis != nil {
		lockKey := "lock:workers:safepay-payout-worker"
		acquired, err := w.redis.SetNX(ctx, lockKey, "1", 90*time.Second).Result()
		if err != nil || !acquired {
			return
		}
		defer w.redis.Del(ctx, lockKey)
	}

	w.sweepVendorPayouts(ctx)
	w.sweepRiderPayouts(ctx)
	w.sweepCustomerPayouts(ctx)
}

// ── 1. Vendor Payouts Sweep (Enforces 7-Day Maturity Rule) ───────────────────
func (w *SafepayPayoutWorker) sweepVendorPayouts(ctx context.Context) {
	if !w.safepay.IsConfigured() {
		return
	}

	query := `
		SELECT id, vendor_tracking_id, amount_paisa, payout_fee_paisa,
		       COALESCE(beneficiary_name, ''), COALESCE(beneficiary_iban, ''), COALESCE(bank_name, '')
		FROM vendor_payouts
		WHERE status = 'scheduled'
		  AND scheduled_at <= NOW()
		  AND beneficiary_iban IS NOT NULL
		  AND beneficiary_iban != ''
		ORDER BY scheduled_at ASC
		LIMIT 20
		FOR UPDATE SKIP LOCKED
	`

	rows, err := w.db.Query(ctx, query)
	if err != nil {
		log.Printf("[SafepayPayoutWorker] Error querying matured vendor payouts: %v", err)
		return
	}
	defer rows.Close()

	type vendorPayoutJob struct {
		id              uuid.UUID
		vendorID        string
		amountPaisa     int64
		feePaisa        int64
		beneficiaryName string
		beneficiaryIBAN string
		bankName        string
	}

	var jobs []vendorPayoutJob
	for rows.Next() {
		var j vendorPayoutJob
		if err := rows.Scan(&j.id, &j.vendorID, &j.amountPaisa, &j.feePaisa, &j.beneficiaryName, &j.beneficiaryIBAN, &j.bankName); err == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()

	for _, job := range jobs {
		netPaisa := job.amountPaisa - job.feePaisa
		if netPaisa <= 0 {
			log.Printf("[SafepayPayoutWorker] Skipping vendor payout %s: net amount <= 0", job.id)
			continue
		}
		netRupees := fmt.Sprintf("%.2f", float64(netPaisa)/100.0)

		payoutReq := safepay.PayoutRequest{
			RequestID:       job.id.String(),
			Amount:          netRupees,
			CreditorIBAN:    job.beneficiaryIBAN,
			BeneficiaryName: job.beneficiaryName,
			Narration:       fmt.Sprintf("OMNIGO Vendor Payout %s", job.vendorID),
		}

		resp, err := w.safepay.InitiatePayout(ctx, payoutReq)
		if err != nil {
			log.Printf("[SafepayPayoutWorker] Vendor payout %s failed via Safepay: %v", job.id, err)
			_, _ = w.db.Exec(ctx,
				`UPDATE vendor_payouts SET status = 'failed', failure_reason = $2, updated_at = NOW() WHERE id = $1`,
				job.id, err.Error(),
			)
			// Compensate vendor wallet on permanent failure
			_, _ = w.db.Exec(ctx,
				`UPDATE vendor_wallet SET balance_paisa = balance_paisa + $1, total_payouts_paisa = total_payouts_paisa - $1 WHERE vendor_tracking_id = $2`,
				job.amountPaisa, job.vendorID,
			)
			continue
		}

		status := "completed"
		if resp.Status != "" {
			status = resp.Status
		}

		_, _ = w.db.Exec(ctx,
			`UPDATE vendor_payouts
			 SET status = $2, trace_reference = $3, completed_at = NOW(), updated_at = NOW()
			 WHERE id = $1`,
			job.id, status, resp.TraceReference,
		)

		log.Printf("✓ [SafepayPayoutWorker] Vendor %s payout %s fulfilled (%s PKR) via Raastwire [trace: %s]",
			job.vendorID, job.id, netRupees, resp.TraceReference)
	}
}

// ── 2. Rider Payouts Sweep ───────────────────────────────────────────────────
func (w *SafepayPayoutWorker) sweepRiderPayouts(ctx context.Context) {
	if !w.safepay.IsConfigured() {
		return
	}

	query := `
		SELECT id, rider_tracking_id, amount_paisa,
		       COALESCE(account_title, ''), COALESCE(beneficiary_iban, account_number, '')
		FROM rider_payouts
		WHERE status = 'pending'
		  AND (beneficiary_iban IS NOT NULL OR account_number LIKE 'PK%')
		ORDER BY created_at ASC
		LIMIT 20
		FOR UPDATE SKIP LOCKED
	`

	rows, err := w.db.Query(ctx, query)
	if err != nil {
		log.Printf("[SafepayPayoutWorker] Error querying pending rider payouts: %v", err)
		return
	}
	defer rows.Close()

	type riderPayoutJob struct {
		id              uuid.UUID
		riderID         string
		amountPaisa     int64
		accountTitle    string
		beneficiaryIBAN string
	}

	var jobs []riderPayoutJob
	for rows.Next() {
		var j riderPayoutJob
		if err := rows.Scan(&j.id, &j.riderID, &j.amountPaisa, &j.accountTitle, &j.beneficiaryIBAN); err == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()

	for _, job := range jobs {
		amountRupees := fmt.Sprintf("%.2f", float64(job.amountPaisa)/100.0)

		payoutReq := safepay.PayoutRequest{
			RequestID:       job.id.String(),
			Amount:          amountRupees,
			CreditorIBAN:    job.beneficiaryIBAN,
			BeneficiaryName: job.accountTitle,
			Narration:       fmt.Sprintf("OMNIGO Rider Payout %s", job.riderID),
		}

		resp, err := w.safepay.InitiatePayout(ctx, payoutReq)
		if err != nil {
			log.Printf("[SafepayPayoutWorker] Rider payout %s failed: %v", job.id, err)
			_, _ = w.db.Exec(ctx,
				`UPDATE rider_payouts SET status = 'failed', failure_reason = $2, updated_at = NOW() WHERE id = $1`,
				job.id, err.Error(),
			)
			// Compensate rider wallet
			_, _ = w.db.Exec(ctx,
				`UPDATE rider_wallet SET balance_paisa = balance_paisa + $1, balance = (balance_paisa + $1)::decimal / 100.0 WHERE rider_tracking_id = $2`,
				job.amountPaisa, job.riderID,
			)
			continue
		}

		status := "completed"
		if resp.Status != "" {
			status = resp.Status
		}

		_, _ = w.db.Exec(ctx,
			`UPDATE rider_payouts
			 SET status = $2, trace_reference = $3, completed_at = NOW(), updated_at = NOW()
			 WHERE id = $1`,
			job.id, status, resp.TraceReference,
		)

		log.Printf("✓ [SafepayPayoutWorker] Rider %s payout %s fulfilled (%s PKR) via Raastwire [trace: %s]",
			job.riderID, job.id, amountRupees, resp.TraceReference)
	}
}

// ── 3. Customer Bank Payouts Sweep ───────────────────────────────────────────
func (w *SafepayPayoutWorker) sweepCustomerPayouts(ctx context.Context) {
	if !w.safepay.IsConfigured() {
		return
	}

	query := `
		SELECT id, customer_tracking_id, amount_paisa,
		       COALESCE(beneficiary_name, ''), COALESCE(beneficiary_iban, '')
		FROM customer_payouts
		WHERE status = 'pending'
		  AND beneficiary_iban IS NOT NULL
		  AND beneficiary_iban != ''
		ORDER BY created_at ASC
		LIMIT 20
		FOR UPDATE SKIP LOCKED
	`

	rows, err := w.db.Query(ctx, query)
	if err != nil {
		log.Printf("[SafepayPayoutWorker] Error querying pending customer payouts: %v", err)
		return
	}
	defer rows.Close()

	type customerPayoutJob struct {
		id              uuid.UUID
		customerID      string
		amountPaisa     int64
		beneficiaryName string
		beneficiaryIBAN string
	}

	var jobs []customerPayoutJob
	for rows.Next() {
		var j customerPayoutJob
		if err := rows.Scan(&j.id, &j.customerID, &j.amountPaisa, &j.beneficiaryName, &j.beneficiaryIBAN); err == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()

	for _, job := range jobs {
		amountRupees := fmt.Sprintf("%.2f", float64(job.amountPaisa)/100.0)

		payoutReq := safepay.PayoutRequest{
			RequestID:       job.id.String(),
			Amount:          amountRupees,
			CreditorIBAN:    job.beneficiaryIBAN,
			BeneficiaryName: job.beneficiaryName,
			Narration:       fmt.Sprintf("OMNIGO Customer Bank Disbursement %s", job.customerID),
		}

		resp, err := w.safepay.InitiatePayout(ctx, payoutReq)
		if err != nil {
			log.Printf("[SafepayPayoutWorker] Customer payout %s failed: %v", job.id, err)
			_, _ = w.db.Exec(ctx,
				`UPDATE customer_payouts SET status = 'failed', failure_reason = $2, updated_at = NOW() WHERE id = $1`,
				job.id, err.Error(),
			)
			// Compensate customer wallet
			_, _ = w.db.Exec(ctx,
				`UPDATE customer_wallet SET balance_paisa = balance_paisa + $1, updated_at = NOW() WHERE customer_tracking_id = $2`,
				job.amountPaisa, job.customerID,
			)
			continue
		}

		status := "completed"
		if resp.Status != "" {
			status = resp.Status
		}

		_, _ = w.db.Exec(ctx,
			`UPDATE customer_payouts
			 SET status = $2, trace_reference = $3, completed_at = NOW(), updated_at = NOW()
			 WHERE id = $1`,
			job.id, status, resp.TraceReference,
		)

		log.Printf("✓ [SafepayPayoutWorker] Customer %s payout %s fulfilled (%s PKR) via Raastwire [trace: %s]",
			job.customerID, job.id, amountRupees, resp.TraceReference)
	}
}
