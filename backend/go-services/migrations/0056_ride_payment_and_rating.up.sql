-- Migration: 0056_ride_payment_and_rating.up.sql
-- Description: Adds payment_method, rating, and tip columns to rides table.

ALTER TABLE rides ADD COLUMN IF NOT EXISTS payment_method VARCHAR(30) DEFAULT 'cash';
ALTER TABLE rides ADD COLUMN IF NOT EXISTS rating NUMERIC(3,2);
ALTER TABLE rides ADD COLUMN IF NOT EXISTS rating_comment TEXT;
ALTER TABLE rides ADD COLUMN IF NOT EXISTS tip_amount NUMERIC(10,2) DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_rides_payment_method ON rides(payment_method);
