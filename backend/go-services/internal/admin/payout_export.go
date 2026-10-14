package admin

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PayoutExportService generates 1LINK / 1IBFT standard corporate banking disbursement CSVs.
type PayoutExportService struct {
	db *pgxpool.Pool
}

func NewPayoutExportService(db *pgxpool.Pool) *PayoutExportService {
	return &PayoutExportService{db: db}
}

// Export1IBFTCSVPending generates a CSV file of pending vendor and rider payouts ready for corporate banking upload.
// payoutType can optionally be "vendor", "rider", or "all" (default: "all").
func (s *PayoutExportService) Export1IBFTCSVPending(ctx context.Context, batchID string, payoutType ...string) ([]byte, string, error) {
	target := "all"
	if len(payoutType) > 0 && payoutType[0] != "" {
		target = payoutType[0]
	}

	var records [][]string
	header := []string{
		"Transaction Reference",
		"Batch ID",
		"Beneficiary Role",
		"Beneficiary Tracking ID",
		"Beneficiary Name",
		"Bank / Branch",
		"Beneficiary IBAN / Account",
		"Amount (PKR)",
		"Currency",
		"Narration",
		"Value Date",
		"Disbursement Status",
	}

	// 1. Query Vendor Payouts (if requested)
	if target == "all" || target == "vendor" {
		query := `
			SELECT 
				vp.id::text,
				COALESCE(vp.batch_id::text, ''),
				vp.vendor_tracking_id,
				COALESCE(vp.beneficiary_name, u.full_name, u.business_name, 'Vendor'),
				COALESCE(vp.bank_name, 'Bank'),
				COALESCE(vp.beneficiary_iban, u.phone, ''),
				vp.amount,
				vp.status,
				vp.created_at
			FROM vendor_payouts vp
			LEFT JOIN users u ON vp.vendor_tracking_id = u.tracking_id
		`
		var args []interface{}
		if batchID != "" {
			query += ` WHERE vp.batch_id = $1 ORDER BY vp.created_at ASC`
			args = append(args, batchID)
		} else {
			query += ` WHERE vp.status IN ('pending', 'pending_disbursement', 'scheduled', 'approved') ORDER BY vp.created_at ASC`
		}

		rows, err := s.db.Query(ctx, query, args...)
		if err == nil {
			for rows.Next() {
				var id, bID, vendorID, name, bank, account, status string
				var amount float64
				var createdAt time.Time
				if err := rows.Scan(&id, &bID, &vendorID, &name, &bank, &account, &amount, &status, &createdAt); err == nil {
					records = append(records, []string{
						id,
						bID,
						"Vendor",
						vendorID,
						name,
						bank,
						account,
						fmt.Sprintf("%.2f", amount),
						"PKR",
						fmt.Sprintf("Omnigo Vendor Payout - %s", vendorID),
						createdAt.Format("2006-01-02"),
						status,
					})
				}
			}
			rows.Close()
		}
	}

	// 2. Query Rider Payouts (if requested)
	if target == "all" || target == "rider" {
		query := `
			SELECT 
				rp.id::text,
				COALESCE(rp.batch_id::text, ''),
				rp.rider_tracking_id,
				COALESCE(rp.account_title, u.full_name, 'Rider'),
				COALESCE(rp.bank_name, 'Bank'),
				COALESCE(rp.beneficiary_iban, rp.account_number, u.phone, ''),
				rp.amount,
				rp.status,
				rp.created_at
			FROM rider_payouts rp
			LEFT JOIN users u ON rp.rider_tracking_id = u.tracking_id
		`
		var args []interface{}
		if batchID != "" {
			query += ` WHERE rp.batch_id = $1 ORDER BY rp.created_at ASC`
			args = append(args, batchID)
		} else {
			query += ` WHERE rp.status IN ('pending', 'approved') ORDER BY rp.created_at ASC`
		}

		rows, err := s.db.Query(ctx, query, args...)
		if err == nil {
			for rows.Next() {
				var id, bID, riderID, name, bank, account, status string
				var amount float64
				var createdAt time.Time
				if err := rows.Scan(&id, &bID, &riderID, &name, &bank, &account, &amount, &status, &createdAt); err == nil {
					records = append(records, []string{
						id,
						bID,
						"Rider",
						riderID,
						name,
						bank,
						account,
						fmt.Sprintf("%.2f", amount),
						"PKR",
						fmt.Sprintf("Omnigo Rider Payout - %s", riderID),
						createdAt.Format("2006-01-02"),
						status,
					})
				}
			}
			rows.Close()
		}
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	if err := writer.Write(header); err != nil {
		return nil, "", err
	}

	for _, rec := range records {
		if err := writer.Write(rec); err != nil {
			return nil, "", err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, "", fmt.Errorf("csv write error: %w", err)
	}

	filename := fmt.Sprintf("1ibft_payouts_%s_%s.csv", target, time.Now().Format("20060102_150405"))
	return buf.Bytes(), filename, nil
}
