-- Migration: 0055_safepay_raastwire_payouts.down.sql

ALTER TABLE refund_requests DROP COLUMN IF EXISTS payout_id;
ALTER TABLE refund_requests DROP COLUMN IF EXISTS refund_gateway;
ALTER TABLE refund_requests DROP COLUMN IF EXISTS bank_name;
ALTER TABLE refund_requests DROP COLUMN IF EXISTS beneficiary_iban;
ALTER TABLE refund_requests DROP COLUMN IF EXISTS beneficiary_name;

DROP INDEX IF EXISTS idx_rider_payouts_scheduled;
ALTER TABLE rider_payouts DROP COLUMN IF EXISTS payout_fee_paisa;
ALTER TABLE rider_payouts DROP COLUMN IF EXISTS failure_reason;
ALTER TABLE rider_payouts DROP COLUMN IF EXISTS trace_reference;
ALTER TABLE rider_payouts DROP COLUMN IF EXISTS request_id;
ALTER TABLE rider_payouts DROP COLUMN IF EXISTS gateway;
ALTER TABLE rider_payouts DROP COLUMN IF EXISTS bank_name;
ALTER TABLE rider_payouts DROP COLUMN IF EXISTS raast_id;
ALTER TABLE rider_payouts DROP COLUMN IF EXISTS beneficiary_iban;

DROP INDEX IF EXISTS idx_vendor_payouts_scheduled;
ALTER TABLE vendor_payouts DROP COLUMN IF EXISTS payout_fee_paisa;
ALTER TABLE vendor_payouts DROP COLUMN IF EXISTS scheduled_at;
ALTER TABLE vendor_payouts DROP COLUMN IF EXISTS failure_reason;
ALTER TABLE vendor_payouts DROP COLUMN IF EXISTS trace_reference;
ALTER TABLE vendor_payouts DROP COLUMN IF EXISTS request_id;
ALTER TABLE vendor_payouts DROP COLUMN IF EXISTS gateway;
ALTER TABLE vendor_payouts DROP COLUMN IF EXISTS bank_name;
ALTER TABLE vendor_payouts DROP COLUMN IF EXISTS raast_id;
ALTER TABLE vendor_payouts DROP COLUMN IF EXISTS beneficiary_iban;
ALTER TABLE vendor_payouts DROP COLUMN IF EXISTS beneficiary_name;

DROP TABLE IF EXISTS user_bank_accounts CASCADE;
DROP TABLE IF EXISTS customer_payouts CASCADE;
