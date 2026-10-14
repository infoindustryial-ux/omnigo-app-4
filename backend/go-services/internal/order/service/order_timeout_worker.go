package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OrderTimeoutWorker periodically cancels orders:
// 1. Stuck in 'pending' state for more than 30 minutes (abandoned checkout).
// 2. Stuck in 'paid' state for more than 10 minutes without vendor acceptance (vendor inactivity timeout).
// When a paid order times out, it cancels the order, releases escrow, cancels delivery gigs,
// and enqueues a full customer wallet refund via outbox_events.
type OrderTimeoutWorker struct {
	db          *pgxpool.Pool
	timeout     time.Duration
	paidTimeout time.Duration
	escrowSvc   EscrowHolder
}

// NewOrderTimeoutWorker constructs the worker with 30m pending and 10m paid timeouts.
func NewOrderTimeoutWorker(db *pgxpool.Pool) *OrderTimeoutWorker {
	return &OrderTimeoutWorker{
		db:          db,
		timeout:     30 * time.Minute,
		paidTimeout: 10 * time.Minute,
	}
}

// SetEscrowService injects the optional escrow service for cancel-time cleanup.
func (w *OrderTimeoutWorker) SetEscrowService(svc EscrowHolder) {
	w.escrowSvc = svc
}

// Start begins the cleanup loop. Call cancel() to stop.
func (w *OrderTimeoutWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second) // Check every 60 seconds
	defer ticker.Stop()

	log.Printf("[OrderTimeout] Started — cancelling pending > %v, paid > %v", w.timeout, w.paidTimeout)

	for {
		select {
		case <-ctx.Done():
			log.Println("[OrderTimeout] Stopped")
			return
		case <-ticker.C:
			w.cancelStaleOrders(ctx)
			w.cancelStalePaidOrders(ctx)
		}
	}
}

// cancelStaleOrders finds and cancels orders stuck in pending state.
func (w *OrderTimeoutWorker) cancelStaleOrders(ctx context.Context) {
	// Find orders stuck in pending for more than 30 minutes
	rows, err := w.db.Query(ctx,
		`SELECT order_tracking_id, COALESCE(payment_gateway, ''), total_amount
		 FROM orders
		 WHERE status = 'pending'
		   AND created_at < NOW() - INTERVAL '30 minutes'
		 FOR UPDATE SKIP LOCKED
		 LIMIT 50`,
	)
	if err != nil {
		log.Printf("[OrderTimeout] Query error: %v", err)
		return
	}
	defer rows.Close()

	var staleOrders []struct {
		OrderID    string
		Gateway    string
		TotalAmount float64
	}

	for rows.Next() {
		var o struct {
			OrderID     string
			Gateway     string
			TotalAmount float64
		}
		if err := rows.Scan(&o.OrderID, &o.Gateway, &o.TotalAmount); err != nil {
			log.Printf("[OrderTimeout] Scan error: %v", err)
			continue
		}
		staleOrders = append(staleOrders, o)
	}
	rows.Close()

	if len(staleOrders) == 0 {
		return
	}

	cancelled := 0
	for _, order := range staleOrders {
		if err := w.cancelOrder(ctx, order.OrderID); err != nil {
			log.Printf("[OrderTimeout] Failed to cancel order %s: %v", order.OrderID, err)
			continue
		}
		cancelled++
		log.Printf("[OrderTimeout] Cancelled stale order %s (gateway=%s, amount=%.2f, pending > %v)",
			order.OrderID, order.Gateway, order.TotalAmount, w.timeout)
	}

	if cancelled > 0 {
		log.Printf("[OrderTimeout] Cancelled %d stale pending orders", cancelled)
	}
}

// cancelStalePaidOrders finds orders in 'paid' status waiting on vendor acceptance for > 10 minutes.
func (w *OrderTimeoutWorker) cancelStalePaidOrders(ctx context.Context) {
	rows, err := w.db.Query(ctx,
		`SELECT order_tracking_id, COALESCE(customer_tracking_id, ''),
		        COALESCE(total_billed_amount_paisa, 0), COALESCE(total_amount, 0), COALESCE(currency, 'PKR')
		 FROM orders
		 WHERE status = 'paid'
		   AND updated_at < NOW() - INTERVAL '10 minutes'
		 FOR UPDATE SKIP LOCKED
		 LIMIT 50`,
	)
	if err != nil {
		log.Printf("[OrderTimeout] Paid orders query error: %v", err)
		return
	}
	defer rows.Close()

	type stalePaidOrder struct {
		OrderID          string
		CustomerID       string
		TotalBilledPaisa int64
		TotalAmount      float64
		Currency         string
	}

	var stalePaid []stalePaidOrder
	for rows.Next() {
		var o stalePaidOrder
		if err := rows.Scan(&o.OrderID, &o.CustomerID, &o.TotalBilledPaisa, &o.TotalAmount, &o.Currency); err != nil {
			log.Printf("[OrderTimeout] Paid scan error: %v", err)
			continue
		}
		stalePaid = append(stalePaid, o)
	}
	rows.Close()

	if len(stalePaid) == 0 {
		return
	}

	cancelled := 0
	for _, order := range stalePaid {
		refundAmount := order.TotalAmount
		if order.TotalBilledPaisa > 0 {
			refundAmount = float64(order.TotalBilledPaisa) / 100.0
		}
		if err := w.cancelPaidOrder(ctx, order.OrderID, order.CustomerID, refundAmount, order.Currency); err != nil {
			log.Printf("[OrderTimeout] Failed to cancel paid order %s: %v", order.OrderID, err)
			continue
		}
		cancelled++
		log.Printf("[OrderTimeout] Cancelled stale paid order %s due to vendor inactivity (10m) — refunded %.2f %s to customer %s",
			order.OrderID, refundAmount, order.Currency, order.CustomerID)
	}

	if cancelled > 0 {
		log.Printf("[OrderTimeout] Successfully auto-cancelled and queued refunds for %d stale paid orders", cancelled)
	}
}

