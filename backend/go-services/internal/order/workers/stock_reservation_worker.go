package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/omnigo/backend/internal/order/models"
	"github.com/omnigo/backend/internal/order/repository"
	"github.com/omnigo/backend/internal/product/pb"
)

// StockReservationWorker processes local stock reservations by calling
// the product service gRPC to confirm them. It provides guaranteed
// compensation via persistent reservation records.

type StockReservationWorker struct {
	db           *pgxpool.Pool
	repo         *repository.OrderRepository
	productGRPC  pb.ProductInventoryServiceClient
	pollInterval time.Duration
}

func NewStockReservationWorker(
	db *pgxpool.Pool,
	repo *repository.OrderRepository,
	productGRPC pb.ProductInventoryServiceClient,
	pollInterval time.Duration,
) *StockReservationWorker {
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}
	return &StockReservationWorker{
		db:           db,
		repo:         repo,
		productGRPC:  productGRPC,
		pollInterval: pollInterval,
	}
}

func (w *StockReservationWorker) Start(ctx context.Context) {
	log.Println("[StockReservationWorker] Starting...")

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[StockReservationWorker] Stopping...")
			return
		case <-ticker.C:
			w.processPending(ctx)
		}
	}
}

func (w *StockReservationWorker) processPending(ctx context.Context) {
	reservations, err := w.repo.GetPendingStockReservations(ctx, 50)
	if err != nil {
		log.Printf("[StockReservationWorker] Failed to fetch pending reservations: %v", err)
		return
	}

	if len(reservations) == 0 {
		return
	}

	log.Printf("[StockReservationWorker] Processing %d pending reservations", len(reservations))

	// Group by order to make single gRPC call per order
	byOrder := make(map[string][]models.StockReservation)
	for _, res := range reservations {
		byOrder[res.OrderTrackingID] = append(byOrder[res.OrderTrackingID], res)
	}

	for orderID, items := range byOrder {
		w.processOrderReservations(ctx, orderID, items)
	}
}

func (w *StockReservationWorker) processOrderReservations(ctx context.Context, orderID string, items []models.StockReservation) {
	// Build gRPC request
	var pbItems []*pb.OrderItem
	for _, item := range items {
		pbItems = append(pbItems, &pb.OrderItem{
			ProductTrackingId: item.ProductTrackingID,
			Quantity:          int32(item.Quantity),
		})
	}

	grpcReq := &pb.ReserveRequest{
		Items:           pbItems,
		OrderTrackingId: orderID,
	}

	res, err := w.productGRPC.ReserveProduct(ctx, grpcReq)
	if err != nil {
		log.Printf("[StockReservationWorker] gRPC ReserveProduct failed for order %s: %v", orderID, err)
		for _, item := range items {
			w.repo.FailStockReservation(ctx, orderID, item.ProductTrackingID, err.Error())
		}
		return
	}

	if !res.Success {
		log.Printf("[StockReservationWorker] ReserveProduct returned failure for order %s: %s", orderID, res.Message)
		for _, item := range items {
			w.repo.FailStockReservation(ctx, orderID, item.ProductTrackingID, res.Message)
		}
		w.handleReservationFailure(ctx, orderID, res.Message)
		return
	}

	// Success: confirm all reservations and update order with prices
	if err := w.handleReservationSuccess(ctx, orderID, items, res); err != nil {
		log.Printf("[StockReservationWorker] Failed to handle success for order %s: %v", orderID, err)
	}
}

// handleReservationFailure handles out-of-stock or inventory reservation rejection.
// It fails the order, cancels any associated delivery gig, and if the order was prepaid,
// queues a 100% refund into outbox_events to credit the customer's wallet immediately.
func (w *StockReservationWorker) handleReservationFailure(ctx context.Context, orderID, reason string) {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		log.Printf("[StockReservationWorker] Failed to begin tx on failure for order %s: %v", orderID, err)
		return
	}
	defer tx.Rollback(ctx)

	var paymentStatus string
	var totalBilledPaisa int64
	var totalAmount float64
	var currency string
	err = tx.QueryRow(ctx,
		`SELECT payment_status, COALESCE(total_billed_amount_paisa, 0),
		        COALESCE(total_amount, 0), COALESCE(currency, 'PKR')
		 FROM orders WHERE order_tracking_id = $1`,
		orderID,
	).Scan(&paymentStatus, &totalBilledPaisa, &totalAmount, &currency)
	if err != nil {
		log.Printf("[StockReservationWorker] Failed to query order %s on failure: %v", orderID, err)
		return
	}

	newPaymentStatus := "failed"
	if paymentStatus == "paid" {
		newPaymentStatus = "refund_pending"
	}

	// 1. Mark order failed
	_, err = tx.Exec(ctx,
		`UPDATE orders SET status = 'failed', payment_status = $1, updated_at = NOW()
		 WHERE order_tracking_id = $2`,
		newPaymentStatus, orderID,
	)
	if err != nil {
		log.Printf("[StockReservationWorker] Failed to update order %s status to failed: %v", orderID, err)
		return
	}

	// 2. Cancel delivery gig
	_, _ = tx.Exec(ctx,
		`UPDATE deliveries SET status = 'cancelled', updated_at = NOW()
		 WHERE order_tracking_id = $1 AND status NOT IN ('completed', 'cancelled')`,
		orderID,
	)

	// 3. If order was prepaid, enqueue 100% refund into outbox_events
	if paymentStatus == "paid" {
		refundAmount := totalAmount
		if totalBilledPaisa > 0 {
			refundAmount = float64(totalBilledPaisa) / 100.0
		}
		payload := map[string]interface{}{
			"order_id":      orderID,
			"reason":        "out_of_stock_auto_reject: " + reason,
			"refund_amount": refundAmount,
			"currency":      currency,
			"refund_to":     "original",
			"timestamp":     time.Now().UnixMilli(),
		}
		payloadBytes, _ := json.Marshal(payload)
		_, err = tx.Exec(ctx,
			`INSERT INTO outbox_events (aggregate_id, topic, payload, status, created_at, updated_at)
			 VALUES ($1, 'payment_refund', $2::jsonb, 'PENDING', NOW(), NOW())`,
			orderID, payloadBytes,
		)
		if err != nil {
			log.Printf("[StockReservationWorker] Failed to enqueue refund outbox for order %s: %v", orderID, err)
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		log.Printf("[StockReservationWorker] Failed to commit failure tx for order %s: %v", orderID, err)
		return
	}

	log.Printf("[StockReservationWorker] Handled stock reservation failure for %s (paid=%v, reason=%s)",
		orderID, paymentStatus == "paid", reason)
}

