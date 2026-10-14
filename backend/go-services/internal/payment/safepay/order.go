package safepay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// TrackerRequest contains parameters to create a Safepay payment tracker.
type TrackerRequest struct {
	MerchantAPIKey string            `json:"merchant_api_key,omitempty"`
	Intent         string            `json:"intent"`               // e.g. "CYBERSOURCE"
	Mode           string            `json:"mode"`                 // e.g. "payment"
	EntryMode      string            `json:"entry_mode,omitempty"` // "raw", "flex", or "hosted"
	Currency       string            `json:"currency"`             // "PKR"
	Amount         int64             `json:"amount"`               // Minor units (paisa)
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// TrackerData returned by Safepay tracker creation.
type TrackerData struct {
	Token       string   `json:"token"`
	State       string   `json:"state"` // e.g. "TRACKER_CREATED", "PAID", "FAILED"
	Amount      int64    `json:"amount"`
	Currency    string   `json:"currency"`
	Intent      string   `json:"intent"`
	Mode        string   `json:"mode"`
	NextActions []string `json:"next_actions,omitempty"`
}

// CardDetails represents raw card payment parameters for Cybersource direct charging.
type CardDetails struct {
	CardNumber      string `json:"card_number"`
	ExpirationMonth string `json:"expiration_month"`
	ExpirationYear  string `json:"expiration_year"`
	CVV             string `json:"cvv"`
	CardholderName  string `json:"cardholder_name,omitempty"`
}

// BillingDetails represents customer billing info required for 3DS Payer Auth Enrollment.
type BillingDetails struct {
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Email      string `json:"email"`
	Phone      string `json:"phone"`
	Address1   string `json:"address_1"`
	Locality   string `json:"locality"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"` // ISO-2, e.g. "PK"
}

// RedirectURLs for 3D Secure return handling.
type RedirectURLs struct {
	SuccessURL string `json:"success_url"`
	FailureURL string `json:"failure_url"`
}

// PaymentActionPayload represents the general action request structure for Safepay tracker actions.
type PaymentActionPayload struct {
	Action  string      `json:"action"`
	Payload interface{} `json:"payload"`
}

// EnrollmentResponse returned when setting up 3DS payer authentication.
type EnrollmentResponse struct {
	Token          string   `json:"token,omitempty"`
	State          string   `json:"state,omitempty"`
	AuthStatus     string   `json:"auth_status,omitempty"` // "REQUIRED", "FRICTIONLESS", "SUCCESS"
	StepUpURL      string   `json:"step_up_url,omitempty"` // 3DS challenge URL for iframe or redirect
	ACSURL         string   `json:"acs_url,omitempty"`
	PaReq          string   `json:"pareq,omitempty"`
	JWT            string   `json:"jwt,omitempty"`
	NextActions    []string `json:"next_actions,omitempty"`
}

// PaymentResult represents the finalized result after authorization/capture.
type PaymentResult struct {
	Token       string   `json:"token"`
	State       string   `json:"state"` // "CAPTURED", "AUTHORIZED", "FAILED"
	Amount      int64    `json:"amount"`
	Currency    string   `json:"currency"`
	NextActions []string `json:"next_actions,omitempty"`
}

// RefundRequest parameters for Safepay tracker refund.
type RefundRequest struct {
	Amount   int64  `json:"amount"` // In minor units (paisa)
	Currency string `json:"currency"`
	Reason   string `json:"reason,omitempty"`
}

// RefundResponse data returned by Safepay refund endpoint.
type RefundResponse struct {
	Token     string `json:"token"`
	State     string `json:"state"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	RefundID  string `json:"refund_id,omitempty"`
}

// CreateTracker initializes a new Safepay payment tracker (order session).
func (c *Client) CreateTracker(ctx context.Context, req TrackerRequest) (*TrackerData, error) {
	if !c.IsConfigured() {
		return nil, errors.New("safepay: client not configured with secret key")
	}

	if req.MerchantAPIKey == "" {
		req.MerchantAPIKey = c.cfg.APIKey
	}
	if req.Intent == "" {
		req.Intent = "CYBERSOURCE"
	}
	if req.Mode == "" {
		req.Mode = "payment"
	}
	if req.Currency == "" {
		req.Currency = "PKR"
	}
	if req.Amount <= 0 {
		return nil, errors.New("safepay: amount must be greater than zero")
	}

	payloadBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to marshal tracker request: %w", err)
	}

	url := fmt.Sprintf("%s/order/payments/v3/", c.cfg.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-SFPY-MERCHANT-SECRET", c.cfg.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("safepay: tracker request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to read tracker response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("safepay: create tracker failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Status struct {
			Message string   `json:"message"`
			Errors  []string `json:"errors"`
		} `json:"status"`
		Data TrackerData `json:"data"`
	}

	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("safepay: failed to unmarshal tracker response: %w", err)
	}

	if res.Data.Token == "" {
		return nil, fmt.Errorf("safepay: missing tracker token in response")
	}

	return &res.Data, nil
}

// GetTracker fetches the current state and details of a payment tracker from Safepay.
func (c *Client) GetTracker(ctx context.Context, trackerToken string) (*TrackerData, error) {
	if !c.IsConfigured() {
		return nil, errors.New("safepay: client not configured with secret key")
	}
	if trackerToken == "" {
		return nil, errors.New("safepay: trackerToken is required")
	}

	url := fmt.Sprintf("%s/order/payments/v3/%s", c.cfg.BaseURL, trackerToken)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to create get tracker request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-SFPY-MERCHANT-SECRET", c.cfg.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("safepay: get tracker request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to read tracker response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("safepay: get tracker failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Status struct {
			Message string   `json:"message"`
			Errors  []string `json:"errors"`
		} `json:"status"`
		Data TrackerData `json:"data"`
	}

	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("safepay: failed to unmarshal tracker response: %w", err)
	}

	return &res.Data, nil
}

// BuildCheckoutURL returns the hosted checkout URL with beacon query parameter for the given tracker.
func (c *Client) BuildCheckoutURL(trackerToken string) string {
	u, err := url.Parse(c.cfg.CheckoutURL)
	if err != nil {
		return fmt.Sprintf("%s?beacon=%s", c.cfg.CheckoutURL, trackerToken)
	}

	q := u.Query()
	q.Set("beacon", trackerToken)
	if c.cfg.Environment != "production" {
		q.Set("env", c.cfg.Environment)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// PayerAuthSetup attaches card details to the tracker and triggers setup.
func (c *Client) PayerAuthSetup(ctx context.Context, trackerToken string, card CardDetails) (*TrackerData, error) {
	if !c.IsConfigured() {
		return nil, errors.New("safepay: client not configured")
	}
	if trackerToken == "" {
		return nil, errors.New("safepay: trackerToken is required")
	}

	actionReq := PaymentActionPayload{
		Action: "PAYER_AUTHENTICATION_SETUP",
		Payload: map[string]interface{}{
			"payment_method": map[string]interface{}{
				"card": map[string]string{
					"card_number":      strings.ReplaceAll(card.CardNumber, " ", ""),
					"expiration_month": card.ExpirationMonth,
					"expiration_year":  card.ExpirationYear,
					"cvv":              card.CVV,
				},
			},
		},
	}

	return c.executeAction(ctx, trackerToken, actionReq)
}

// PayerAuthEnrollment sends billing details and 3DS return URLs.
func (c *Client) PayerAuthEnrollment(ctx context.Context, trackerToken string, billing BillingDetails, urls RedirectURLs) (*EnrollmentResponse, error) {
	if !c.IsConfigured() {
		return nil, errors.New("safepay: client not configured")
	}
	if trackerToken == "" {
		return nil, errors.New("safepay: trackerToken is required")
	}

	if billing.Country == "" {
		billing.Country = "PK"
	}

	actionReq := PaymentActionPayload{
		Action: "PAYER_AUTH_ENROLLMENT",
		Payload: map[string]interface{}{
			"billing":       billing,
			"redirect_urls": urls,
		},
	}

	bodyBytes, err := c.postActionRaw(ctx, trackerToken, actionReq)
	if err != nil {
		return nil, err
	}

	var res struct {
		Data EnrollmentResponse `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("safepay: failed to unmarshal enrollment response: %w", err)
	}

	return &res.Data, nil
}

