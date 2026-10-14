package safepay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// QuickLinkRequest contains parameters to create a shareable Safepay quick payment link.
type QuickLinkRequest struct {
	Amount         int64             `json:"amount"` // In minor units (paisa)
	Currency       string            `json:"currency"`
	Note           string            `json:"note,omitempty"`
	Workflow       string            `json:"workflow,omitempty"`        // "MANUAL" or "EMAIL"
	RecipientEmail string            `json:"recipient_email,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// QuickLinkResponse returned by Safepay Quick Links API.
type QuickLinkResponse struct {
	Token            string `json:"token"`
	Amount           int64  `json:"amount"`
	Currency         string `json:"currency"`
	RecipientViewURL string `json:"recipient_view_url"`
	Note             string `json:"note,omitempty"`
}

// CreateQuickLink creates a hosted payment link that can be sent via WhatsApp, SMS, or Email.
func (c *Client) CreateQuickLink(ctx context.Context, req QuickLinkRequest) (*QuickLinkResponse, error) {
	if !c.IsConfigured() {
		return nil, errors.New("safepay: client not configured with secret key")
	}

	if req.Amount <= 0 {
		return nil, errors.New("safepay: amount must be greater than zero")
	}
	if req.Currency == "" {
		req.Currency = "PKR"
	}
	if req.Workflow == "" {
		req.Workflow = "MANUAL"
	}

	payloadBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to marshal quicklink payload: %w", err)
	}

	url := fmt.Sprintf("%s/invoice/quick-links/v1/", c.cfg.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to create quicklink request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-SFPY-MERCHANT-SECRET", c.cfg.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("safepay: quicklink request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to read quicklink response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("safepay: quicklink creation failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Status struct {
			Message string   `json:"message"`
			Errors  []string `json:"errors"`
		} `json:"status"`
		Data QuickLinkResponse `json:"data"`
	}

	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("safepay: failed to unmarshal quicklink response: %w", err)
	}

	return &res.Data, nil
}
