-- Migration: 0057_create_ledger_idempotency_table.up.sql
-- Description: Creates the missing ledger_idempotency table required by internal/ledger/service.go
-- to ensure atomic idempotency deduplication across all financial transfers.

CREATE TABLE IF NOT EXISTS ledger_idempotency (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_idempotency_created_at ON ledger_idempotency(created_at);
