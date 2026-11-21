-- Plan 006 F7: the language each notification channel writes its messages in,
-- set from the console. No row means the server default (PGLENS_LANG). A row
-- needs no stored address: a channel of the environment has a language too.
CREATE TABLE IF NOT EXISTS notification_channel_languages (
  tenant_id text NOT NULL DEFAULT 'default',
  kind text NOT NULL CHECK (kind IN ('slack', 'webhook', 'google_chat')),
  language text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by text NOT NULL,
  PRIMARY KEY (tenant_id, kind)
);
