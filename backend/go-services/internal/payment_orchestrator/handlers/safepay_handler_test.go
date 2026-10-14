package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSafepayHandler_3DSCallback_Failed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handler := NewSafepayHandler(nil)
	r.GET("/api/v1/payments/safepay/3ds/callback", handler.Handle3DSCallback)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/payments/safepay/3ds/callback?failed=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Payment Failed") {
		t.Errorf("expected body to contain 'Payment Failed', got: %s", body)
	}
}

func TestSafepayHandler_3DSCallback_MissingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handler := NewSafepayHandler(nil)
	r.GET("/api/v1/payments/safepay/3ds/callback", handler.Handle3DSCallback)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/payments/safepay/3ds/callback", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", w.Code)
	}
}
