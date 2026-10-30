#!/bin/sh
# The restore drill (deploy/README.md, "Restore drill"). Run it on your Mac, where the private
# age key is, never on the VM:
#
#   deploy/backup/restore-drill.sh <downloaded .dump.age file> <age key file>
#
# It decrypts the backup, starts a throwaway Postgres with the same version and the same
# first-start scripts as production, restores into it in one transaction (all or nothing), then
# prints the last migration, the rows in every table, and checks the API's login can read them.
# The container and the decrypted copy are deleted at the end. KEEP=1 leaves the container
# running so you can look around (docker exec -it blinks-restore-drill psql -U blinks blinks).
set -eu

if [ $# -ne 2 ]; then
  echo "usage: $0 <backup.dump.age> <age key file>" >&2
  exit 2
fi
backup=$1
key=$2
command -v age >/dev/null || { echo "age is not installed: brew install age" >&2; exit 1; }

deploy=$(cd "$(dirname "$0")/.." && pwd)
name=blinks-restore-drill
# Keep in step with the postgres image in compose.yaml.
image=postgres:18.6-alpine
tmp=$(mktemp -d)

cleanup() {
  rm -rf "$tmp"
  if [ "${KEEP:-}" != 1 ]; then docker rm -f -v "$name" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

echo "Decrypting $backup"
age --decrypt --identity "$key" --output "$tmp/blinks.dump" "$backup"

echo "Starting a throwaway Postgres ($name, no ports published)"
docker rm -f -v "$name" >/dev/null 2>&1 || true
docker run -d --name "$name" \
  -e POSTGRES_DB=blinks -e POSTGRES_USER=blinks -e POSTGRES_PASSWORD=drill \
  -e APP_DB_USER=blinks_app -e APP_DB_PASSWORD=drill \
  -e BACKUP_DB_USER=blinks_backup -e BACKUP_DB_PASSWORD=drill \
  -v "$deploy/postgres-init:/docker-entrypoint-initdb.d:ro" \
  "$image" >/dev/null
# Over TCP: during its first start the image runs a temporary server on a socket only, so this
# waits for the real one.
tries=0
until docker exec "$name" pg_isready -q -h 127.0.0.1 -U blinks -d blinks; do
  tries=$((tries + 1))
  [ "$tries" -lt 60 ] || { docker logs "$name" >&2; echo "Postgres did not start" >&2; exit 1; }
  sleep 1
done

echo "Restoring"
docker cp "$tmp/blinks.dump" "$name:/tmp/blinks.dump"
rm -f "$tmp/blinks.dump"
docker exec "$name" pg_restore -U blinks -d blinks --exit-on-error --single-transaction /tmp/blinks.dump
docker exec "$name" rm -f /tmp/blinks.dump

docker exec -i "$name" psql -X -q -v ON_ERROR_STOP=1 -U blinks -d blinks <<'SQL'
\echo
\echo 'Last migration applied:'
SELECT id, author, dateexecuted FROM databasechangelog ORDER BY orderexecuted DESC LIMIT 1;
\echo 'Rows per table:'
SELECT string_agg(format('SELECT %L AS "table", count(*) AS "rows" FROM public.%I', tablename, tablename), ' UNION ALL ' ORDER BY tablename)
FROM pg_tables WHERE schemaname = 'public' \gexec
\echo 'The API login (blinks_app) can read the restored tables:'
SET ROLE blinks_app;
SELECT count(*) AS staff_users FROM staff_users;
SQL

echo "Restore drill passed."
