package safepay

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"unicode"
)

// PayoutRequest holds the payload required by Safepay Raastwire Payout API.
type PayoutRequest struct {
	RequestID       string `json:"request_id"`
	Amount          string `json:"amount"` // String representation of PKR amount, e.g. "1500.00"
	CreditorIBAN    string `json:"creditor_iban"`
	BeneficiaryName string `json:"beneficiary_name,omitempty"`
	Narration       string `json:"narration,omitempty"`
}

// PayoutResponse represents the outcome of a Safepay Raastwire disbursement.
type PayoutResponse struct {
	Success        bool   `json:"success"`
	Status         string `json:"status"` // "pending", "processing", "completed", "failed"
	RequestID      string `json:"request_id"`
	TraceReference string `json:"trace_reference"`
	Message        string `json:"message"`
	ErrorCode      string `json:"error_code,omitempty"`
	RawResponse    string `json:"raw_response,omitempty"`
}

// ValidatePakistaniIBAN performs rigorous validation against State Bank of Pakistan standards:
// - Exactly 24 alphanumeric characters.
// - Starts with "PK".
// - Followed by 2 check digits, 4-character bank identifier, and 16-character account number.
// - Full ISO 7064 Modulo 97-10 check (must equal 1).
func ValidatePakistaniIBAN(rawIBAN string) error {
	clean := strings.ToUpper(strings.ReplaceAll(rawIBAN, " ", ""))
	clean = strings.TrimSpace(clean)

	if len(clean) != 24 {
		return fmt.Errorf("invalid IBAN length: expected 24 characters, got %d", len(clean))
	}

	if !strings.HasPrefix(clean, "PK") {
		return fmt.Errorf("invalid IBAN country code: must start with 'PK'")
	}

	for _, ch := range clean {
		if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) {
			return fmt.Errorf("invalid character in IBAN: '%c'", ch)
		}
	}

	// Move first 4 characters (PK + 2 check digits) to the end
	rearranged := clean[4:] + clean[:4]

	// Convert letters to digits: A=10, B=11, ..., Z=35
	var numStr strings.Builder
	for _, ch := range rearranged {
		if unicode.IsDigit(ch) {
			numStr.WriteRune(ch)
		} else if unicode.IsLetter(ch) {
			numStr.WriteString(fmt.Sprintf("%d", ch-'A'+10))
		}
	}

	// ISO 7064 Modulo 97 check
	n := new(big.Int)
	n, ok := n.SetString(numStr.String(), 10)
	if !ok {
		return fmt.Errorf("failed to parse IBAN numeric expansion")
	}

	mod97 := new(big.Int).Mod(n, big.NewInt(97))
	if mod97.Int64() != 1 {
		return fmt.Errorf("IBAN checksum validation failed (MOD-97 check failed)")
	}

	return nil
}

// SanitizeIBAN cleans and formats an IBAN to standard uppercase string.
func SanitizeIBAN(rawIBAN string) string {
	clean := strings.ToUpper(strings.ReplaceAll(rawIBAN, " ", ""))
	return strings.TrimSpace(clean)
}

// InitiatePayout dispatches a real-time bank transfer via Safepay Raastwire API.
// Endpoint: POST {BaseURL}/raastwire/v1/aggregators/{AggregatorID}/payout
func (c *Client) InitiatePayout(ctx context.Context, req PayoutRequest) (*PayoutResponse, error) {
	if !c.IsConfigured() {
		return nil, fmt.Errorf("safepay client is not configured (missing SAFEPAY_AGGREGATOR_ID or SAFEPAY_SECRET_KEY)")
	}

	sanitizedIBAN := SanitizeIBAN(req.CreditorIBAN)
	if err := ValidatePakistaniIBAN(sanitizedIBAN); err != nil {
		return nil, fmt.Errorf("invalid creditor IBAN: %w", err)
	}
	req.CreditorIBAN = sanitizedIBAN

	reqURL := fmt.Sprintf("%s/raastwire/v1/aggregators/%s/payout", c.cfg.BaseURL, c.cfg.AggregatorID)

	payloadBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payout request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("X-SFPY-AGGREGATOR-SECRET-KEY", c.cfg.SecretKey)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("safepay api network error: %w", err)
	}
	defer httpResp.Body.Close()

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read safepay response: %w", err)
	}

	var parsedResp struct {
		Status         any    `json:"status"`
		Data           struct {
			TraceReference string `json:"trace_reference"`
			RequestID      string `json:"request_id"`
			Status         string `json:"status"`
			TransactionID  string `json:"transaction_id"`
			Message        string `json:"message"`
		} `json:"data"`
		Message   string `json:"message"`
		Error     any    `json:"error"`
		Code      string `json:"code"`
	}

	_ = json.Unmarshal(bodyBytes, &parsedResp)

	resp := &PayoutResponse{
		RequestID:   req.RequestID,
		RawResponse: string(bodyBytes),
	}

	if parsedResp.Data.TraceReference != "" {
		resp.TraceReference = parsedResp.Data.TraceReference
	}
	if parsedResp.Data.Status != "" {
		resp.Status = strings.ToLower(parsedResp.Data.Status)
	}

	if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
		resp.Success = true
		if resp.Status == "" {
			resp.Status = "processing"
		}
		if parsedResp.Message != "" {
			resp.Message = parsedResp.Message
		} else if parsedResp.Data.Message != "" {
			resp.Message = parsedResp.Data.Message
		} else {
			resp.Message = "Payout request accepted by Safepay Raastwire"
		}
		return resp, nil
	}

	// Error response
	resp.Success = false
	resp.Status = "failed"
	if parsedResp.Message != "" {
		resp.Message = parsedResp.Message
	} else if parsedResp.Code != "" {
		resp.ErrorCode = parsedResp.Code
		resp.Message = fmt.Sprintf("Safepay error code: %s", parsedResp.Code)
	} else {
		resp.Message = fmt.Sprintf("Safepay HTTP %d: %s", httpResp.StatusCode, string(bodyBytes))
	}

	return resp, fmt.Errorf("safepay payout rejected: %s", resp.Message)
}

// VerifyWebhookSignature verifies the HMAC SHA-256 signature on Safepay payout webhooks.
func (c *Client) VerifyWebhookSignature(payload []byte, signatureHeader string) bool {
	if c.cfg.WebhookSecret == "" || signatureHeader == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(c.cfg.WebhookSecret))
	mac.Write(payload)
	expectedMAC := mac.Sum(nil)
	expectedHex := hex.EncodeToString(expectedMAC)

	return hmac.Equal([]byte(strings.ToLower(signatureHeader)), []byte(strings.ToLower(expectedHex)))
}
