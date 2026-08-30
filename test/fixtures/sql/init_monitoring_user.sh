#!/bin/sh
# Invokes the shipped deploy/sql/monitoring_user.sql (bind-mounted alongside
# this script, see test/compose/topo-standalone.yml) with its psql
# variables — docker-entrypoint-initdb.d runs .sql files with a
# plain `psql -f`, which never supplies that variable, so the real script
# needs this wrapper to run as an init script at all. This is not a copy of
# monitoring_user.sql (STATE.md §10 forbids that): it is an invocation of the
# exact shipped file with the one parameter automated init can't otherwise
# provide.
set -e
psql -v ON_ERROR_STOP=1 -v pglens_password="${PGLENS_MONITORING_PASSWORD:-pglens-monitoring-test}" \
  -v dbname="$POSTGRES_DB" \
  --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  -f /opt/pglens/monitoring_user.sql
