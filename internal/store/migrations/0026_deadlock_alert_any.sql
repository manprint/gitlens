-- db.deadlock_rate fires on any deadlock.
--
-- 0006 seeded it as "rate above 0.1 per second for 300 seconds", which a real
-- deadlock never reaches. pg_deadlocks_total is stored as a per-second rate
-- over one scrape interval (30 seconds by default), so a single deadlock reads
-- as about 0.033 per second, below 0.1. And a pending episode fires only on a
-- pass that still sees a sample above the threshold 300 seconds after the
-- first, while the alert source keeps a deadlock for two minutes only.
-- Deadlocks arrive one or two at a time, so the rule never fired on them.
--
-- A deadlock is an application bug worth looking at even when it happens
-- once: the default now fires on any positive rate, at once. The alert source
-- keeps the newest deadlock of its lookback window (two minutes at the default
-- evaluation interval); once none is left, the engine holds the episode for
-- its metric grace (ten minutes) before it resolves it. The alert therefore
-- resolves about twelve minutes after the last deadlock, and deadlocks that
-- recur within that hold extend one episode instead of notifying again.
--
-- Scoped to the exact seeded values so a threshold, comparator or duration an
-- operator set through the API is preserved, and idempotent on re-run.
UPDATE alert_rules
   SET threshold = 0,
       for_seconds = 0,
       updated_at = now()
 WHERE rule_id = 'db.deadlock_rate'
   AND comparator = 'gt'
   AND threshold = 0.1
   AND for_seconds = 300;
