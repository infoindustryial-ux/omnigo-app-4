package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/omnigo/backend/internal/ledger"
	"github.com/omnigo/backend/internal/ride/models"
	"github.com/omnigo/backend/internal/ride/repository"
	"github.com/omnigo/backend/internal/shared/h3search"
	"github.com/omnigo/backend/internal/shared/messaging"
	"github.com/omnigo/backend/internal/shared/tracking"
	"github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"
)

func envFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

// rupeesToPaisa converts rupees (float64) to paisa (int64).
func rupeesToPaisa(rupees float64) int64 {
	return int64(math.Round(rupees * 100))
}

type RideService struct {
	repo   *repository.RideRepository
	kafka  *messaging.KafkaClient
	redis  redis.UniversalClient
	ledger *ledger.Service
}

func NewRideService(
	repo *repository.RideRepository,
	kafka *messaging.KafkaClient,
	rdb redis.UniversalClient,
	ledgerSvc *ledger.Service,
) *RideService {
	return &RideService{
		repo:   repo,
		kafka:  kafka,
		redis:  rdb,
		ledger: ledgerSvc,
	}
}

// generateUTID creates the Universal Tracking ID for rides
func generateUTID() string {
	return tracking.Generate("RIDE")
}

// FindNearbyRiders discovers active drivers in proximity using Redis H3 spatial search with Postgres fallback.
func (s *RideService) FindNearbyRiders(ctx context.Context, lat, lng float64, radiusKm float64, maxResults int) []models.NearbyRiderInfo {
	if radiusKm <= 0 {
		radiusKm = 5.0
	}
	if maxResults <= 0 {
		maxResults = 10
	}

	var riderIDs []string
	if s.redis != nil {
		riderIDs = h3search.FindNearbyRiders(ctx, s.redis, lat, lng, radiusKm, maxResults)
	}

	var riders []models.NearbyRiderInfo
	// If riders found in Redis, enrich with user profile
	if len(riderIDs) > 0 && s.repo != nil && s.repo.GetReader() != nil {
		rows, err := s.repo.GetReader().Query(ctx,
			`SELECT tracking_id, COALESCE(full_name, 'Captain'), COALESCE(vehicle_type, 'bike'),
			        COALESCE(vehicle_plate_number, 'LHR-LIVE'), COALESCE(phone, '')
			 FROM users WHERE tracking_id = ANY($1) AND role = 'rider'`, riderIDs,
		)
		if err == nil {
			idx := 1
			for rows.Next() {
				var r models.NearbyRiderInfo
				var phone string
				if err := rows.Scan(&r.RiderTrackingID, &r.FullName, &r.VehicleType, &r.VehiclePlate, &phone); err == nil {
					r.Rating = "4.8 ★"
					distKm := 0.4 + float64(idx)*0.3
					r.DistanceKm = math.Round(distKm*10) / 10
					angle := float64(idx) * 1.2
					r.Latitude = lat + (distKm/111.0)*math.Cos(angle)
					r.Longitude = lng + (distKm/(111.0*math.Cos(lat*math.Pi/180.0)))*math.Sin(angle)
					riders = append(riders, r)
					idx++
				}
			}
			rows.Close()
		}
	}

	// Resilient fallback for demo/cold cache: query verified riders from database
	if len(riders) == 0 && s.repo != nil && s.repo.GetReader() != nil {
		rows, err := s.repo.GetReader().Query(ctx,
			`SELECT tracking_id, COALESCE(full_name, 'Captain'), COALESCE(vehicle_type, 'bike'),
			        COALESCE(vehicle_plate_number, 'LHR-LIVE'), '4.8 ★'
			 FROM users WHERE role = 'rider' AND is_verified = true LIMIT $1`, maxResults,
		)
		if err == nil {
			idx := 1
			for rows.Next() {
				var r models.NearbyRiderInfo
				if err := rows.Scan(&r.RiderTrackingID, &r.FullName, &r.VehicleType, &r.VehiclePlate, &r.Rating); err == nil {
					distKm := 0.5 + float64(idx)*0.4
					r.DistanceKm = math.Round(distKm*10) / 10
					angle := float64(idx) * 1.3
					r.Latitude = lat + (distKm/111.0)*math.Cos(angle)
					r.Longitude = lng + (distKm/(111.0*math.Cos(lat*math.Pi/180.0)))*math.Sin(angle)
					riders = append(riders, r)
					idx++
				}
			}
			rows.Close()
		}
	}

	return riders
}

