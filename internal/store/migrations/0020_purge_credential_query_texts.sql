-- Statement texts stored before the agent withheld credentials can still hold
-- the PASSWORD literal of role DDL, the password option of a mapping, or the
-- password of a connection string, whole or in part. The agent now withholds a
-- text with any sign of a credential before it ships it, but the upsert keeps
-- the first text stored for a queryid, and a text that was shipped once is not
-- sent again until the agent's resend interval has passed. A statement evicted
-- from pg_stat_statements is never sent again, so its old row would keep the
-- secret for good. The rows are deleted rather than rewritten: a live
-- statement's text is shipped again within that interval, already withheld.
--
-- The match is the agent's, made at least as broad. A credential word is
-- looked for in the text's letters and digits alone, so a word split by
-- quotes, || or blanks is found; a libpq URI scheme in its letters, digits,
-- colons and slashes; and a text with any backslash is deleted, since an
-- escape can spell either. An over-deletion costs only a short gap in the
-- displayed text.
DELETE FROM query_texts
WHERE regexp_replace(lower(query_text), '[^a-z0-9]', '', 'g') ~ '(password|passwd|secret|uescape)'
   OR regexp_replace(lower(query_text), '[^a-z0-9:/]', '', 'g') ~ 'postgres(ql)?:/'
   OR strpos(query_text, chr(92)) > 0;
