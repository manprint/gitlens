-- A cluster can be named from the interface. The name labels the cluster in
-- every view, so two clusters may not share one, compared without case. A
-- cluster without a name is not constrained: NULL means "use the default
-- label". The clusters table is a small metadata table, so the index is built
-- in place.
CREATE UNIQUE INDEX IF NOT EXISTS clusters_name_uniq ON clusters (tenant_id, lower(name)) WHERE name IS NOT NULL;