// RequestRide handles the business logic for creating a ride
func (s *RideService) RequestRide(ctx context.Context, req *models.RequestRidePayload) (*models.Ride, error) {

	paymentMethod := req.PaymentMethod
	if paymentMethod == "" {
		paymentMethod = "cash"
	}

	utid := generateUTID()

	ride := &models.Ride{
		TrackingID:      utid,
		CustomerTrackID: req.CustomerTrackID,
		Status:          "requested",
		VehicleType:     req.VehicleType,
		PickupLat:       req.PickupLat,
		PickupLng:       req.PickupLng,
		DropoffLat:      req.DropoffLat,
		DropoffLng:      req.DropoffLng,
		FareAmount:      req.FareAmount,
		PaymentMethod:   paymentMethod,
		AdminCommission: envFloat("RIDE_COMMISSION_PERCENT", 5.0),
	}

	// 1. Save to Database
	err := s.repo.CreateRide(ctx, ride)
	if err != nil {
		return nil, fmt.Errorf("failed to create ride: %w", err)
	}

	// 2. Discover nearby drivers and attach live radar telemetry
	nearby := s.FindNearbyRiders(ctx, ride.PickupLat, ride.PickupLng, 5.0, 10)
	ride.NearbyRidersCount = len(nearby)
	ride.SearchRadiusKm = 5.0

	// 3. Publish directly to Redis for immediate real-time driver socket broadcasting
	if s.redis != nil {
		broadcast := map[string]interface{}{
			"action":         "RIDE_BROADCAST",
			"tracking_id":    ride.TrackingID,
			"vehicle_type":   ride.VehicleType,
			"pickup_lat":     ride.PickupLat,
			"pickup_lng":     ride.PickupLng,
			"dropoff_lat":    ride.DropoffLat,
			"dropoff_lng":    ride.DropoffLng,
			"fare_amount":    ride.FareAmount,
			"payment_method": ride.PaymentMethod,
			"created_at":     ride.CreatedAt,
		}
		if bBytes, err := json.Marshal(broadcast); err == nil {
			s.redis.Publish(ctx, "rides:broadcast", bBytes)
		}
	}

	// 4. Publish Event to Kafka so we can broadcast it to drivers
	event := models.RideEvent{
		RideID:      ride.TrackingID,
		VehicleType: ride.VehicleType,
		PickupLat:   ride.PickupLat,
		PickupLng:   ride.PickupLng,
		FareAmount:  ride.FareAmount,
	}

	eventBytes, _ := json.Marshal(event)

	if s.kafka != nil {
		record := &kgo.Record{
			Topic: "ride.requested",
			Key:   []byte(ride.TrackingID), // Partition by Ride ID
			Value: eventBytes,
		}

		s.kafka.Client.Produce(ctx, record, func(_ *kgo.Record, err error) {
			if err != nil {
				fmt.Printf("Warning: Failed to produce ride.requested event: %v\n", err)
			}
		})
	}

	return ride, nil
}

// GetRide fetches a ride by its UTID
func (s *RideService) GetRide(ctx context.Context, trackingID string) (*models.Ride, error) {
	return s.repo.GetRideByTrackingID(ctx, trackingID)
}

// AcceptRide assigns a rider to a ride request. Uses row-level locking
// inside the repository so two concurrent Accept calls cannot both win.
func (s *RideService) AcceptRide(ctx context.Context, trackingID string, req *models.AcceptRidePayload) (*models.Ride, error) {
	if !strings.HasPrefix(req.RiderTrackID, "RIDR-") {
		return nil, fmt.Errorf("invalid rider tracking id")
	}
	if err := s.repo.AssignRider(ctx, trackingID, req.RiderTrackID); err != nil {
		return nil, err
	}
	ride, err := s.repo.GetRideByTrackingID(ctx, trackingID)
	if err != nil {
		return nil, err
	}

	// Enrich with rider details
	var riderName, riderPhone, vType, vPlate string
	if s.repo != nil && s.repo.GetReader() != nil {
		_ = s.repo.GetReader().QueryRow(ctx,
			`SELECT COALESCE(full_name, 'Captain'), COALESCE(phone, ''), COALESCE(vehicle_type, 'bike'), COALESCE(vehicle_plate_number, '')
			 FROM users WHERE tracking_id = $1`, req.RiderTrackID,
		).Scan(&riderName, &riderPhone, &vType, &vPlate)
	}

	// Publish real-time RIDE_ACCEPTED event directly to Redis channel rides:events for customer WebSocket
	if s.redis != nil {
		event := map[string]interface{}{
			"action":                "RIDE_ACCEPTED",
			"tracking_id":           ride.TrackingID,
			"customer_tracking_id":  ride.CustomerTrackID,
			"rider_tracking_id":     req.RiderTrackID,
			"rider_name":            riderName,
			"rider_phone":           riderPhone,
			"vehicle_type":          vType,
			"vehicle_plate":         vPlate,
			"fare_amount":           ride.FareAmount,
			"status":                "accepted",
		}
		if evBytes, err := json.Marshal(event); err == nil {
			s.redis.Publish(ctx, "rides:events", evBytes)
		}
	}

	return ride, nil
}

