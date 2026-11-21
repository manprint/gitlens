-- Notification channels Teams, PagerDuty and email (plan 007 phase 3): the
-- kinds a stored address or a channel language may name.
ALTER TABLE notification_channels DROP CONSTRAINT IF EXISTS notification_channels_kind_check;
ALTER TABLE notification_channels ADD CONSTRAINT notification_channels_kind_check
  CHECK (kind IN ('slack', 'webhook', 'google_chat', 'teams', 'pagerduty', 'email'));
ALTER TABLE notification_channel_languages DROP CONSTRAINT IF EXISTS notification_channel_languages_kind_check;
ALTER TABLE notification_channel_languages ADD CONSTRAINT notification_channel_languages_kind_check
  CHECK (kind IN ('slack', 'webhook', 'google_chat', 'teams', 'pagerduty', 'email'));
