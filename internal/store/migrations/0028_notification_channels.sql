-- Webhook addresses set from the console (11.5b, D18, Q2). One row per tenant
-- and channel kind; a kind without a row uses the address of the server
-- environment, if any, and a row overrides it. The address is write-only: the
-- API never returns it and the server never logs it (I-10). It is stored
-- without encryption at rest, so a dump or backup of the database holds it
-- (docs/LIMITS.md). updated_by names the credential that set it, as the
-- retention log does.
CREATE TABLE IF NOT EXISTS notification_channels (
  tenant_id text NOT NULL DEFAULT 'default',
  kind text NOT NULL CHECK (kind IN ('slack', 'webhook', 'google_chat')),
  url text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by text NOT NULL,
  PRIMARY KEY (tenant_id, kind)
);