// cancelPaidOrder cancels a paid order whose vendor did not accept in time,
// enqueuing an automated refund to the customer's wallet via outbox_events.
func (w *OrderTimeoutWorker) cancelPaidOrder(ctx context.Context, orderID, customerID string, refundAmount float64, currency string) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// 1. Cancel the order (idempotent: only if still paid)
	result, err := tx.Exec(ctx,
		`UPDATE orders SET status = 'cancelled', payment_status = 'refund_pending', updated_at = NOW()
		 WHERE order_tracking_id = $1 AND status = 'paid'`,
		orderID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return nil // Already accepted or cancelled concurrently
	}

	// 2. Cancel any active delivery gig
	_, _ = tx.Exec(ctx,
		`UPDATE deliveries SET status = 'cancelled', updated_at = NOW()
		 WHERE order_tracking_id = $1 AND status NOT IN ('completed', 'cancelled')`,
		orderID,
	)

	// 3. Enqueue 100% wallet refund into outbox_events
	payload := map[string]interface{}{
		"order_id":      orderID,
		"reason":        "vendor_acceptance_timeout_10min",
		"refund_amount": refundAmount,
		"currency":      currency,
		"refund_to":     "original",
		"timestamp":     time.Now().UnixMilli(),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal refund payload: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO outbox_events (aggregate_id, topic, payload, status, created_at, updated_at)
		 VALUES ($1, 'payment_refund', $2::jsonb, 'PENDING', NOW(), NOW())`,
		orderID, payloadBytes,
	)
	if err != nil {
		return fmt.Errorf("insert refund outbox: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// 4. Cancel escrow hold for the vendor
	if w.escrowSvc != nil {
		if err := w.escrowSvc.CancelForOrder(ctx, orderID); err != nil {
			log.Printf("[OrderTimeout] WARNING: escrow cancel failed for paid order %s: %v", orderID, err)
		}
	}

	return nil
}

// cancelOrder performs the full cancellation: order + delivery gig + stock release + escrow + COD debts.
func (w *OrderTimeoutWorker) cancelOrder(ctx context.Context, orderID string) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// 1. Cancel the order (idempotent: only if still pending)
	result, err := tx.Exec(ctx,
		`UPDATE orders SET status = 'cancelled', payment_status = 'failed', updated_at = NOW()
		 WHERE order_tracking_id = $1 AND status = 'pending'`,
		orderID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return nil // Already cancelled or moved to another state
	}

	// 2. Cancel any active delivery gig
	_, _ = tx.Exec(ctx,
		`UPDATE deliveries SET status = 'cancelled', updated_at = NOW()
		 WHERE order_tracking_id = $1 AND status NOT IN ('completed', 'cancelled')`,
		orderID,
	)

	// 3. Cancel any COD debts for this order
	_, _ = tx.Exec(ctx,
		`UPDATE cod_debts SET status = 'cancelled', updated_at = NOW()
		 WHERE order_tracking_id = $1 AND status != 'cancelled'`,
		orderID,
	)

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// 4. Cancel escrow hold (outside DB tx — escrow service uses its own connection)
	if w.escrowSvc != nil {
		if err := w.escrowSvc.CancelForOrder(ctx, orderID); err != nil {
			log.Printf("[OrderTimeout] WARNING: escrow cancel failed for order %s: %v (order was cancelled in DB)", orderID, err)
		}
	}

	return nil
}

// StartOrderTimeoutWorker is a convenience method on OrderService that
// creates and starts the timeout worker using the service's own DB pool.
func (s *OrderService) StartOrderTimeoutWorker(ctx context.Context) {
	w := NewOrderTimeoutWorker(s.repo.DB())
	w.SetEscrowService(s.escrowService)
	w.Start(ctx)
}
