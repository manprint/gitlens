-- The dedup index of metrics_replication predates the source column (0014),
-- so its key left source out. A sender row and a slot row for the same
-- instance, upstream, slot and timestamp then collided, and the ON CONFLICT
-- DO NOTHING of the writer dropped the second one without a word. source
-- joins the key. COALESCE keeps the rows written before 0014, whose source is
-- NULL, from being unique among themselves: NULLs never collide in a unique
-- index, so the empty string stands in for them.
--
-- A key with one more column collides less often, so no set of rows that the
-- old index accepted can violate the new one. The index is replaced inside
-- this migration's transaction, so the table never runs without its dedup.
DROP INDEX IF EXISTS metrics_replication_dedup_idx;
CREATE UNIQUE INDEX metrics_replication_dedup_idx ON metrics_replication
  (instance_id, COALESCE(upstream_id, '00000000-0000-0000-0000-000000000000'::uuid),
   slot_name, COALESCE(source, ''), ts);
