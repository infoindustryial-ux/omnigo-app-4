package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omnigo/backend/internal/payment/safepay"
	"github.com/omnigo/backend/internal/payment_orchestrator/service"
)

// SafepayWebhookHandler processes asynchronous payout notifications and payment collection webhooks from Safepay.
type SafepayWebhookHandler struct {
	db         *pgxpool.Pool
	safepay    *safepay.Client
	safepaySvc *service.SafepayService
}

func NewSafepayWebhookHandler(db *pgxpool.Pool, client *safepay.Client) *SafepayWebhookHandler {
	if client == nil {
		client = safepay.NewClientFromEnv()
	}
	return &SafepayWebhookHandler{
		db:      db,
		safepay: client,
	}
}

// SetSafepayService attaches SafepayService for automated settlement.
func (h *SafepayWebhookHandler) SetSafepayService(svc *service.SafepayService) {
	h.safepaySvc = svc
}

type SafepayPayoutWebhookPayload struct {
	Event string `json:"event"`
	Data  struct {
		RequestID      string `json:"request_id"`
		TraceReference string `json:"trace_reference"`
		Status         string `json:"status"` // "completed", "failed", "reversed"
		FailureReason  string `json:"failure_reason,omitempty"`
		Amount         string `json:"amount,omitempty"`
	} `json:"data"`
}

// HandlePayoutCallback handles POST /api/v1/payments/safepay/payout/callback
func (h *SafepayWebhookHandler) HandlePayoutCallback(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read request body"})
		return
	}

	sigHeader := c.GetHeader("X-SFPY-SIGNATURE")
	if sigHeader == "" {
		sigHeader = c.GetHeader("X-Safepay-Signature")
	}

	// Verify webhook HMAC signature if secret is configured
	if h.safepay.IsConfigured() && sigHeader != "" {
		if !h.safepay.VerifyWebhookSignature(bodyBytes, sigHeader) {
			log.Printf("[SafepayWebhook] WARNING: Webhook signature verification failed")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook signature"})
			return
		}
	}

	var payload SafepayPayoutWebhookPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json payload"})
		return
	}

	reqID := payload.Data.RequestID
	if reqID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing request_id"})
		return
	}

	status := strings.ToLower(payload.Data.Status)
	traceRef := payload.Data.TraceReference
	failureReason := payload.Data.FailureReason

	ctx := c.Request.Context()

	if status == "completed" || status == "success" {
		// Try updating vendor_payouts
		vTag, _ := h.db.Exec(ctx,
			`UPDATE vendor_payouts
			 SET status = 'completed', trace_reference = COALESCE(NULLIF($2, ''), trace_reference), completed_at = NOW(), updated_at = NOW()
			 WHERE id = $1 AND status != 'completed'`,
			reqID, traceRef,
		)

		// Try updating rider_payouts
		rTag, _ := h.db.Exec(ctx,
			`UPDATE rider_payouts
			 SET status = 'completed', trace_reference = COALESCE(NULLIF($2, ''), trace_reference), completed_at = NOW(), updated_at = NOW()
			 WHERE id = $1 AND status != 'completed'`,
			reqID, traceRef,
		)

		// Try updating customer_payouts
		cTag, _ := h.db.Exec(ctx,
			`UPDATE customer_payouts
			 SET status = 'completed', trace_reference = COALESCE(NULLIF($2, ''), trace_reference), completed_at = NOW(), updated_at = NOW()
			 WHERE id = $1 AND status != 'completed'`,
			reqID, traceRef,
		)

		log.Printf("✓ [SafepayWebhook] Payout %s marked completed (v:%d, r:%d, c:%d)",
			reqID, vTag.RowsAffected(), rTag.RowsAffected(), cTag.RowsAffected())
	} else if status == "failed" || status == "reversed" {
		// Vendor failure compensation
		var vendorID string
		var amountPaisa int64
		err := h.db.QueryRow(ctx,
			`UPDATE vendor_payouts
			 SET status = 'failed', failure_reason = $2, updated_at = NOW()
			 WHERE id = $1 AND status != 'failed'
			 RETURNING vendor_tracking_id, amount_paisa`,
			reqID, failureReason,
		).Scan(&vendorID, &amountPaisa)
		if err == nil && vendorID != "" {
			_, _ = h.db.Exec(ctx,
				`UPDATE vendor_wallet
				 SET balance_paisa = balance_paisa + $1, total_payouts_paisa = total_payouts_paisa - $1
				 WHERE vendor_tracking_id = $2`,
				amountPaisa, vendorID,
			)
			log.Printf("[SafepayWebhook] Vendor payout %s failed; refunded %d paisa to vendor %s", reqID, amountPaisa, vendorID)
		}

		// Rider failure compensation
		var riderID string
		err = h.db.QueryRow(ctx,
			`UPDATE rider_payouts
			 SET status = 'failed', failure_reason = $2, updated_at = NOW()
			 WHERE id = $1 AND status != 'failed'
			 RETURNING rider_tracking_id, amount_paisa`,
			reqID, failureReason,
		).Scan(&riderID, &amountPaisa)
		if err == nil && riderID != "" {
			_, _ = h.db.Exec(ctx,
				`UPDATE rider_wallet
				 SET balance_paisa = balance_paisa + $1, balance = (balance_paisa + $1)::decimal / 100.0
				 WHERE rider_tracking_id = $2`,
				amountPaisa, riderID,
			)
			log.Printf("[SafepayWebhook] Rider payout %s failed; refunded %d paisa to rider %s", reqID, amountPaisa, riderID)
		}

		// Customer failure compensation
		var customerID string
		err = h.db.QueryRow(ctx,
			`UPDATE customer_payouts
			 SET status = 'failed', failure_reason = $2, updated_at = NOW()
			 WHERE id = $1 AND status != 'failed'
			 RETURNING customer_tracking_id, amount_paisa`,
			reqID, failureReason,
		).Scan(&customerID, &amountPaisa)
		if err == nil && customerID != "" {
			_, _ = h.db.Exec(ctx,
				`UPDATE customer_wallet
				 SET balance_paisa = balance_paisa + $1, updated_at = NOW()
				 WHERE customer_tracking_id = $2`,
				amountPaisa, customerID,
			)
			log.Printf("[SafepayWebhook] Customer payout %s failed; refunded %d paisa to customer %s", reqID, amountPaisa, customerID)
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "received", "request_id": reqID})
}

