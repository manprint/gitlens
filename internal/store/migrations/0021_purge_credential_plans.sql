-- Explain plans and command errors read from a monitored database can carry a
-- credential its statement text does not show: a plan shows the constants of
-- the views and functions the planner expanded (a view over dblink shows its
-- connection string), and an error can quote the statement it ran. The agent
-- now withholds a plan or an error with any sign of a credential, by the rule
-- of the statement texts (see 0020). Plans and errors stored before that are
-- removed by the same rule.
--
-- A plan is matched on its JSON text without the backslash rule of 0020: JSON
-- escapes every quoted identifier with one, and the planner prints each
-- constant decoded, so no escape is left to spell a word. A stored plan is
-- deleted, a command's result is cleared, and a command's error and an audit
-- entry's detail are replaced by the text the agent now sends in their place.
-- The audit is written after its insert only here.
DELETE FROM query_plans
WHERE regexp_replace(lower(plan::text), '[^a-z0-9]', '', 'g') ~ '(password|passwd|secret|uescape)'
   OR regexp_replace(lower(plan::text), '[^a-z0-9:/]', '', 'g') ~ 'postgres(ql)?:/';

UPDATE commands SET result = NULL
WHERE result IS NOT NULL
  AND (regexp_replace(lower(result::text), '[^a-z0-9]', '', 'g') ~ '(password|passwd|secret|uescape)'
    OR regexp_replace(lower(result::text), '[^a-z0-9:/]', '', 'g') ~ 'postgres(ql)?:/');

UPDATE commands SET error = 'the error text is withheld because it may hold a credential'
WHERE regexp_replace(lower(error), '[^a-z0-9]', '', 'g') ~ '(password|passwd|secret|uescape)'
   OR regexp_replace(lower(error), '[^a-z0-9:/]', '', 'g') ~ 'postgres(ql)?:/'
   OR strpos(error, chr(92)) > 0;

UPDATE command_audit SET detail = 'the error text is withheld because it may hold a credential'
WHERE regexp_replace(lower(detail), '[^a-z0-9]', '', 'g') ~ '(password|passwd|secret|uescape)'
   OR regexp_replace(lower(detail), '[^a-z0-9:/]', '', 'g') ~ 'postgres(ql)?:/'
   OR strpos(detail, chr(92)) > 0;