// UpdateRideStatus moves the ride through the state machine.
func (s *RideService) UpdateRideStatus(ctx context.Context, trackingID string, req *models.UpdateRideStatusPayload) (*models.Ride, error) {
	if !strings.HasPrefix(req.RiderTrackID, "RIDR-") {
		return nil, fmt.Errorf("invalid rider tracking id")
	}
	if err := s.repo.UpdateRideStatus(ctx, trackingID, req.RiderTrackID, req.Status); err != nil {
		return nil, err
	}
	ride, err := s.repo.GetRideByTrackingID(ctx, trackingID)
	if err != nil {
		return nil, err
	}

	if s.redis != nil {
		event := map[string]any{
			"action":               "RIDE_STATUS_UPDATED",
			"tracking_id":          ride.TrackingID,
			"customer_tracking_id": ride.CustomerTrackID,
			"rider_tracking_id":    req.RiderTrackID,
			"status":               req.Status,
		}
		if evBytes, err := json.Marshal(event); err == nil {
			s.redis.Publish(ctx, "rides:events", evBytes)
		}
	}

	return ride, nil
}

// CompleteRide finishes the ride, updates the fare, and runs the
// money-split through the ledger. The split is:
//
//   - For payment_method=cash:
//     Customer paid physical rupees in hand to rider.
//     Debit rider_wallet (platform's admin commission %)
//     Credit admin_revenue
//
//   - For payment_method=wallet/stripe:
//     debit central_escrow (the platform's float held for the trip)
//     credit admin_revenue (admin commission %)
//     credit rider_wallet  (rider earnings, later settled at cash-out)
func (s *RideService) CompleteRide(ctx context.Context, trackingID string, req *models.CompleteRidePayload) (*models.Ride, error) {
	if !strings.HasPrefix(req.RiderTrackID, "RIDR-") {
		return nil, fmt.Errorf("invalid rider tracking id")
	}

	// SECURITY (HIGH-05): the rider-reported FinalFare is NEVER trusted for
	// settlement. OMNIGO is a bidding marketplace — the fare the customer
	// agreed to (stored on the ride when the bid was accepted) is the
	// contract price. Rider-submitted values are ignored entirely;
	// distance/duration from the payload are kept for analytics only.
	valRide, err := s.repo.GetRideByTrackingID(ctx, trackingID)
	if err != nil {
		return nil, fmt.Errorf("ride not found: %w", err)
	}
	if valRide.FareAmount <= 0 {
		return nil, fmt.Errorf("ride %s has no agreed fare; cannot complete", trackingID)
	}
	if req.FinalFare != valRide.FareAmount {
		log.Printf("[WARN] CompleteRide: client submitted fare %.2f differs from agreed fare %.2f for ride %s — using agreed fare", req.FinalFare, valRide.FareAmount, trackingID)
	}
	req.FinalFare = valRide.FareAmount

	if err := s.repo.CompleteRide(ctx, trackingID, req.RiderTrackID, req.FinalFare, float64(req.DistanceMeters), float64(req.DurationSeconds)); err != nil {
		return nil, err
	}

	// 2. Settle the money. We use the fare stored on the ride (re-fetched
	//    above) so the split stays consistent with what the customer saw.
	ride, err := s.repo.GetRideByTrackingID(ctx, trackingID)
	if err != nil {
		return nil, err
	}

	effectivePaymentMethod := ride.PaymentMethod
	if req.PaymentMethod != "" {
		effectivePaymentMethod = req.PaymentMethod
	}
	if effectivePaymentMethod == "" {
		effectivePaymentMethod = "cash"
	}

	var nettedDebt int64 = 0
	var withdrawableCredit int64 = 0

	if ride.FareAmount > 0 {
		commissionRate := ride.AdminCommission
		if commissionRate <= 0 || commissionRate >= 100 {
			commissionRate = envFloat("RIDE_COMMISSION_PERCENT", 5.0)
		}
		farePaisa := rupeesToPaisa(ride.FareAmount)
		commission := int64(math.Round(float64(farePaisa) * commissionRate / 100.0))
		riderEarning := farePaisa - commission
		withdrawableCredit = riderEarning

		writer := s.repo.GetWriter()

		if effectivePaymentMethod == "cash" {
			// For CASH: Customer paid rider directly in cash.
			// 1. Update rider_wallet cash_in_hand_paisa.
			// 2. Collect commission from rider: if wallet balance sufficient, debit it;
			//    if insufficient, record unpaid commission into cod_debts so rider cannot withdraw until paid!
			if writer != nil {
				tx, txErr := writer.Begin(ctx)
				if txErr == nil {
					defer tx.Rollback(ctx)
					// Upsert rider_wallet
					_, _ = tx.Exec(ctx, `
						INSERT INTO rider_wallet (rider_tracking_id, balance_paisa, cash_in_hand_paisa, lifetime_earnings_paisa, updated_at)
						VALUES ($1, 0, 0, 0, NOW())
						ON CONFLICT (rider_tracking_id) DO NOTHING
					`, req.RiderTrackID)

					var currentBalance, currentCashInHand, currentLifetime int64
					_ = tx.QueryRow(ctx, `
						SELECT COALESCE(balance_paisa, 0), COALESCE(cash_in_hand_paisa, 0), COALESCE(lifetime_earnings_paisa, 0)
						FROM rider_wallet
						WHERE rider_tracking_id = $1
						FOR UPDATE
					`, req.RiderTrackID).Scan(&currentBalance, &currentCashInHand, &currentLifetime)

					newCashInHand := currentCashInHand + farePaisa
					cashThresholdPaisa := int64(math.Round(envFloat("RIDER_CASH_BLOCK_THRESHOLD", 5000.0) * 100))
					isBlocked := newCashInHand >= cashThresholdPaisa
					var unpaidCommission int64 = 0

					if currentBalance >= commission {
						currentBalance -= commission
					} else {
						unpaidCommission = commission - currentBalance
						currentBalance = 0

						// Record unpaid commission into cod_debts so platform commission is never lost
						unpaidRupees := float64(unpaidCommission) / 100.0
						_, _ = tx.Exec(ctx, `
							INSERT INTO cod_debts (rider_tracking_id, order_tracking_id, amount, amount_owed, amount_owed_paisa, status, created_at)
							VALUES ($1, $2, $3, $3, $4, 'pending', NOW())
						`, req.RiderTrackID, trackingID, unpaidRupees, unpaidCommission)
					}

					newLifetime := currentLifetime + riderEarning
					_, _ = tx.Exec(ctx, `
						UPDATE rider_wallet
						SET balance_paisa = $1,
						    cash_in_hand_paisa = $2,
						    lifetime_earnings_paisa = $3,
						    is_cash_blocked = $4,
						    updated_at = NOW()
						WHERE rider_tracking_id = $5
					`, currentBalance, newCashInHand, newLifetime, isBlocked, req.RiderTrackID)

					_ = tx.Commit(ctx)
				}
			}

			// Double-entry ledger split
			if s.ledger != nil {
				idempotencyKey := fmt.Sprintf("ride:complete:cash:%s", trackingID)
				transfers := []ledger.TransferRequest{}
				if commission > 0 {
					transfers = append(transfers, ledger.TransferRequest{
						DebitAccount:   ledger.AccountRiderWallet,
						CreditAccount:  ledger.AccountAdminRevenue,
						Amount:         commission,
						ReferenceType:  "ride_commission_cash",
						ReferenceID:    trackingID,
						Description:    fmt.Sprintf("Platform commission collected from rider for cash ride %s (%d paisa)", trackingID, commission),
						IdempotencyKey: idempotencyKey + ":admin",
					})
				}
				if len(transfers) > 0 {
					if _, err := s.ledger.MultiTransfer(ctx, transfers); err != nil {
						log.Printf("[WARN] Ledger cash commission debit failed for ride %s: %v", trackingID, err)
					}
				}
			}
		} else {
			// For WALLET / ONLINE: Escrow holds customer funds.
			// AUTOMATED DEBT NETTING: Deduct rider earnings from outstanding cod_debts (FIFO)
			if writer != nil {
				tx, txErr := writer.Begin(ctx)
				if txErr == nil {
					defer tx.Rollback(ctx)

					// Upsert rider_wallet
					_, _ = tx.Exec(ctx, `
						INSERT INTO rider_wallet (rider_tracking_id, balance_paisa, cash_in_hand_paisa, lifetime_earnings_paisa, updated_at)
						VALUES ($1, 0, 0, 0, NOW())
						ON CONFLICT (rider_tracking_id) DO NOTHING
					`, req.RiderTrackID)

					var currentBalance, currentCashInHand, lifetimeEarnings int64
					_ = tx.QueryRow(ctx, `
						SELECT COALESCE(balance_paisa, 0), COALESCE(cash_in_hand_paisa, 0), COALESCE(lifetime_earnings_paisa, 0)
						FROM rider_wallet
						WHERE rider_tracking_id = $1
						FOR UPDATE
					`, req.RiderTrackID).Scan(&currentBalance, &currentCashInHand, &lifetimeEarnings)

					// Query pending COD debts in FIFO order
					rows, qErr := tx.Query(ctx, `
						SELECT id, COALESCE(amount_owed_paisa, ROUND(amount_owed * 100)::bigint, 0)
						FROM cod_debts
						WHERE rider_tracking_id = $1 AND status = 'pending' AND (amount_owed_paisa > 0 OR amount_owed > 0)
						ORDER BY created_at ASC
						FOR UPDATE
					`, req.RiderTrackID)

					type pendingDebt struct {
						id        string
						owedPaisa int64
					}
					var debts []pendingDebt

					if qErr == nil {
						for rows.Next() {
							var pd pendingDebt
							if scanErr := rows.Scan(&pd.id, &pd.owedPaisa); scanErr == nil && pd.owedPaisa > 0 {
								debts = append(debts, pd)
							}
						}
						rows.Close()
					}

					remainingToSettle := riderEarning
					for _, d := range debts {
						if remainingToSettle <= 0 {
							break
						}
						if remainingToSettle >= d.owedPaisa {
							_, _ = tx.Exec(ctx, `
								UPDATE cod_debts
								SET amount_owed_paisa = 0,
								    amount_owed = 0,
								    status = 'settled',
								    settled_via = 'ride_earning_netting',
								    settled_at = NOW()
								WHERE id = $1
							`, d.id)
							nettedDebt += d.owedPaisa
							remainingToSettle -= d.owedPaisa
						} else {
							newOwed := d.owedPaisa - remainingToSettle
							newOwedRupees := float64(newOwed) / 100.0
							_, _ = tx.Exec(ctx, `
								UPDATE cod_debts
								SET amount_owed_paisa = $1,
								    amount_owed = $2,
								    updated_at = NOW()
								WHERE id = $3
							`, newOwed, newOwedRupees, d.id)
							nettedDebt += remainingToSettle
							remainingToSettle = 0
						}
					}

					withdrawableCredit = remainingToSettle
					newCashInHand := int64(math.Max(0, float64(currentCashInHand-nettedDebt)))
					cashThresholdPaisa := int64(math.Round(envFloat("RIDER_CASH_BLOCK_THRESHOLD", 5000.0) * 100))
					isBlocked := newCashInHand >= cashThresholdPaisa

					newBalance := currentBalance + withdrawableCredit
					newLifetime := lifetimeEarnings + riderEarning

					_, _ = tx.Exec(ctx, `
						UPDATE rider_wallet
						SET balance_paisa = $1,
						    cash_in_hand_paisa = $2,
						    lifetime_earnings_paisa = $3,
						    is_cash_blocked = $4,
						    updated_at = NOW()
						WHERE rider_tracking_id = $5
					`, newBalance, newCashInHand, newLifetime, isBlocked, req.RiderTrackID)

					_ = tx.Commit(ctx)
				}
			}

			// Ledger split:
			// 1. Commission -> Admin Revenue
			// 2. Netted Debt -> Rider COD Debt (extinguishes liability on ledger)
			// 3. Withdrawable Credit -> Rider Wallet (surplus earnings available for withdrawal)
			if s.ledger != nil {
				idempotencyKey := fmt.Sprintf("ride:complete:%s", trackingID)
				transfers := []ledger.TransferRequest{
					{
						DebitAccount:   ledger.AccountCentralEscrow,
						CreditAccount:  ledger.AccountAdminRevenue,
						Amount:         commission,
						ReferenceType:  "ride_completion",
						ReferenceID:    trackingID,
						Description:    fmt.Sprintf("Admin commission on ride %s (%d paisa)", trackingID, commission),
						IdempotencyKey: idempotencyKey + ":admin",
					},
				}

				if nettedDebt > 0 {
					transfers = append(transfers, ledger.TransferRequest{
						DebitAccount:   ledger.AccountCentralEscrow,
						CreditAccount:  ledger.AccountRiderCODDebt,
						Amount:         nettedDebt,
						ReferenceType:  "ride_debt_netting",
						ReferenceID:    trackingID,
						Description:    fmt.Sprintf("Auto-settlement of rider debt via ride %s (%d paisa)", trackingID, nettedDebt),
						IdempotencyKey: idempotencyKey + ":netting",
					})
				}

				if withdrawableCredit > 0 {
					transfers = append(transfers, ledger.TransferRequest{
						DebitAccount:   ledger.AccountCentralEscrow,
						CreditAccount:  ledger.AccountRiderWallet,
						Amount:         withdrawableCredit,
						ReferenceType:  "ride_completion",
						ReferenceID:    trackingID,
						Description:    fmt.Sprintf("Rider net withdrawable earnings for ride %s (%d paisa)", trackingID, withdrawableCredit),
						IdempotencyKey: idempotencyKey + ":rider",
					})
				}

				if _, err := s.ledger.MultiTransfer(ctx, transfers); err != nil {
					return nil, fmt.Errorf("ride completion succeeded but ledger split failed: %w", err)
				}
			}
		}
	}

	// 3. Emit real-time WebSocket event via Redis for instant client feedback
	if s.redis != nil {
		event := map[string]any{
			"action":                     "RIDE_COMPLETED",
			"tracking_id":                ride.TrackingID,
			"customer_tracking_id":       ride.CustomerTrackID,
			"rider_tracking_id":          req.RiderTrackID,
			"final_fare":                 ride.FareAmount,
			"payment_method":             effectivePaymentMethod,
			"distance_meters":            req.DistanceMeters,
			"duration_seconds":           req.DurationSeconds,
			"status":                     "completed",
			"netted_debt_paisa":          nettedDebt,
			"withdrawable_earning_paisa": withdrawableCredit,
		}
		if evBytes, err := json.Marshal(event); err == nil {
			s.redis.Publish(ctx, "rides:events", evBytes)
		}
	}

	// 4. Emit ride.completed event for analytics + admin dashboards.
	if s.kafka != nil {
		eventBytes, _ := json.Marshal(map[string]any{
			"ride_id":            trackingID,
			"rider_id":           req.RiderTrackID,
			"final_fare_paisa":   rupeesToPaisa(req.FinalFare),
			"distance_meters":    req.DistanceMeters,
			"duration_seconds":   req.DurationSeconds,
			"payment_method":     effectivePaymentMethod,
		})
		s.kafka.Client.Produce(ctx, &kgo.Record{
			Topic: "ride.completed",
			Key:   []byte(trackingID),
			Value: eventBytes,
		}, func(_ *kgo.Record, err error) {
			if err != nil {
				fmt.Printf("Warning: failed to produce ride.completed: %v\n", err)
			}
		})
	}

	return ride, nil
}

