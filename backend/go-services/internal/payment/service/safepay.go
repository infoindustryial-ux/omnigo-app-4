package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/omnigo/backend/internal/payment/safepay"
)

// SafepayService implements PaymentGateway for Safepay Pakistan.
type SafepayService struct {
	client *safepay.Client
}

// NewSafepayService constructs a new SafepayService wrapper.
func NewSafepayService(client *safepay.Client) *SafepayService {
	if client == nil {
		client = safepay.NewClientFromEnv()
	}
	return &SafepayService{
		client: client,
	}
}

// IsConfigured reports whether Safepay has necessary credentials.
func (s *SafepayService) IsConfigured() bool {
	return s.client != nil && s.client.IsConfigured()
}

// CreateCheckoutSession generates a Safepay payment tracker and returns the hosted checkout URL.
func (s *SafepayService) CreateCheckoutSession(ctx context.Context, req CheckoutRequest) (CheckoutResponse, error) {
	if !s.IsConfigured() {
		return CheckoutResponse{}, errors.New("safepay: gateway not configured")
	}

	amountPaisa := int64(math.Round(req.Amount * 100))
	if amountPaisa <= 0 {
		return CheckoutResponse{}, errors.New("safepay: invalid checkout amount")
	}

	metadata := map[string]string{
		"order_id": req.OrderID,
	}
	if req.CustomerID != "" {
		metadata["customer_id"] = req.CustomerID
	}
	if req.StoreID != "" {
		metadata["store_id"] = req.StoreID
	}

	tracker, err := s.client.CreateTracker(ctx, safepay.TrackerRequest{
		Currency:  "PKR",
		Amount:    amountPaisa,
		EntryMode: "hosted",
		Metadata:  metadata,
	})
	if err != nil {
		return CheckoutResponse{}, fmt.Errorf("safepay tracker creation failed: %w", err)
	}

	checkoutURL := s.client.BuildCheckoutURL(tracker.Token)

	return CheckoutResponse{
		Gateway:     "safepay",
		SessionID:   tracker.Token,
		RedirectURL: checkoutURL,
	}, nil
}

// Refund initiates a refund through Safepay.
func (s *SafepayService) Refund(ctx context.Context, transactionID string, amount float64) error {
	if !s.IsConfigured() {
		return errors.New("safepay: gateway not configured")
	}

	amountPaisa := int64(math.Round(amount * 100))
	if amountPaisa <= 0 {
		return errors.New("safepay: invalid refund amount")
	}

	_, err := s.client.RefundPayment(ctx, transactionID, amountPaisa, "PKR", "OMNIGO Customer Refund")
	if err != nil {
		return fmt.Errorf("safepay refund failed: %w", err)
	}

	return nil
}

type SafepayGenericWebhook struct {
	Event string `json:"event"`
	Data  struct {
		Token    string            `json:"token"`
		State    string            `json:"state"`
		Amount   int64             `json:"amount"`
		Currency string            `json:"currency"`
		Metadata map[string]string `json:"metadata"`
	} `json:"data"`
}

// VerifyWebhook validates Safepay signature and parses transaction event.
func (s *SafepayService) VerifyWebhook(payload []byte, signature string) (WebhookEvent, error) {
	if !s.IsConfigured() {
		return WebhookEvent{}, errors.New("safepay: gateway not configured")
	}

	if signature != "" && !s.client.VerifyWebhookSignature(payload, signature) {
		return WebhookEvent{}, errors.New("safepay: invalid webhook signature")
	}

	var event SafepayGenericWebhook
	if err := json.Unmarshal(payload, &event); err != nil {
		return WebhookEvent{}, fmt.Errorf("safepay: failed to unmarshal webhook payload: %w", err)
	}

	orderID := event.Data.Metadata["order_id"]
	customerID := event.Data.Metadata["customer_id"]

	status := "PENDING"
	stateLower := strings.ToLower(event.Data.State)
	if stateLower == "paid" || stateLower == "completed" || stateLower == "captured" {
		status = "SUCCESS"
	} else if stateLower == "failed" || stateLower == "cancelled" {
		status = "FAILED"
	} else if stateLower == "refunded" {
		status = "REFUNDED"
	}

	return WebhookEvent{
		OrderID:       orderID,
		CustomerID:    customerID,
		TransactionID: event.Data.Token,
		Status:        status,
		Amount:        float64(event.Data.Amount) / 100.0,
		Currency:      event.Data.Currency,
		Gateway:       "safepay",
	}, nil
}
