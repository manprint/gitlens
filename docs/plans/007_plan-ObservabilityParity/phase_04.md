# Phase 3 — Notification channels: Teams, PagerDuty, email

Intent: alerts are delivered to Microsoft Teams, PagerDuty and email, managed from Settings like the existing channels.
Prerequisites: P0.
Phase closure: P3; review by agent-1.

## State and ownership contract
Read STATE.md §1 first (and PROTOCOL.md once without this skill) for scope, ownership,
recovery, checks, and commit rules. Open before edits; checkpoint at a recovery boundary;
close with evidence. A delegated worker follows its assigned OPEN unit. Missing design goes
to agent-1, the supervisor. Executor power: low.

## Local design context
Plan revision: 1.
- D5: one address per kind. `notificationKind` (in `internal/server/notification_channels.go`) today has `Kind`, `url func(AlertConfig) string`, `build func(address string) notify.Channel`. This phase adds two optional fields:
  `validate func(address string) error` (nil → `validateWebhookURL`) and `host func(address string) string` (nil → current host extraction). `available func() error` (nil → always available).
- I-7: the API never returns an address; it returns `host`. For `pagerduty` `host` is `events.pagerduty.com`; for `email` it is the number of recipients as text, for example `3 recipients`; for `teams` the URL host. No address, routing key or SMTP password in any log line or error text.
- `notify.Message` fields: `DedupID, RuleID, Severity, Phase, Summary, Cluster, Instance, Database string; Value float64; At time.Time; Labels map[string]string; Detail *Detail`. Read `internal/alert/notify/slack.go` and `googlechat.go` first: copy how they turn `Message` and `Detail` into lines, and which `Phase` value means "resolved".
- R2 Teams body:
  ```json
  {"type":"message","attachments":[{"contentType":"application/vnd.microsoft.card.adaptive",
   "content":{"type":"AdaptiveCard","$schema":"http://adaptivecards.io/schemas/adaptive-card.json",
   "version":"1.4","body":[
     {"type":"TextBlock","text":"<title line>","weight":"Bolder","wrap":true},
     {"type":"TextBlock","text":"<summary>","wrap":true},
     {"type":"FactSet","facts":[{"title":"Cluster","value":"…"},{"title":"Instance","value":"…"},{"title":"Database","value":"…"},{"title":"Severity","value":"…"},{"title":"Time","value":"…"}]},
     {"type":"TextBlock","text":"<detail lines>","wrap":true,"fontType":"Monospace"}]}}]}
  ```
  Facts with an empty value are left out; the last block is left out when there is no detail. The title line and fact titles use the same localized texts as the Google Chat channel.
- R1 PagerDuty: `POST https://events.pagerduty.com/v2/enqueue`, body
  `{"routing_key":K,"event_action":"trigger"|"resolve","dedup_key":DedupID,"payload":{"summary":S,"source":SRC,"severity":SEV,"timestamp":RFC3339,"component":"postgresql","group":Cluster,"class":RuleID,"custom_details":{…}}}`.
  `S` = `Summary` cut to 1024 characters. `SRC` = `Instance`, else `Cluster`, else `pglens`. `SEV`: `critical`→`critical`, `warning`→`warning`, anything else→`info`. A resolved message sends `event_action: "resolve"` with only `routing_key`, `event_action`, `dedup_key`. `custom_details` holds `database`, `value`, the labels, and the detail lines. Routing key format: `^[A-Za-z0-9]{32}$`. Any 2xx is success. The endpoint URL is a package variable so tests can point it at `httptest`.
- Email: environment `PGLENS_SMTP_HOST` (required to enable), `PGLENS_SMTP_PORT` (default `587`), `PGLENS_SMTP_USERNAME`, `PGLENS_SMTP_PASSWORD` (both or neither), `PGLENS_SMTP_FROM` (required, one address), `PGLENS_SMTP_TLS` = `starttls` (default) | `tls` | `none`. Address = comma-separated recipients, 1 to 20, parsed with `net/mail.ParseAddressList`. Message: `From`, `To`, `Subject: [pglens] <SEVERITY> <summary cut to 120>` (subject encoded with `mime.QEncoding`), `Date`, `Message-ID: <DedupID.unixnano@pglens>`, `MIME-Version: 1.0`, `Content-Type: text/plain; charset=UTF-8`, `Content-Transfer-Encoding: 8bit`; body = the same lines as the Slack text, CRLF line ends, no line over 998 bytes. Header values must have CR and LF removed before use. Send with `net/smtp`: dial with a 10 s timeout; `tls` → `tls.Dial` then `smtp.NewClient`; `starttls` → `StartTLS` is mandatory (fail if the server does not offer it); `none` → plain and no AUTH unless the host is `localhost` or `127.0.0.1`. TLS `ServerName` is the host; `MinVersion: tls.VersionTLS12`.

