package handlers

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/omnigo/backend/internal/payment_orchestrator/service"
	"github.com/omnigo/backend/internal/shared/middleware"
)

// SafepayHandler handles client requests for Safepay hosted checkout, card charges, 3DS returns, and quick links.
type SafepayHandler struct {
	safepaySvc *service.SafepayService
}

// NewSafepayHandler constructs a new SafepayHandler.
func NewSafepayHandler(svc *service.SafepayService) *SafepayHandler {
	return &SafepayHandler{
		safepaySvc: svc,
	}
}

// RegisterRoutes mounts Safepay API routes onto Gin router.
func (h *SafepayHandler) RegisterRoutes(router gin.IRoutes) {
	router.POST("/api/v1/payments/safepay/checkout", middleware.JWTAuth(), h.InitiateCheckout)
	router.POST("/api/v1/payments/safepay/charge", middleware.JWTAuth(), h.ChargeCard)
	router.GET("/api/v1/payments/safepay/3ds/callback", h.Handle3DSCallback)
	router.GET("/api/v1/payments/safepay/status/:tracker", middleware.JWTAuth(), h.GetPaymentStatus)
	router.POST("/api/v1/payments/safepay/quick-link", middleware.JWTAuth(), h.CreateQuickLink)
	log.Println("[safepay] Routes registered: /api/v1/payments/safepay/{checkout,charge,3ds/callback,status/:tracker,quick-link}")
}

// InitiateCheckout handles POST /api/v1/payments/safepay/checkout
func (h *SafepayHandler) InitiateCheckout(c *gin.Context) {
	var req service.SafepayCheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload: " + err.Error()})
		return
	}

	customerID := middleware.GetTrackingID(c)
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: missing user tracking ID"})
		return
	}

	clientIP := c.ClientIP()
	resp, err := h.safepaySvc.InitiateHostedCheckout(c.Request.Context(), customerID, clientIP, req)
	if err != nil {
		classifyError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ChargeCard handles POST /api/v1/payments/safepay/charge
func (h *SafepayHandler) ChargeCard(c *gin.Context) {
	var req service.SafepayCardPaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid card payload: " + err.Error()})
		return
	}

	customerID := middleware.GetTrackingID(c)
	if customerID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: missing user tracking ID"})
		return
	}

	clientIP := c.ClientIP()
	resp, err := h.safepaySvc.ProcessCardPayment(c.Request.Context(), customerID, clientIP, req)
	if err != nil {
		classifyError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// Handle3DSCallback handles GET /api/v1/payments/safepay/3ds/callback
func (h *SafepayHandler) Handle3DSCallback(c *gin.Context) {
	tracker := c.Query("tracker")
	orderID := c.Query("order_id")
	failed := c.Query("failed")

	if failed == "true" {
		failHTML := `<!DOCTYPE html><html><body><h3>Payment Failed</h3><p>Your 3D Secure verification could not be completed.</p></body></html>`
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(failHTML))
		return
	}

	if tracker == "" || orderID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing tracker or order_id"})
		return
	}

	resp, err := h.safepaySvc.Handle3DSCallback(c.Request.Context(), tracker, orderID)
	if err != nil {
		log.Printf("ERROR [Safepay 3DS]: %v", err)
		c.String(http.StatusInternalServerError, "Verification failed: "+err.Error())
		return
	}

	// Return clean HTML bridge indicating success to WebView
	successHTML := `<!DOCTYPE html>
<html>
<head>
  <title>Payment Successful</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    body { font-family: sans-serif; display: flex; align-items: center; justify-content: center; height: 90vh; text-align: center; }
    .card { padding: 2rem; border-radius: 8px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); }
  </style>
</head>
<body>
  <div class="card">
    <h2 style="color: #16a34a;">Payment Confirmed!</h2>
    <p>Transaction settled successfully. You may close this window.</p>
  </div>
  <script>
    if (window.FlutterChannel) { window.FlutterChannel.postMessage('safepay:success'); }
    if (window.opener) { window.opener.postMessage('safepay:success', '*'); }
  </script>
</body>
</html>`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(successHTML))
	_ = resp
}

// GetPaymentStatus handles GET /api/v1/payments/safepay/status/:tracker
func (h *SafepayHandler) GetPaymentStatus(c *gin.Context) {
	tracker := c.Param("tracker")
	if tracker == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tracker parameter required"})
		return
	}

	details, err := h.safepaySvc.GetPaymentStatus(c.Request.Context(), tracker)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, details)
}

// CreateQuickLink handles POST /api/v1/payments/safepay/quick-link
func (h *SafepayHandler) CreateQuickLink(c *gin.Context) {
	var body struct {
		OrderID        string `json:"order_id" binding:"required"`
		Note           string `json:"note"`
		RecipientEmail string `json:"recipient_email"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	merchantUserID := middleware.GetTrackingID(c)
	resp, err := h.safepaySvc.CreateQuickLink(c.Request.Context(), merchantUserID, body.OrderID, body.Note, body.RecipientEmail)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func classifyError(c *gin.Context, err error) {
	errMsg := err.Error()
	switch {
	case errors.Is(err, service.ErrNotFound) || strings.Contains(errMsg, "not found"):
		c.JSON(http.StatusNotFound, gin.H{"error": errMsg})
	case errors.Is(err, service.ErrForbidden) || strings.Contains(errMsg, "belongs to a different user"):
		c.JSON(http.StatusForbidden, gin.H{"error": errMsg})
	case errors.Is(err, service.ErrConflict) || strings.Contains(errMsg, "already") || strings.Contains(errMsg, "conflict"):
		c.JSON(http.StatusConflict, gin.H{"error": errMsg})
	case errors.Is(err, service.ErrValidation) || strings.Contains(errMsg, "invalid") || strings.Contains(errMsg, "expired"):
		c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": errMsg})
	}
}