// HandlePaymentWebhook processes incoming payment collection webhooks from Safepay.
func (h *SafepayWebhookHandler) HandlePaymentWebhook(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read request body"})
		return
	}

	sigHeader := c.GetHeader("X-SFPY-SIGNATURE")
	if sigHeader == "" {
		sigHeader = c.GetHeader("X-Safepay-Signature")
	}

	if h.safepay.IsConfigured() && sigHeader != "" {
		if !h.safepay.VerifyWebhookSignature(bodyBytes, sigHeader) {
			log.Printf("[SafepayWebhook] WARNING: Payment webhook signature verification failed")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook signature"})
			return
		}
	}

	var payload struct {
		Event string `json:"event"`
		Data  struct {
			Token    string            `json:"token"`
			State    string            `json:"state"`
			Amount   int64             `json:"amount"`
			Currency string            `json:"currency"`
			Metadata map[string]string `json:"metadata"`
		} `json:"data"`
	}

	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json payload"})
		return
	}

	state := strings.ToLower(payload.Data.State)
	orderID := payload.Data.Metadata["order_id"]
	trackerToken := payload.Data.Token
	amountPaisa := payload.Data.Amount

	if state == "paid" || state == "completed" || state == "captured" {
		ctx := c.Request.Context()
		if h.safepaySvc != nil && orderID != "" {
			var internalTxnID string
			_ = h.db.QueryRow(ctx,
				`SELECT transaction_id FROM payment_transactions 
				 WHERE order_tracking_id = $1 AND gateway = 'safepay' 
				 ORDER BY created_at DESC LIMIT 1`,
				orderID,
			).Scan(&internalTxnID)

			if internalTxnID == "" {
				internalTxnID = "sp_wh_" + trackerToken
			}

			if err := h.safepaySvc.VerifyAndSettle(ctx, internalTxnID, orderID, trackerToken, amountPaisa); err != nil {
				log.Printf("ERROR [SafepayWebhook] Settlement failed for order %s: %v", orderID, err)
			}
		} else if orderID != "" {
			_, _ = h.db.Exec(ctx,
				`UPDATE orders SET payment_status = 'paid', status = 'confirmed', updated_at = NOW() WHERE order_tracking_id = $1`,
				orderID,
			)
			_, _ = h.db.Exec(ctx,
				`UPDATE payment_transactions SET status = 'captured', updated_at = NOW() WHERE order_tracking_id = $1 AND gateway = 'safepay'`,
				orderID,
			)
		}
		log.Printf("✓ [SafepayWebhook] Payment for order %s settled successfully (tracker: %s)", orderID, trackerToken)
	}

	c.JSON(http.StatusOK, gin.H{"status": "received", "tracker": trackerToken})
}

// RegisterRoutes registers the Safepay webhook callback routes.
func (h *SafepayWebhookHandler) RegisterRoutes(router *gin.Engine) {
	router.POST("/api/v1/payments/safepay/payout/callback", h.HandlePayoutCallback)
	router.POST("/api/v1/payments/safepay/webhook", h.HandlePaymentWebhook)
}
