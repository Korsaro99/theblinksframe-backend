# The nightly backup job (deploy/README.md, "Backups"): pg_dump, then age, then rclone.
# Compose builds it on the VM the first time (`build:` in compose.yaml), so there is no image to
# publish.
#
# pg_dump must be the same major version as the server or newer, so keep postgresql18-client in
# step with the postgres image in compose.yaml. Alpine 3.24 ships 18.6, the same as the server.
# (Not FROM postgres:18.6-alpine: that image declares a data volume, and every recreated backup
# container would leave an empty one behind.)
FROM alpine:3.24.2
RUN apk add --no-cache postgresql18-client age rclone \
 && adduser -D -H -u 10001 backup
COPY --chmod=755 backup.sh /usr/local/bin/backup.sh
USER backup
ENTRYPOINT ["/usr/local/bin/backup.sh"]
CMD ["loop"]
