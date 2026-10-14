package safepay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSafepay_CreateTrackerAndCheckoutURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-SFPY-MERCHANT-SECRET") != "test_secret_123" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if r.URL.Path == "/order/payments/v3/" && r.Method == http.MethodPost {
			var body TrackerRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if body.Amount != 50000 || body.Currency != "PKR" {
				http.Error(w, "bad amount or currency", http.StatusBadRequest)
				return
			}

			resp := map[string]interface{}{
				"status": map[string]interface{}{"message": "success"},
				"data": TrackerData{
					Token:       "track_test_123456",
					State:       "TRACKER_CREATED",
					Amount:      body.Amount,
					Currency:    body.Currency,
					Intent:      body.Intent,
					Mode:        body.Mode,
					NextActions: []string{"PAYER_AUTHENTICATION_SETUP"},
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewClient(Config{
		APIKey:      "sec_test_api_key",
		SecretKey:   "test_secret_123",
		BaseURL:     server.URL,
		CheckoutURL: "https://sandbox.getsafepay.com/checkout/pay",
		Environment: "sandbox",
	})

	ctx := context.Background()
	tracker, err := client.CreateTracker(ctx, TrackerRequest{
		Amount:   50000,
		Currency: "PKR",
		Metadata: map[string]string{"order_id": "ORD-123"},
	})
	if err != nil {
		t.Fatalf("CreateTracker failed: %v", err)
	}

	if tracker.Token != "track_test_123456" {
		t.Errorf("expected tracker token 'track_test_123456', got %s", tracker.Token)
	}

	checkoutURL := client.BuildCheckoutURL(tracker.Token)
	expectedURL := "https://sandbox.getsafepay.com/checkout/pay?beacon=track_test_123456&env=sandbox"
	if checkoutURL != expectedURL {
		t.Errorf("expected checkout URL %s, got %s", expectedURL, checkoutURL)
	}
}

func TestSafepay_CybersourceCardActions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-SFPY-MERCHANT-SECRET") != "sec_key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/order/payments/v3/track_999":
			var action PaymentActionPayload
			json.NewDecoder(r.Body).Decode(&action)

			if action.Action == "PAYER_AUTHENTICATION_SETUP" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": TrackerData{
						Token:       "track_999",
						State:       "PENDING",
						NextActions: []string{"PAYER_AUTH_ENROLLMENT"},
					},
				})
				return
			} else if action.Action == "PAYER_AUTH_ENROLLMENT" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": EnrollmentResponse{
						Token:      "track_999",
						AuthStatus: "FRICTIONLESS",
						StepUpURL:  "",
					},
				})
				return
			} else if action.Action == "AUTHORIZATION" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": PaymentResult{
						Token:    "track_999",
						State:    "CAPTURED",
						Amount:   250000,
						Currency: "PKR",
					},
				})
				return
			}
		case "/order/payments/v3/track_999/refund":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": RefundResponse{
					Token:    "track_999",
					State:    "REFUNDED",
					Amount:   250000,
					Currency: "PKR",
					RefundID: "ref_111",
				},
			})
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewClient(Config{
		SecretKey: "sec_key",
		BaseURL:   server.URL,
	})
	ctx := context.Background()

	// 1. Setup
	setupRes, err := client.PayerAuthSetup(ctx, "track_999", CardDetails{
		CardNumber:      "4111 1111 1111 1111",
		ExpirationMonth: "12",
		ExpirationYear:  "2028",
		CVV:             "123",
	})
	if err != nil {
		t.Fatalf("PayerAuthSetup error: %v", err)
	}
	if setupRes.Token != "track_999" {
		t.Errorf("unexpected setup token: %s", setupRes.Token)
	}

	// 2. Enrollment
	enrollRes, err := client.PayerAuthEnrollment(ctx, "track_999", BillingDetails{
		FirstName: "Ali",
		LastName:  "Khan",
		Email:     "ali@example.com",
	}, RedirectURLs{
		SuccessURL: "https://omnigo.pk/success",
		FailureURL: "https://omnigo.pk/failure",
	})
	if err != nil {
		t.Fatalf("PayerAuthEnrollment error: %v", err)
	}
	if enrollRes.AuthStatus != "FRICTIONLESS" {
		t.Errorf("unexpected auth status: %s", enrollRes.AuthStatus)
	}

	// 3. Authorize
	authRes, err := client.Authorize(ctx, "track_999", true)
	if err != nil {
		t.Fatalf("Authorize error: %v", err)
	}
	if authRes.State != "CAPTURED" {
		t.Errorf("unexpected auth state: %s", authRes.State)
	}

	// 4. Refund
	refundRes, err := client.RefundPayment(ctx, "track_999", 250000, "PKR", "Order cancelled")
	if err != nil {
		t.Fatalf("RefundPayment error: %v", err)
	}
	if refundRes.State != "REFUNDED" {
		t.Errorf("unexpected refund state: %s", refundRes.State)
	}
}

func TestSafepay_QuickLinksAndReporter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/invoice/quick-links/v1/":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": QuickLinkResponse{
					Token:            "link_abc",
					Amount:           150000,
					Currency:         "PKR",
					RecipientViewURL: "https://sandbox.getsafepay.com/checkout/pay?beacon=link_abc",
					Note:             "Test invoice",
				},
			})
			return
		case "/reporter/api/v2/payments/track_reported":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": PaymentDetails{
					Token:    "track_reported",
					State:    "PAID",
					Amount:   150000,
					Currency: "PKR",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewClient(Config{
		SecretKey: "sec_key",
		BaseURL:   server.URL,
	})
	ctx := context.Background()

	// Test QuickLink
	ql, err := client.CreateQuickLink(ctx, QuickLinkRequest{
		Amount:   150000,
		Currency: "PKR",
		Note:     "Test invoice",
	})
	if err != nil {
		t.Fatalf("CreateQuickLink failed: %v", err)
	}
	if ql.RecipientViewURL == "" {
		t.Errorf("missing recipient view url")
	}

	// Test Reporter
	details, err := client.GetPaymentDetails(ctx, "track_reported")
	if err != nil {
		t.Fatalf("GetPaymentDetails failed: %v", err)
	}
	if details.State != "PAID" {
		t.Errorf("expected state PAID, got %s", details.State)
	}
}
