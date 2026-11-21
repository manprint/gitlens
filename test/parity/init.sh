#!/bin/sh
# First boot of the monitored PostgreSQL of test/parity/compose.yml: scratch
# database, two application users, extensions, and the read-only monitoring
# role (tier 1, so EXPLAIN commands run) with the log-reading grants.
set -eu
psql -v ON_ERROR_STOP=1 -U postgres -d postgres <<SQL
CREATE DATABASE parity;
CREATE ROLE app_a LOGIN PASSWORD '${PARITY_APP_PASSWORD}';
CREATE ROLE app_b LOGIN PASSWORD '${PARITY_APP_PASSWORD}';
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
SQL
psql -v ON_ERROR_STOP=1 -U postgres -d parity <<'SQL'
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
CREATE EXTENSION IF NOT EXISTS hypopg;
CREATE TABLE orders (id bigserial PRIMARY KEY, customer int NOT NULL, status text NOT NULL, note text);
INSERT INTO orders (customer, status, note)
  SELECT g % 5000, CASE WHEN g % 50 = 0 THEN 'open' ELSE 'done' END, md5(g::text)
  FROM generate_series(1, 300000) g;
ANALYZE orders;
CREATE TABLE churn (id bigserial PRIMARY KEY, payload text);
CREATE TABLE lock_a (id int PRIMARY KEY, v int);
CREATE TABLE lock_b (id int PRIMARY KEY, v int);
INSERT INTO lock_a VALUES (1, 0);
INSERT INTO lock_b VALUES (1, 0);
GRANT ALL ON ALL TABLES IN SCHEMA public TO app_a, app_b;
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO app_a, app_b;
SQL
for db in postgres parity; do
  psql -v ON_ERROR_STOP=1 -v pglens_password="${PGLENS_MONITORING_PASSWORD}" \
    -v role=pglens_monitor -v tier1=1 -v dbname="$db" \
    -U postgres -d "$db" -f /opt/pglens/monitoring_user.sql </dev/null
done
psql -v ON_ERROR_STOP=1 -U postgres -d postgres <<'SQL'
GRANT EXECUTE ON FUNCTION pg_read_binary_file(text, bigint, bigint, boolean) TO pglens_monitor;
GRANT EXECUTE ON FUNCTION pg_current_logfile(text) TO pglens_monitor;
GRANT EXECUTE ON FUNCTION pg_current_logfile() TO pglens_monitor;
SQL
