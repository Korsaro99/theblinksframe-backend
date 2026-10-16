#!/bin/sh
# Creates the API's own database login, which can read and write data but not change the
# schema (docs/SECURITY.md §4). Liquibase migrates as the owner, POSTGRES_USER.
#
# The postgres image runs files in /docker-entrypoint-initdb.d only once, when the data volume
# is empty. To run it on an existing database, see deploy/README.md, "Database logins".
set -eu

psql -v ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  -v app_user="$APP_DB_USER" -v app_password="$APP_DB_PASSWORD" -v owner="$POSTGRES_USER" <<'SQL'
CREATE ROLE :"app_user" LOGIN PASSWORD :'app_password';
GRANT USAGE ON SCHEMA public TO :"app_user";
-- Tables and sequences the owner creates from now on (every Liquibase migration) are
-- readable and writable by the API. No CREATE, ALTER, DROP or TRUNCATE.
ALTER DEFAULT PRIVILEGES FOR ROLE :"owner" IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO :"app_user";
ALTER DEFAULT PRIVILEGES FOR ROLE :"owner" IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO :"app_user";
SQL
