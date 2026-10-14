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

// GuestUserRequest parameters for creating a guest user session.
type GuestUserRequest struct {
	Client    string `json:"client"` // API Key
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Country   string `json:"country"` // ISO-2 country code, e.g. "PK"
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// PassportTokenResponse data from Safepay passport endpoint.
type PassportTokenResponse struct {
	Token string `json:"token"`
}

// GuestUserResponse data from Safepay guest user creation endpoint.
type GuestUserResponse struct {
	Token  string `json:"token"`
	UserID string `json:"user_id,omitempty"`
}

// CreatePassportToken requests a time-based client authorization token (valid 2 mins).
func (c *Client) CreatePassportToken(ctx context.Context) (string, error) {
	if !c.IsConfigured() {
		return "", errors.New("safepay: client not configured with secret key")
	}

	url := fmt.Sprintf("%s/client/passport/v1/token", c.cfg.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return "", fmt.Errorf("safepay: failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SFPY-MERCHANT-SECRET", c.cfg.SecretKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("safepay: passport token request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("safepay: failed to read passport response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("safepay: passport request error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Status struct {
			Message string   `json:"message"`
			Errors  []string `json:"errors"`
		} `json:"status"`
		Data PassportTokenResponse `json:"data"`
	}

	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return "", fmt.Errorf("safepay: failed to unmarshal passport response: %w", err)
	}

	if res.Data.Token == "" {
		return "", fmt.Errorf("safepay: empty passport token received in response")
	}

	return res.Data.Token, nil
}

// CreateGuestToken registers a guest user and returns a 7-day session token.
func (c *Client) CreateGuestToken(ctx context.Context, guestReq GuestUserRequest) (*GuestUserResponse, error) {
	if !c.IsConfigured() {
		return nil, errors.New("safepay: client not configured with secret key")
	}

	if guestReq.Client == "" {
		guestReq.Client = c.cfg.APIKey
	}
	if guestReq.Country == "" {
		guestReq.Country = "PK"
	}

	payloadBytes, err := json.Marshal(guestReq)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to marshal guest user request: %w", err)
	}

	url := fmt.Sprintf("%s/user/v1/guest/", c.cfg.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to create guest request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SFPY-MERCHANT-SECRET", c.cfg.SecretKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("safepay: guest request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("safepay: failed to read guest response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("safepay: guest request error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Status struct {
			Message string   `json:"message"`
			Errors  []string `json:"errors"`
		} `json:"status"`
		Data GuestUserResponse `json:"data"`
	}

	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("safepay: failed to unmarshal guest response: %w", err)
	}

	return &res.Data, nil
}