## Sub-phases

### 3.1 Kind hooks and the Teams channel
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/alert/notify/teams.go`, `teams_test.go`; WRITE `internal/server/notification_channels.go` (`notificationKind`, `notificationKinds`, `handleSetNotificationChannel`, the list handler, `lookupNotificationKind`), `internal/server/config.go` (`AlertConfig.TeamsURL`, `LoadAlertConfig` reading `PGLENS_TEAMS_WEBHOOK_URL` like the other URLs), `cmd/pglens-server/channels.go` (`buildChannels`, `wireNotificationChannels`), tests of those files.
- **Change:**
  Preconditions: P0 DONE.
  Contract: `notify.NewTeams(url string, client *http.Client) notify.Channel`; `Name()` returns `teams`; `Send` posts the R2 body with `sendJSON`. The kind table gets `teams`. The set handler calls `kind.validate` when present, else `validateWebhookURL`; the list handler calls `kind.host` when present; a kind whose `available()` returns an error answers 409 with that error code.
  Steps: 1. S1 — channel and its tests. 2. S2 — the three optional fields and their call sites, no behavior change for existing kinds. 3. S3 — register `teams` in kinds, config, `buildChannels`.
- **Unit tests:** `TestNCH001` (body equals the golden JSON for a firing critical message with detail), `TestNCH002` (no detail → three body blocks), `TestNCH003` (non-2xx → error that does not contain the URL), `TestNCH004` (`PUT …/teams` with a valid https URL stores and swaps; with `http://` → 400 as for other webhooks), `TestNCH005` (list response for `teams` has `host` and no `url`; guards I-7). Gate G-GO-FOCUS `./internal/alert/notify/ ./internal/server/ ./cmd/pglens-server/`.
- **e2e tests:** `TestINTNCH001` (`./cmd/pglens-server/` or the existing channel integration file): a stored `teams` channel receives a firing message on an `httptest` server. Gate G-GO-INT.
- **Done:** tests pass; existing channel tests unchanged and green; reviewed; unit closed and committed.

