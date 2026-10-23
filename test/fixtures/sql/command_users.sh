#!/bin/sh
# Command-channel roles used only by the primary/standby E2E topology. The
# agent fixture assigns T1 to the primary and T2 to the standby so the UI
# acceptance suite can exercise both command permission boundaries. Both
# roles come from the shipped deploy/sql/monitoring_user.sql (mounted at
# /opt/pglens by topo-primary-standby.yml), as an operator would make them,
# so they are as read-only as the role users get.
set -e
monitoring_role() {
  psql -v ON_ERROR_STOP=1 -v pglens_password=pglens-monitoring-test \
    -v dbname="$POSTGRES_DB" \
    --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
    "$@" -f /opt/pglens/monitoring_user.sql
}
monitoring_role -v role=pglens_t1 -v tier1=1
monitoring_role -v role=pglens_t2 -v tier1=1 -v tier2=1

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  -c 'CREATE EXTENSION IF NOT EXISTS pgstattuple'