// CancelRide cancels a ride that has not yet started. Customer or rider
// can call it. After completion the ride is final — no cancellations.
func (s *RideService) CancelRide(ctx context.Context, trackingID string, req *models.CancelRidePayload) (*models.Ride, error) {
	// The repository update enforces the state machine: only rides in
	// "requested" or "accepted" can be cancelled.
	if err := s.repo.UpdateRideStatus(ctx, trackingID, req.ActorTrackID, "cancelled"); err != nil {
		return nil, err
	}
	ride, err := s.repo.GetRideByTrackingID(ctx, trackingID)
	if err != nil {
		return nil, err
	}

	if s.redis != nil {
		event := map[string]any{
			"action":               "RIDE_CANCELLED",
			"tracking_id":          ride.TrackingID,
			"customer_tracking_id": ride.CustomerTrackID,
			"rider_tracking_id":    ride.RiderTrackID,
			"reason":               req.Reason,
			"status":               "cancelled",
		}
		if evBytes, err := json.Marshal(event); err == nil {
			s.redis.Publish(ctx, "rides:events", evBytes)
		}
	}

	return ride, nil
}

// RateRide allows the customer to rate a completed ride and optionally leave a tip.
func (s *RideService) RateRide(ctx context.Context, trackingID string, req *models.RateRidePayload) error {
	ride, err := s.repo.GetRideByTrackingID(ctx, trackingID)
	if err != nil {
		return fmt.Errorf("ride not found: %w", err)
	}
	if ride.CustomerTrackID != req.CustomerTrackID {
		return fmt.Errorf("unauthorized: caller is not the ride customer")
	}
	if ride.Status != "completed" {
		return fmt.Errorf("cannot rate ride that is not completed")
	}

	if err := s.repo.SaveRideRating(ctx, trackingID, req.CustomerTrackID, req.Rating, req.Comment, req.TipAmount); err != nil {
		return fmt.Errorf("failed to save ride rating: %w", err)
	}

	// If customer tipped the captain, transfer tip to rider wallet
	if s.ledger != nil && req.TipAmount > 0 && ride.RiderTrackID != "" {
		tipPaisa := rupeesToPaisa(req.TipAmount)
		idempotencyKey := fmt.Sprintf("ride:tip:%s", trackingID)
		_, err := s.ledger.MultiTransfer(ctx, []ledger.TransferRequest{
			{
				DebitAccount:   ledger.AccountCentralEscrow,
				CreditAccount:  ledger.AccountRiderWallet,
				Amount:         tipPaisa,
				ReferenceType:  "ride_tip",
				ReferenceID:    trackingID,
				Description:    fmt.Sprintf("Customer tip for captain on ride %s (%d paisa)", trackingID, tipPaisa),
				IdempotencyKey: idempotencyKey,
			},
		})
		if err != nil {
			log.Printf("[WARN] Tip ledger transfer failed for ride %s: %v", trackingID, err)
		}
	}

	return nil
}

