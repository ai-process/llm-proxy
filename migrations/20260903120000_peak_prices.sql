-- Peak/off-peak vendor pricing. DeepSeek V4 doubles its rates for two windows a
-- day (UTC 01:00-04:00 and 06:00-10:00), and it now serves every low-effort
-- request, so a single price per model understates cost by up to 2x.
-- The existing columns keep their meaning: the standard (off-peak) rate.
ALTER TABLE model ADD COLUMN IF NOT EXISTS price_in_peak_per_mtok numeric NOT NULL DEFAULT 0;
ALTER TABLE model ADD COLUMN IF NOT EXISTS price_out_peak_per_mtok numeric NOT NULL DEFAULT 0;
