#!/bin/sh
# Creates the backup service's database login, which can read everything and change nothing
# (deploy/README.md, "Backups"). pg_dump only needs to read, so the one container that has both
# database access and a route to the internet never holds the owner's password.
#
# pg_read_all_data is a role built into Postgres (14 and later): SELECT on every table and
# sequence, now and in the future, without listing them.
#
# The postgres image runs files in /docker-entrypoint-initdb.d only once, when the data volume
# is empty. To run it on an existing database, see deploy/README.md, "Backups".
set -eu

psql -v ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  -v backup_user="$BACKUP_DB_USER" -v backup_password="$BACKUP_DB_PASSWORD" <<'SQL'
CREATE ROLE :"backup_user" LOGIN PASSWORD :'backup_password';
GRANT pg_read_all_data TO :"backup_user";
SQL