### 3.2 PagerDuty channel
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/alert/notify/pagerduty.go`, `pagerduty_test.go`; WRITE `internal/server/notification_channels.go` (kind `pagerduty` with `validate`, `host`), `internal/server/config.go` (`AlertConfig.PagerDutyRoutingKey` from `PGLENS_PAGERDUTY_ROUTING_KEY`), `cmd/pglens-server/channels.go`, tests.
- **Change:**
  Preconditions: 3.1 DONE.
  Contract: `notify.NewPagerDuty(routingKey string, client *http.Client) notify.Channel`; `Name()` `pagerduty`; body per Local design context. `validate`: the regular expression above, error text `routing key must be 32 letters or digits` (never echoes the value). `host`: constant `events.pagerduty.com`. An invalid key in the environment variable is a start-up error.
  Steps: 1. S1 — channel with package variable `pagerDutyEndpoint`. 2. S2 — kind, config, wiring.
- **Unit tests:** `TestNCH010` (firing → `trigger` body equals golden; `dedup_key` = `DedupID`), `TestNCH011` (resolved → `resolve` body has exactly three keys), `TestNCH012` (severity mapping for `critical`, `warning`, `info`, `other`), `TestNCH013` (summary of 2000 characters is cut to 1024), `TestNCH014` (`PUT …/pagerduty` with a 31-character key → 400 and the response and captured log do not contain the key; a valid key → list shows `host: events.pagerduty.com`; guards I-7). Gate G-GO-FOCUS.
- **e2e tests:** N/A — the endpoint is external; the `httptest` unit tests cover the wire format.
- **Done:** tests pass; reviewed; unit closed and committed.

### 3.3 Email channel
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** NEW `internal/alert/notify/email.go`, `email_test.go`; WRITE `internal/server/config.go` (new `SMTPConfig` struct and `LoadSMTPConfig() (SMTPConfig, error)`), `internal/server/notification_channels.go` (kind `email` with `validate`, `host`, `available`), `cmd/pglens-server/main.go` and `channels.go` (load and pass the SMTP config), tests.
- **Change:**
  Preconditions: 3.2 DONE.
  Contract: `notify.NewEmail(cfg notify.SMTPSettings, recipients []string) notify.Channel`; `Name()` `email`. `available()` returns code `smtp_not_configured` when `PGLENS_SMTP_HOST` is empty. `validate` parses the recipient list (1–20 addresses, else `between 1 and 20 recipients are required`). `LoadSMTPConfig` errors: missing `FROM` when host is set; username without password or the reverse; port not 1–65535; TLS value outside the three. Message and transport per Local design context. No environment variable sets the recipients.
  Steps: 1. S1 — message builder `buildEmail(cfg, recipients, m) []byte` (pure function). 2. S2 — transport with an injectable dial function for tests. 3. S3 — config, kind, wiring.
- **Unit tests:** `TestNCH020` (`buildEmail` output equals golden; CRLF line ends), `TestNCH021` (a summary containing `\r\nBcc: x@y` cannot add a header: the produced header block has exactly the expected header names), `TestNCH022` (`starttls` against a fake server without STARTTLS fails; `none` to a non-loopback host with a username fails before AUTH), `TestNCH023` (`LoadSMTPConfig` table: each error case), `TestNCH024` (`PUT …/email` without SMTP host → 409 `smtp_not_configured`; with 21 recipients → 400; list shows `host: "2 recipients"`; the password appears in no response or log; guards I-7). Gate G-GO-FOCUS.
- **e2e tests:** `TestINTNCH002`: a minimal in-process SMTP listener (plain, loopback, TLS `none`) receives one message whose `Subject` starts with `[pglens] CRITICAL`. Gate G-GO-INT (no Docker needed; keep it under the `integration` tag).
- **Done:** tests pass; reviewed; unit closed and committed.

### 3.4 OpenAPI, console, help, README
- **Agent:** agent-2
- **Assignment:** implement; review after diff by agent-1.
- **Files:** WRITE `api/openapi.yaml` (`NotificationChannelKind` enum adds `teams`, `pagerduty`, `email`; the set body documents that `url` carries the address for every kind; 409 response), G-API outputs; `web/src/lib/notificationChannels.ts` (`NOTIFICATION_CHANNELS`, a per-kind address check next to `webhookUrlProblem`), `web/src/features/settings/NotificationsPanel.tsx`, `web/src/i18n/messages/{en,it}/settings.ts`, help topic files on notifications in `web/src/lib/help/topics/{en,it}/`, tests; `README.md`, `docs/api.md` (generated).
- **Change:**
  Preconditions: 3.3 DONE.
  Contract: the panel lists six kinds. Per kind: field label (`Webhook URL`, `Routing key`, `Recipients`), input type (`password`-style masking for the routing key), client check mirroring the server rule, and instructions text (Teams: create a "Post to a channel when a webhook request is received" workflow and paste its URL; PagerDuty: Events API v2 integration key of the service; email: comma-separated addresses, SMTP is configured on the server with `PGLENS_SMTP_*`). A 409 `smtp_not_configured` shows the text `Email needs the SMTP settings on the server (PGLENS_SMTP_HOST, PGLENS_SMTP_FROM).` The test-message button works for all kinds.
  Steps: 1. S1 — OpenAPI and G-API. 2. S2 — lib and panel. 3. S3 — help (`en`, `it`) and README: a table of the six kinds, their address and environment variables.
- **Unit tests:** `UI-NCH-001` (six kinds render with their labels in `en` and `it`), `UI-NCH-002` (routing key of 31 characters shows the problem text and disables Save), `UI-NCH-003` (21 recipients → problem text), `UI-NCH-004` (409 → the SMTP text), `UI-NCH-005` (the routing key input is masked). Gates G-WEB-STATIC, G-WEB-UNIT, G-API.
- **e2e tests:** N/A at unit level; lab check in 15.2.
- **Done:** tests pass; README and help updated; reviewed; unit closed and committed.

## Phase gates and closure
- Required gates: G-GO-FULL, G-GO-INT (`TestINTNCH00[12]`), G-API, G-WEB-FULL, G-I18N; assertions: I-7 tests green; no new Go module in `go.mod`.
- README obligation: unit 3.4.
- Every sub-phase DONE or explicitly justified SKIPPED. Run full phase gates now.
- agent-1 reviews integration, I-5, I-7, header-injection handling, documentation.
- Open closure unit P3, record review and result, mark the phase DONE, close, commit `phase(007/P3): PHASE 3 COMPLETE — Notification channels; gates and review passed`.
- P3 edits no production code or tests. Blocked gate: P3 stays OPEN and BLOCKED. Failing gate or rejected review: attempt failed; reopen the responsible sub-phase, then rerun P3.