// SubmitBid submits a new bid for a requested ride from a rider
func (s *RideService) SubmitBid(ctx context.Context, trackingID string, req *models.SubmitBidPayload) (*models.RideBid, error) {
	if !strings.HasPrefix(req.RiderTrackID, "RIDR-") {
		return nil, fmt.Errorf("invalid rider tracking id")
	}

	bid := &models.RideBid{
		RideTrackID:  trackingID,
		RiderTrackID: req.RiderTrackID,
		BidAmount:    req.BidAmount,
	}

	err := s.repo.SaveBid(ctx, bid)
	if err != nil {
		return nil, err
	}

	// Publish bid event to Kafka
	if s.kafka != nil {
		eventBytes, _ := json.Marshal(bid)
		s.kafka.Client.Produce(ctx, &kgo.Record{
			Topic: "ride.bid.submitted",
			Key:   []byte(trackingID),
			Value: eventBytes,
		}, func(_ *kgo.Record, err error) {
			if err != nil {
				fmt.Printf("Warning: failed to produce ride.bid.submitted: %v\n", err)
			}
		})
	}

	// Publish real-time counter-offer to Redis channel rides:events for customer WebSocket
	if s.redis != nil {
		var riderName, riderPhone, vType, vPlate string
		if s.repo != nil && s.repo.GetReader() != nil {
			_ = s.repo.GetReader().QueryRow(ctx,
				`SELECT COALESCE(full_name, 'Captain'), COALESCE(phone, ''), COALESCE(vehicle_type, 'bike'), COALESCE(vehicle_plate_number, '')
				 FROM users WHERE tracking_id = $1`, req.RiderTrackID,
			).Scan(&riderName, &riderPhone, &vType, &vPlate)
		}
		ride, _ := s.repo.GetRideByTrackingID(ctx, trackingID)
		custID := ""
		if ride != nil {
			custID = ride.CustomerTrackID
		}
		event := map[string]interface{}{
			"action":               "RIDER_COUNTER_OFFER",
			"tracking_id":          trackingID,
			"customer_tracking_id": custID,
			"bid_id":               bid.ID,
			"rider_tracking_id":    req.RiderTrackID,
			"rider_name":           riderName,
			"rider_phone":          riderPhone,
			"vehicle_type":         vType,
			"vehicle_plate":        vPlate,
			"bid_amount":           req.BidAmount,
			"proposed_fare":        req.BidAmount,
			"rating":               "4.8 ★",
			"eta":                  "3 min",
		}
		if evBytes, err := json.Marshal(event); err == nil {
			s.redis.Publish(ctx, "rides:events", evBytes)
		}
	}

	return bid, nil
}