func (w *StockReservationWorker) handleReservationSuccess(
	ctx context.Context,
	orderID string,
	localItems []models.StockReservation,
	res *pb.ReserveResponse,
) error {
	// Mark all local reservations as confirmed
	for _, item := range localItems {
		if err := w.repo.ConfirmStockReservation(ctx, orderID, item.ProductTrackingID, ""); err != nil {
			log.Printf("[StockReservationWorker] Failed to confirm reservation for %s/%s: %v", orderID, item.ProductTrackingID, err)
		}
	}

	// Update order with vendor info and item prices from gRPC response
	if res.VendorTrackingId != "" && res.StoreTrackingId != "" {
		tx, err := w.db.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin tx for order update: %w", err)
		}
		defer tx.Rollback(ctx)

		// Update vendor tracking ID and store tracking ID
		_, err = tx.Exec(ctx, `
			UPDATE orders
			SET vendor_tracking_id = $1, store_tracking_id = $2, updated_at = NOW()
			WHERE order_tracking_id = $3
		`, res.VendorTrackingId, res.StoreTrackingId, orderID)
		if err != nil {
			return fmt.Errorf("failed to update order vendor info: %w", err)
		}

		// Update item prices from gRPC response
		priceMap := make(map[string]float64)
		for _, rItem := range res.Items {
			priceMap[rItem.ProductTrackingId] = rItem.PriceAtCheckout
		}

		for _, localItem := range localItems {
			if price, ok := priceMap[localItem.ProductTrackingID]; ok {
				_, err = tx.Exec(ctx, `
					UPDATE order_items
					SET price_at_checkout = $1, updated_at = NOW()
					WHERE order_tracking_id = $2 AND product_tracking_id = $3
				`, price, orderID, localItem.ProductTrackingID)
				if err != nil {
					log.Printf("[StockReservationWorker] Failed to update price for %s/%s: %v", orderID, localItem.ProductTrackingID, err)
				}
			}
		}

		// Only set total_amount if it was uninitialized (0), do NOT overwrite authorized checkout amount!
		var currentTotal float64
		_ = tx.QueryRow(ctx, "SELECT COALESCE(total_amount, 0) FROM orders WHERE order_tracking_id = $1", orderID).Scan(&currentTotal)
		if currentTotal <= 0 {
			var total float64
			for _, rItem := range res.Items {
				total += rItem.PriceAtCheckout * float64(rItem.Quantity)
			}
			_, err = tx.Exec(ctx, `
				UPDATE orders
				SET total_amount = $1, updated_at = NOW()
				WHERE order_tracking_id = $2
			`, total, orderID)
			if err != nil {
				return fmt.Errorf("failed to update order total: %w", err)
			}
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit order updates: %w", err)
		}

		log.Printf("[StockReservationWorker] Order %s confirmed: vendor=%s, store=%s",
			orderID, res.VendorTrackingId, res.StoreTrackingId)
	}

	return nil
}

// ReleaseOrderReservations releases all reservations for an order (compensation).
// Called when order is cancelled before reservations are confirmed.
func (w *StockReservationWorker) ReleaseOrderReservations(ctx context.Context, orderID string) error {
	reservations, err := w.repo.GetStockReservationsByOrder(ctx, orderID)
	if err != nil {
		return err
	}

	for _, res := range reservations {
		if res.Status == "pending" || res.Status == "confirmed" {
			grpcReq := &pb.ReleaseRequest{
				Items: []*pb.OrderItem{{
					ProductTrackingId: res.ProductTrackingID,
					Quantity:          int32(res.Quantity),
				}},
				OrderTrackingId: orderID,
			}
			if _, err := w.productGRPC.ReleaseProduct(ctx, grpcReq); err != nil {
				log.Printf("[StockReservationWorker] Failed to release %s/%s via gRPC: %v", orderID, res.ProductTrackingID, err)
			}
			_ = w.repo.ReleaseStockReservation(ctx, orderID, res.ProductTrackingID)
		}
	}
	return nil
}