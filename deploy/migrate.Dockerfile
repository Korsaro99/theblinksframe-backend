# The production migrations image: Liquibase with the changelog baked in, so each image runs
# exactly the migrations of the commit it was built from. Built by `make publish`:
#
#   docker build --platform linux/amd64 -f deploy/migrate.Dockerfile -t blinks-migrate db
#
# The build context is db/. Keep the two versions in step with db/liquibase.Dockerfile, which
# local development and the integration tests use (they mount the changelog instead).
FROM liquibase/liquibase:5.0.4-alpine
# Liquibase 5 no longer bundles JDBC drivers; add the Postgres one at build time.
RUN lpm add postgresql@42.7.13 --global
COPY changelog /liquibase/changelog
ENV LIQUIBASE_SEARCH_PATH=/liquibase/changelog \
    LIQUIBASE_COMMAND_CHANGELOG_FILE=changelog-master.yaml \
    LIQUIBASE_SHOW_BANNER=false \
    LIQUIBASE_ANALYTICS_ENABLED=false
