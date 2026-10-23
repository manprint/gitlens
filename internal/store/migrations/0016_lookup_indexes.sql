-- Two lookups scanned far more rows than they needed, and neither had an index
-- led by the columns it binds. Both use plain CREATE INDEX, as 0015 does for
-- events_ts_idx, because the migration runs inside one transaction.

-- The slots list reports, for each inactive slot, its last active reading. The
-- lookup binds the tenant, the instance, the slot and the activity, and orders
-- by time. Without an index led by those columns, each inactive slot walked
-- its history back to the last active row.
CREATE INDEX IF NOT EXISTS metrics_replication_slot_active_idx
  ON metrics_replication (tenant_id, instance_id, slot_name, slot_active, ts DESC);

-- The ASH top list joins query_texts once per top entry on (cluster, queryid).
-- The primary key leads with datname, which that join never binds, so each
-- entry scanned the whole text table of its cluster.
CREATE INDEX IF NOT EXISTS query_texts_cluster_queryid_idx
  ON query_texts (tenant_id, cluster_id, queryid);