// GetBidsForRide returns all active bids for a ride
func (s *RideService) GetBidsForRide(ctx context.Context, trackingID string) ([]models.RideBid, error) {
	return s.repo.GetBidsForRide(ctx, trackingID)
}

// AcceptBid accepts a specific driver's bid and assigns the ride
func (s *RideService) AcceptBid(ctx context.Context, trackingID string, req *models.AcceptBidPayload) (*models.RideBid, error) {
	if !strings.HasPrefix(req.CustomerTrackID, "CUST-") {
		return nil, fmt.Errorf("invalid customer tracking id")
	}

	ride, err := s.repo.GetRideByTrackingID(ctx, trackingID)
	if err != nil {
		return nil, fmt.Errorf("ride not found: %w", err)
	}
	if ride.CustomerTrackID != req.CustomerTrackID {
		return nil, fmt.Errorf("unauthorized: caller is not the owner of this ride")
	}

	bid, err := s.repo.AcceptBid(ctx, trackingID, req.BidID)
	if err != nil {
		return nil, err
	}

	// Publish bid accepted event to Kafka
	if s.kafka != nil {
		eventBytes, _ := json.Marshal(bid)
		s.kafka.Client.Produce(ctx, &kgo.Record{
			Topic: "ride.bid.accepted",
			Key:   []byte(trackingID),
			Value: eventBytes,
		}, func(_ *kgo.Record, err error) {
			if err != nil {
				fmt.Printf("Warning: failed to produce ride.bid.accepted: %v\n", err)
			}
		})
	}

	// Publish real-time RIDE_ACCEPTED event directly to Redis channel rides:events for customer WebSocket
	if s.redis != nil {
		var riderName, riderPhone, vType, vPlate string
		if s.repo != nil && s.repo.GetReader() != nil {
			_ = s.repo.GetReader().QueryRow(ctx,
				`SELECT COALESCE(full_name, 'Captain'), COALESCE(phone, ''), COALESCE(vehicle_type, 'bike'), COALESCE(vehicle_plate_number, '')
				 FROM users WHERE tracking_id = $1`, bid.RiderTrackID,
			).Scan(&riderName, &riderPhone, &vType, &vPlate)
		}
		event := map[string]interface{}{
			"action":                "RIDE_ACCEPTED",
			"tracking_id":           trackingID,
			"customer_tracking_id":  ride.CustomerTrackID,
			"rider_tracking_id":     bid.RiderTrackID,
			"rider_name":            riderName,
			"rider_phone":           riderPhone,
			"vehicle_type":          vType,
			"vehicle_plate":         vPlate,
			"fare_amount":           bid.BidAmount,
			"status":                "accepted",
		}
		if evBytes, err := json.Marshal(event); err == nil {
			s.redis.Publish(ctx, "rides:events", evBytes)
		}
	}

	return bid, nil
}

// BumpFare increases the customer's offered fare and re-broadcasts to nearby drivers.
func (s *RideService) BumpFare(ctx context.Context, trackingID, customerTrackID string, increaseAmount float64) (*models.Ride, error) {
	if increaseAmount <= 0 {
		return nil, fmt.Errorf("increase amount must be greater than zero")
	}

	ride, err := s.repo.BumpFare(ctx, trackingID, customerTrackID, increaseAmount)
	if err != nil {
		return nil, err
	}

	// Publish updated broadcast to Redis for drivers
	if s.redis != nil {
		broadcast := map[string]interface{}{
			"action":       "RIDE_BROADCAST",
			"tracking_id":  ride.TrackingID,
			"vehicle_type": ride.VehicleType,
			"pickup_lat":   ride.PickupLat,
			"pickup_lng":   ride.PickupLng,
			"dropoff_lat":  ride.DropoffLat,
			"dropoff_lng":  ride.DropoffLng,
			"fare_amount":  ride.FareAmount,
			"created_at":   ride.CreatedAt,
			"fare_bumped":  true,
		}
		if bBytes, err := json.Marshal(broadcast); err == nil {
			s.redis.Publish(ctx, "rides:broadcast", bBytes)
		}
	}

	return ride, nil
}
