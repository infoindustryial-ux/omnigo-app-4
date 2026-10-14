package safepay

import (
	"testing"
)

func TestValidatePakistaniIBAN(t *testing.T) {
	// A valid Pakistani IBAN structure test
	// e.g. PK36SCBL0000001123456702
	// Let's test basic structure checks:
	tests := []struct {
		name    string
		iban    string
		wantErr bool
	}{
		{
			name:    "Invalid length short",
			iban:    "PK12345",
			wantErr: true,
		},
		{
			name:    "Invalid country code",
			iban:    "GB82WEST12345698765432",
			wantErr: true,
		},
		{
			name:    "Invalid characters",
			iban:    "PK36SCBL00000011234567-2",
			wantErr: true,
		},
		{
			name:    "Valid SCB PK IBAN format",
			iban:    "PK36SCBL0000001123456702",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePakistaniIBAN(tt.iban)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePakistaniIBAN(%q) error = %v, wantErr %v", tt.iban, err, tt.wantErr)
			}
		})
	}
}
