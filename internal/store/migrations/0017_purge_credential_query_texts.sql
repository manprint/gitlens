-- Statement texts stored before the agent redacted credentials still hold the
-- PASSWORD literal of role DDL, the password option of a mapping and the
-- password of a connection string. The agent now redacts before it ships a
-- text, but the upsert keeps the first text stored for a queryid, and a text
-- that was shipped once is not sent again until the agent's resend interval
-- has passed. A statement evicted from pg_stat_statements is never sent again,
-- so its old row would keep the secret for good. The rows are deleted rather
-- than rewritten: a live statement's text is shipped again within that
-- interval, already redacted. The match is deliberately broad, so an
-- over-deletion costs only a short gap in the displayed text.
DELETE FROM query_texts
WHERE query_text ~* 'password'
   OR query_text ~* '://[^[:space:]/@]+:[^[:space:]/@]+@';
