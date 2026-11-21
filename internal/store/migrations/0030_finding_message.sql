-- Plan 006 F5: a finding's text as catalog key and arguments, so the API can
-- render it in the console's language. NULL for findings written before.
ALTER TABLE findings ADD COLUMN IF NOT EXISTS message jsonb;
