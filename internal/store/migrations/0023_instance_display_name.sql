-- An instance can be given a display name from the interface; NULL means the
-- interface labels it by address and port. The inventory never writes the
-- column, so a re-registration keeps it. Two instances may not share a display
-- name, compared without case.
ALTER TABLE instances ADD COLUMN IF NOT EXISTS display_name text;
CREATE UNIQUE INDEX IF NOT EXISTS instances_display_name_uniq ON instances (tenant_id, lower(display_name)) WHERE display_name IS NOT NULL;
