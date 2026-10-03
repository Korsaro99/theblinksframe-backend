# Liquibase 5 no longer bundles JDBC drivers; add the Postgres one at build time.
FROM liquibase/liquibase:5.0.4-alpine
RUN lpm add postgresql@42.7.13 --global
