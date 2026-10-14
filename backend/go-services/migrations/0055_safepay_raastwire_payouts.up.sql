-- Migration: 0055_safepay_raastwire_payouts.up.sql
-- Description: Adds tables and columns for Safepay Raastwire bank disbursements (Vendors, Riders, Customers).

-- 1. Customer Payouts Table (for bank account withdrawals and direct bank refunds)
CREATE TABLE IF NOT EXISTS customer_payouts (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_tracking_id VARCHAR(100) NOT NULL,
    amount               DECIMAL(14,2) NOT NULL,
    amount_paisa         BIGINT NOT NULL DEFAULT 0,
    method               VARCHAR(30) NOT NULL DEFAULT 'bank_transfer',
    beneficiary_name     VARCHAR(100) NOT NULL,
    beneficiary_iban     VARCHAR(34),
    raast_id             VARCHAR(50),
    bank_name            VARCHAR(100),
    gateway              VARCHAR(30) NOT NULL DEFAULT 'safepay',
    status               VARCHAR(30) NOT NULL DEFAULT 'pending',
    request_id           UUID,
    trace_reference      VARCHAR(100),
    failure_reason       TEXT,
    payout_fee_paisa     BIGINT NOT NULL DEFAULT 0,
    batch_id             UUID,
    completed_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_customer_payouts_customer ON customer_payouts(customer_tracking_id, status);
CREATE INDEX IF NOT EXISTS idx_customer_payouts_status ON customer_payouts(status);

-- 2. User Bank Accounts Table (saved IBANs and Raast profiles for Customers, Vendors, and Riders)
CREATE TABLE IF NOT EXISTS user_bank_accounts (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_tracking_id     VARCHAR(100) NOT NULL,
    user_role            VARCHAR(30) NOT NULL DEFAULT 'vendor',
    beneficiary_name     VARCHAR(100) NOT NULL,
    beneficiary_iban     VARCHAR(34),
    raast_id             VARCHAR(50),
    bank_name            VARCHAR(100),
    is_default           BOOLEAN NOT NULL DEFAULT true,
    is_verified          BOOLEAN NOT NULL DEFAULT false,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_bank_accounts_user ON user_bank_accounts(user_tracking_id, user_role);

-- 3. Enhance vendor_payouts with bank details, 7-day schedule, fee, and Safepay tracking
ALTER TABLE vendor_payouts ADD COLUMN IF NOT EXISTS beneficiary_name VARCHAR(100);
ALTER TABLE vendor_payouts ADD COLUMN IF NOT EXISTS beneficiary_iban VARCHAR(34);
ALTER TABLE vendor_payouts ADD COLUMN IF NOT EXISTS raast_id VARCHAR(50);
ALTER TABLE vendor_payouts ADD COLUMN IF NOT EXISTS bank_name VARCHAR(100);
ALTER TABLE vendor_payouts ADD COLUMN IF NOT EXISTS gateway VARCHAR(30) DEFAULT 'safepay';
ALTER TABLE vendor_payouts ADD COLUMN IF NOT EXISTS request_id UUID;
ALTER TABLE vendor_payouts ADD COLUMN IF NOT EXISTS trace_reference VARCHAR(100);
ALTER TABLE vendor_payouts ADD COLUMN IF NOT EXISTS failure_reason TEXT;
ALTER TABLE vendor_payouts ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMPTZ;
ALTER TABLE vendor_payouts ADD COLUMN IF NOT EXISTS payout_fee_paisa BIGINT DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_vendor_payouts_scheduled ON vendor_payouts(status, scheduled_at);

-- 4. Enhance rider_payouts with IBAN, Safepay tracking, and fees
ALTER TABLE rider_payouts ADD COLUMN IF NOT EXISTS beneficiary_iban VARCHAR(34);
ALTER TABLE rider_payouts ADD COLUMN IF NOT EXISTS raast_id VARCHAR(50);
ALTER TABLE rider_payouts ADD COLUMN IF NOT EXISTS bank_name VARCHAR(100);
ALTER TABLE rider_payouts ADD COLUMN IF NOT EXISTS gateway VARCHAR(30) DEFAULT 'safepay';
ALTER TABLE rider_payouts ADD COLUMN IF NOT EXISTS request_id UUID;
ALTER TABLE rider_payouts ADD COLUMN IF NOT EXISTS trace_reference VARCHAR(100);
ALTER TABLE rider_payouts ADD COLUMN IF NOT EXISTS failure_reason TEXT;
ALTER TABLE rider_payouts ADD COLUMN IF NOT EXISTS payout_fee_paisa BIGINT DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_rider_payouts_scheduled ON rider_payouts(status, created_at);

-- 5. Refund Requests Table (with bank disbursement and Safepay tracking)
CREATE TABLE IF NOT EXISTS refund_requests (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_tracking_id    VARCHAR(100) NOT NULL,
    customer_tracking_id VARCHAR(100) NOT NULL,
    gateway              VARCHAR(30) NOT NULL DEFAULT 'safepay',
    gateway_txn_id       VARCHAR(100),
    amount               DECIMAL(14,2) NOT NULL,
    currency             VARCHAR(10) NOT NULL DEFAULT 'PKR',
    status               VARCHAR(30) NOT NULL DEFAULT 'pending',
    reason               TEXT,
    requested_by         VARCHAR(100),
    processed_by         VARCHAR(100),
    processed_at         TIMESTAMPTZ,
    notes                TEXT,
    beneficiary_name     VARCHAR(100),
    beneficiary_iban     VARCHAR(34),
    bank_name            VARCHAR(100),
    refund_gateway       VARCHAR(30) DEFAULT 'safepay',
    payout_id            UUID,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE refund_requests ADD COLUMN IF NOT EXISTS beneficiary_name VARCHAR(100);
ALTER TABLE refund_requests ADD COLUMN IF NOT EXISTS beneficiary_iban VARCHAR(34);
ALTER TABLE refund_requests ADD COLUMN IF NOT EXISTS bank_name VARCHAR(100);
ALTER TABLE refund_requests ADD COLUMN IF NOT EXISTS refund_gateway VARCHAR(30) DEFAULT 'safepay';
ALTER TABLE refund_requests ADD COLUMN IF NOT EXISTS payout_id UUID;

CREATE INDEX IF NOT EXISTS idx_refund_requests_order ON refund_requests(order_tracking_id);
CREATE INDEX IF NOT EXISTS idx_refund_requests_customer ON refund_requests(customer_tracking_id);
