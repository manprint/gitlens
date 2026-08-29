#!/bin/bash
set -e

PGDATA="${PGDATA:-/var/lib/postgresql/data}"
UPSTREAM_HOST="${UPSTREAM_HOST:?UPSTREAM_HOST is required}"
REPLICATION_SLOT="${REPLICATION_SLOT:?REPLICATION_SLOT is required}"
PGAPPNAME="${PGAPPNAME:?PGAPPNAME is required}"

if [ ! -f "$PGDATA/PG_VERSION" ]; then
  retries=60
  until pg_isready -h "$UPSTREAM_HOST" -U postgres -d postgres; do
    retries=$((retries - 1))
    if [ "$retries" -le 0 ]; then
      echo "upstream $UPSTREAM_HOST did not become ready"
      exit 1
    fi
    sleep 1
  done

  PGPASSWORD=postgres psql -h "$UPSTREAM_HOST" -U postgres -d postgres -v ON_ERROR_STOP=1 -c \
    "SELECT pg_create_physical_replication_slot('$REPLICATION_SLOT') WHERE NOT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name='$REPLICATION_SLOT')" \
    || true
  PGAPPNAME="$PGAPPNAME" PGPASSWORD=postgres pg_basebackup \
    -h "$UPSTREAM_HOST" -U postgres -S "$REPLICATION_SLOT" -D "$PGDATA" -Fp -Xs -R
  chmod 700 "$PGDATA"
  chown -R postgres:postgres "$PGDATA"
fi

exec gosu postgres postgres
