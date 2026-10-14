package safepay

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds credentials and parameters for Safepay gateway and Raastwire payouts.
type Config struct {
	APIKey         string        // Merchant API Key / Public Key (sec_...)
	SecretKey      string        // Merchant Secret Key (used in X-SFPY-MERCHANT-SECRET)
	AggregatorID   string        // Used for Raastwire payouts
	WebhookSecret  string        // Used for webhook HMAC verification
	BaseURL        string        // API Base URL (e.g., https://sandbox.api.getsafepay.com)
	CheckoutURL    string        // Hosted Checkout UI (e.g., https://sandbox.getsafepay.com/checkout/pay)
	Environment    string        // "sandbox" or "production"
	PayoutFeePaisa int64         // Default 2000 paisa (Rs. 20)
	Timeout        time.Duration // HTTP request timeout
}

// Client interacts with Safepay APIs for checkout trackers, 3DS cards, payouts, and webhooks.
type Client struct {
	cfg        Config
	httpClient *http.Client
}

// NewClient constructs a Safepay client with the provided configuration.
// It never panics: if credentials are missing, IsConfigured() returns false.
func NewClient(cfg Config) *Client {
	isProd := os.Getenv("APP_ENV") == "production" || cfg.Environment == "production"

	if cfg.BaseURL == "" {
		if isProd {
			cfg.BaseURL = "https://api.getsafepay.com"
		} else {
			cfg.BaseURL = "https://sandbox.api.getsafepay.com"
		}
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	if cfg.CheckoutURL == "" {
		if isProd {
			cfg.CheckoutURL = "https://getsafepay.com/checkout/pay"
		} else {
			cfg.CheckoutURL = "https://sandbox.getsafepay.com/checkout/pay"
		}
	}
	cfg.CheckoutURL = strings.TrimRight(cfg.CheckoutURL, "/")

	if cfg.Environment == "" {
		if isProd {
			cfg.Environment = "production"
		} else {
			cfg.Environment = "sandbox"
		}
	}

	if cfg.PayoutFeePaisa <= 0 {
		cfg.PayoutFeePaisa = 2000 // 2000 paisa = Rs. 20 default
	}

	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// NewClientFromEnv constructs a Safepay client initialized from environment variables.
func NewClientFromEnv() *Client {
	apiKey := os.Getenv("SAFEPAY_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("SAFEPAY_PUBLIC_KEY")
	}

	secretKey := os.Getenv("SAFEPAY_SECRET_KEY")
	if secretKey == "" {
		secretKey = os.Getenv("SAFEPAY_MERCHANT_SECRET")
	}

	aggregatorID := os.Getenv("SAFEPAY_AGGREGATOR_ID")
	if aggregatorID == "" {
		aggregatorID = os.Getenv("SAFEPAY_MERCHANT_ID")
	}
	if aggregatorID == "" {
		aggregatorID = apiKey
	}

	webhookSecret := os.Getenv("SAFEPAY_WEBHOOK_SECRET")
	if webhookSecret == "" {
		webhookSecret = secretKey
	}

	baseURL := os.Getenv("SAFEPAY_BASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("SAFEPAY_API_URL")
	}

	checkoutURL := os.Getenv("SAFEPAY_CHECKOUT_URL")

	env := os.Getenv("SAFEPAY_ENV")
	if env == "" {
		if os.Getenv("APP_ENV") == "production" {
			env = "production"
		} else {
			env = "sandbox"
		}
	}

	feePaisa := int64(2000) // Default 2000 paisa (Rs. 20)
	if feeStr := os.Getenv("SAFEPAY_PAYOUT_FEE_PAISA"); feeStr != "" {
		if parsed, err := strconv.ParseInt(feeStr, 10, 64); err == nil && parsed >= 0 {
			feePaisa = parsed
		}
	}

	timeoutSec := 30
	if toStr := os.Getenv("SAFEPAY_TIMEOUT_SECONDS"); toStr != "" {
		if parsed, err := strconv.Atoi(toStr); err == nil && parsed > 0 {
			timeoutSec = parsed
		}
	}

	client := NewClient(Config{
		APIKey:         apiKey,
		SecretKey:      secretKey,
		AggregatorID:   aggregatorID,
		WebhookSecret:  webhookSecret,
		BaseURL:        baseURL,
		CheckoutURL:    checkoutURL,
		Environment:    env,
		PayoutFeePaisa: feePaisa,
		Timeout:        time.Duration(timeoutSec) * time.Second,
	})

	if !client.IsConfigured() {
		log.Println("ℹ [safepay] Safepay credentials not set; operations will return unconfigured error.")
	} else {
		log.Printf("✓ [safepay] Safepay client initialized (BaseURL: %s, Env: %s, Fee: %d paisa)", client.cfg.BaseURL, client.cfg.Environment, client.cfg.PayoutFeePaisa)
	}

	return client
}

// IsConfigured returns true if SecretKey is set.
func (c *Client) IsConfigured() bool {
	return c.cfg.SecretKey != ""
}

// APIKey returns the configured API Key.
func (c *Client) APIKey() string {
	return c.cfg.APIKey
}

// SecretKey returns the configured Secret Key.
func (c *Client) SecretKey() string {
	return c.cfg.SecretKey
}

// AggregatorID returns the configured Aggregator ID.
func (c *Client) AggregatorID() string {
	return c.cfg.AggregatorID
}

// BaseURL returns the configured Base URL.
func (c *Client) BaseURL() string {
	return c.cfg.BaseURL
}

// CheckoutURL returns the hosted checkout URL base.
func (c *Client) CheckoutURL() string {
	return c.cfg.CheckoutURL
}

// Environment returns the configured environment (sandbox or production).
func (c *Client) Environment() string {
	return c.cfg.Environment
}

// PayoutFeePaisa returns the configured fee per payout in paisa.
func (c *Client) PayoutFeePaisa() int64 {
	return c.cfg.PayoutFeePaisa
}
