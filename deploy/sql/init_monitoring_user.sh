#!/bin/sh
# Creates the example stack's read-only monitoring role, pglens_monitor, by
# running the shipped deploy/sql/monitoring_user.sql. deploy/agent.example.yaml
# connects as that role, not as the superuser that owns the pglens storage.
#
# docker-entrypoint-initdb.d runs .sql files with a plain `psql -f`, which
# never supplies the psql variables the script needs, so this wrapper passes
# them. It is mounted next to the file by deploy/docker-compose.yml; the
# script itself is not copied.
set -e
psql -v ON_ERROR_STOP=1 \
  -v pglens_password="${PGLENS_MONITORING_PASSWORD:?set PGLENS_MONITORING_PASSWORD}" \
  -v role=pglens_monitor \
  -v dbname="$POSTGRES_DB" \
  --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  -f /opt/pglens/monitoring_user.sql
