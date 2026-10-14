package service

import (
	"testing"
)

func TestSafepayCardPaymentRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     SafepayCardPaymentRequest
		wantErr bool
	}{
		{
			name: "Valid card details",
			req: SafepayCardPaymentRequest{
				CardNumber:  "4111 1111 1111 1111",
				ExpiryMonth: "12",
				ExpiryYear:  "2030",
				CVV:         "123",
			},
			wantErr: false,
		},
		{
			name: "Invalid card PAN length",
			req: SafepayCardPaymentRequest{
				CardNumber:  "1234",
				ExpiryMonth: "12",
				ExpiryYear:  "2030",
				CVV:         "123",
			},
			wantErr: true,
		},
		{
			name: "Invalid CVV",
			req: SafepayCardPaymentRequest{
				CardNumber:  "4111 1111 1111 1111",
				ExpiryMonth: "12",
				ExpiryYear:  "2030",
				CVV:         "1",
			},
			wantErr: true,
		},
		{
			name: "Invalid Expiry Month",
			req: SafepayCardPaymentRequest{
				CardNumber:  "4111 1111 1111 1111",
				ExpiryMonth: "15",
				ExpiryYear:  "2030",
				CVV:         "123",
			},
			wantErr: true,
		},
		{
			name: "Expired Card",
			req: SafepayCardPaymentRequest{
				CardNumber:  "4111 1111 1111 1111",
				ExpiryMonth: "01",
				ExpiryYear:  "2020",
				CVV:         "123",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
