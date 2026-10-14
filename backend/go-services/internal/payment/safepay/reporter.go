package safepay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PaymentDetails contains reporting data for a Safepay transaction tracker.
type PaymentDetails struct {
	Token       string            `json:"token"`
	State       string            `json:"state"` // "PAID", "TRACKER_CREATED", "CANCELLED", "REFUNDED", "FAILED"
	Currency    string            `json:"currency"`
	Amount      int64             `json:"amount"` // In minor units (paisa)
	Fee         int64             `json:"fee,omitempty"`
	Net         int64             `json:"net,omitempty"`
	Intent      string            `json:"intent,omitempty"`
	Mode        string            `json:"mode,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Customer    struct {
		Email string `json:"email,omitempty"`
		Phone string `json:"phone,omitempty"`
	} `json:"customer,omitempty"`
	CreatedAt   *time.Time        `json:"created_at,omitempty"`
	PaidAt      *time.Time        `json:"paid_at,omitempty"`
}

// GetPaymentDetails fetches real-time payment status and metadata from Safepay Reporter API.
func (c *Client) GetPaymentDetails(ctx context.Context, trackerToken string) (*PaymentDetails, error) {
	if !c.IsConfigured() {
		return nil, errors.New("safepay: client not configured with secret key")
	}
	if trackerToken == "" {
		return nil, errors.New("safepay: trackerToken is required")
	}

	url := fmt.Sprintf("%s/reporter/api/v2/payments/%s", c.cfg.BaseURL, trackerToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to create reporter request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SFPY-MERCHANT-SECRET", c.cfg.SecretKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("safepay: reporter request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to read reporter response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("safepay: reporter query failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Status struct {
			Message string   `json:"message"`
			Errors  []string `json:"errors"`
		} `json:"status"`
		Data PaymentDetails `json:"data"`
	}

	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("safepay: failed to unmarshal reporter response: %w", err)
	}

	return &res.Data, nil
}
