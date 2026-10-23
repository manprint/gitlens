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
# The script covers one database per run, and PUBLIC's large-object and WAL
# message grants are per database. The role can connect to every database
# that accepts connections, so the script runs in each of them. A listing that
# fails, or that names no database, stops here: a loop over nothing would
# exit 0 and leave the cluster without the role.
#
# harden_public=1 revokes the PUBLIC write grants too, not only the one the
# script always revokes. The cluster is fresh and its storage role is a
# superuser, which PUBLIC grants do not restrict, so nothing here depends on
# them.
set -e
dbs=$(psql -X -At -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  -c "SELECT datname FROM pg_database WHERE datallowconn ORDER BY datname") || {
  echo "init_monitoring_user.sh: cannot list the databases of the cluster" >&2
  exit 1
}
if [ -z "$dbs" ]; then
  echo "init_monitoring_user.sh: no database accepts connections" >&2
  exit 1
fi
# The names are read from a here-document, not from a pipe, so the loop runs
# in this shell and a failed run stops the script. Each name is read whole,
# spaces included.
while IFS= read -r db; do
  psql -v ON_ERROR_STOP=1 \
    -v pglens_password="${PGLENS_MONITORING_PASSWORD:?set PGLENS_MONITORING_PASSWORD}" \
    -v role=pglens_monitor \
    -v harden_public=1 \
    -v dbname="$db" \
    --username "$POSTGRES_USER" --dbname "$db" \
    -f /opt/pglens/monitoring_user.sql </dev/null
done <<EOL
$dbs
EOL
