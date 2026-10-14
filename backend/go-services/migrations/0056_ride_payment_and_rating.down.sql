-- Migration: 0056_ride_payment_and_rating.down.sql

DROP INDEX IF EXISTS idx_rides_payment_method;
ALTER TABLE rides DROP COLUMN IF EXISTS tip_amount;
ALTER TABLE rides DROP COLUMN IF EXISTS rating_comment;
ALTER TABLE rides DROP COLUMN IF EXISTS rating;
ALTER TABLE rides DROP COLUMN IF EXISTS payment_method;
