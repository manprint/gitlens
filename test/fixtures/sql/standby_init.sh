#!/bin/bash
set -e

PGDATA="${PGDATA:-/var/lib/postgresql/data}"

# If PGDATA is empty, bootstrap from primary via pg_basebackup
if [ ! -f "$PGDATA/PG_VERSION" ]; then
  echo "Initializing standby from primary..."

  # Wait for primary to be ready
  RETRIES=30
  while ! pg_isready -h pg-primary -U postgres -d postgres 2>/dev/null; do
    RETRIES=$((RETRIES - 1))
    if [ $RETRIES -le 0 ]; then
      echo "Primary did not become ready in time"
      exit 1
    fi
    sleep 1
  done

  # Create replication slot on primary if needed
  psql -h pg-primary -U postgres -d postgres -c \
    "SELECT * FROM pg_create_physical_replication_slot('standby1', true);" 2>/dev/null || true

  # Use pg_basebackup to initialize the standby. application_name is set via
  # PGAPPNAME (pg_basebackup's -c/--checkpoint only accepts fast|spread — an
  # earlier version of this script passed the application_name SET statement
  # to -c by mistake, which pg_basebackup rejected outright).
  PGAPPNAME=pg-standby pg_basebackup -h pg-primary -U postgres -S standby1 -D "$PGDATA" -Fp -Xs -R

  # Fix permissions: this whole script runs as root (it replaces the base
  # image's own entrypoint, which normally does this before ever touching
  # postgres — see topo-primary-standby.yml), so pg_basebackup's output is
  # root-owned until this chown.
  chmod 700 "$PGDATA"
  chown -R postgres:postgres "$PGDATA"

  echo "Standby initialized from primary"
else
  echo "PGDATA already exists, skipping bootstrap"
fi

# Start PostgreSQL as the unprivileged postgres user via gosu (bundled in
# the base image) — postgres refuses outright to run as root, and nothing
# upstream of this custom entrypoint does that switch for us.
exec gosu postgres postgres