// Authorize authorizes and optionally captures the payment.
func (c *Client) Authorize(ctx context.Context, trackerToken string, doCapture bool) (*PaymentResult, error) {
	if !c.IsConfigured() {
		return nil, errors.New("safepay: client not configured")
	}
	if trackerToken == "" {
		return nil, errors.New("safepay: trackerToken is required")
	}

	actionReq := PaymentActionPayload{
		Action: "AUTHORIZATION",
		Payload: map[string]interface{}{
			"do_capture": doCapture,
		},
	}

	bodyBytes, err := c.postActionRaw(ctx, trackerToken, actionReq)
	if err != nil {
		return nil, err
	}

	var res struct {
		Data PaymentResult `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("safepay: failed to unmarshal auth result: %w", err)
	}

	return &res.Data, nil
}

// Capture captures a previously authorized transaction.
func (c *Client) Capture(ctx context.Context, trackerToken string, amountPaisa int64, currency string) (*PaymentResult, error) {
	if !c.IsConfigured() {
		return nil, errors.New("safepay: client not configured")
	}
	if currency == "" {
		currency = "PKR"
	}

	actionReq := PaymentActionPayload{
		Action: "CAPTURE",
		Payload: map[string]interface{}{
			"amount":   amountPaisa,
			"currency": currency,
		},
	}

	bodyBytes, err := c.postActionRaw(ctx, trackerToken, actionReq)
	if err != nil {
		return nil, err
	}

	var res struct {
		Data PaymentResult `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("safepay: failed to unmarshal capture result: %w", err)
	}

	return &res.Data, nil
}

