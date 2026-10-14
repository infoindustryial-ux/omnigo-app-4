-- Migration: 0057_create_ledger_idempotency_table.down.sql
-- Description: Reverts creation of ledger_idempotency table.

DROP TABLE IF EXISTS ledger_idempotency CASCADE;
