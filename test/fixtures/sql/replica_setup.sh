#!/bin/bash
set -e

PGDATA="${PGDATA:-/var/lib/postgresql/data}"

# Wait for PostgreSQL to be ready
until pg_isready -U postgres; do
  echo "Waiting for PostgreSQL..."
  sleep 1
done

# Allow the standby container to open a replication connection. The default
# pg_hba.conf this image generates has no "replication" entry for anything
# but the local Unix socket, so pg_basebackup from pg-standby fails with
# "no pg_hba.conf entry for replication connection" without this.
echo "host replication all all trust" >>"$PGDATA/pg_hba.conf"
psql -U postgres -d postgres -c "SELECT pg_reload_conf();"

# Create replication slot
psql -U postgres -d postgres <<SQL
SELECT * FROM pg_create_physical_replication_slot('standby1', true);
SQL

echo "Replication slot created and primary ready for standby"
