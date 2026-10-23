#!/bin/sh
# Creates the example stack's read-only monitoring role, pglens_monitor, by
# running the shipped deploy/sql/monitoring_user.sql in every database of the
# cluster. deploy/agent.example.yaml connects as that role, not as the
# superuser that owns the pglens storage.
#
# docker-entrypoint-initdb.d runs .sql files with a plain `psql -f`, which
# never supplies the psql variables the script needs, so this wrapper passes
# them. It is mounted next to the file by deploy/docker-compose.yml; the
# script itself is not copied.
#
# The script covers one database per run, and PUBLIC's large-object grant is
# per database. The role can connect to every database that accepts
# connections, so the script runs in each of them.
#
# harden_public=1 revokes the PUBLIC write grants too, not only the one the
# script always revokes. The cluster is fresh and its storage role is a
# superuser, which PUBLIC grants do not restrict, so nothing here depends on
# them.
set -e
for db in $(psql -X -At --username "$POSTGRES_USER" --dbname postgres \
  -c "SELECT datname FROM pg_database WHERE datallowconn ORDER BY datname"); do
  psql -v ON_ERROR_STOP=1 \
    -v pglens_password="${PGLENS_MONITORING_PASSWORD:?set PGLENS_MONITORING_PASSWORD}" \
    -v role=pglens_monitor \
    -v harden_public=1 \
    -v dbname="$db" \
    --username "$POSTGRES_USER" --dbname "$db" \
    -f /opt/pglens/monitoring_user.sql
done