// RefundPayment issues a full or partial refund for a tracker.
func (c *Client) RefundPayment(ctx context.Context, trackerToken string, amountPaisa int64, currency, reason string) (*RefundResponse, error) {
	if !c.IsConfigured() {
		return nil, errors.New("safepay: client not configured")
	}
	if trackerToken == "" {
		return nil, errors.New("safepay: trackerToken is required")
	}
	if currency == "" {
		currency = "PKR"
	}

	reqPayload := RefundRequest{
		Amount:   amountPaisa,
		Currency: currency,
		Reason:   reason,
	}

	payloadBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to marshal refund payload: %w", err)
	}

	url := fmt.Sprintf("%s/order/payments/v3/%s/refund", c.cfg.BaseURL, trackerToken)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to create refund request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-SFPY-MERCHANT-SECRET", c.cfg.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("safepay: refund request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to read refund response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("safepay: refund failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Data RefundResponse `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("safepay: failed to unmarshal refund response: %w", err)
	}

	return &res.Data, nil
}

// ReversePayment cancels an authorized transaction before capture.
func (c *Client) ReversePayment(ctx context.Context, trackerToken string, authID string) error {
	if !c.IsConfigured() {
		return errors.New("safepay: client not configured")
	}

	payloadBytes, err := json.Marshal(map[string]string{
		"authorization": authID,
	})
	if err != nil {
		return fmt.Errorf("safepay: failed to marshal reversal payload: %w", err)
	}

	url := fmt.Sprintf("%s/order/payments/v3/%s/reversal", c.cfg.BaseURL, trackerToken)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("safepay: failed to create reversal request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-SFPY-MERCHANT-SECRET", c.cfg.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("safepay: reversal request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("safepay: failed to read reversal response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("safepay: reversal failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// executeAction is a private helper to dispatch an action to a payment tracker.
func (c *Client) executeAction(ctx context.Context, trackerToken string, actionReq PaymentActionPayload) (*TrackerData, error) {
	bodyBytes, err := c.postActionRaw(ctx, trackerToken, actionReq)
	if err != nil {
		return nil, err
	}

	var res struct {
		Data TrackerData `json:"data"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("safepay: failed to unmarshal action response: %w", err)
	}

	return &res.Data, nil
}

// postActionRaw dispatches raw JSON to /order/payments/v3/{track} and returns raw body.
func (c *Client) postActionRaw(ctx context.Context, trackerToken string, payload interface{}) ([]byte, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to marshal action payload: %w", err)
	}

	url := fmt.Sprintf("%s/order/payments/v3/%s", c.cfg.BaseURL, trackerToken)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to create http action request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-SFPY-MERCHANT-SECRET", c.cfg.SecretKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("safepay: action request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to read action response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("safepay: payment action failed (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return bodyBytes, nil
}
