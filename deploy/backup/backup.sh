#!/bin/sh
# Daily database backup (deploy/README.md, "Backups").
#
#   backup.sh loop     the container's normal job: wait for BACKUP_TIME (UTC), back up, repeat
#   backup.sh once     one backup now: sudo docker compose exec backup backup.sh once
#   backup.sh health   the Docker health check: was there a good backup in the last 26 hours?
#
# One backup is: pg_dump (custom format) into /tmp, a RAM-only tmpfs, so the readable copy never
# touches a disk; check that pg_restore can read it; encrypt it for BACKUP_AGE_RECIPIENT (a
# public key: the VM can lock backups but never open them); upload it with rclone; delete the
# local files. The bucket deletes old copies itself (lifecycle rules), so this script never
# deletes anything remotely.
#
# Settings come from the environment (compose.yaml): PGHOST, PGDATABASE, PGUSER, PGPASSWORD,
# BACKUP_TIME (HH:MM), BACKUP_AGE_RECIPIENT, BACKUP_BUCKET, BACKUP_PING_URL (optional) and the
# rclone remote `offsite` (RCLONE_CONFIG_OFFSITE_*).
set -eu

work=/tmp/backup
state=/tmp/state
remote="offsite:${BACKUP_BUCKET:-}"

log() { printf '%s backup: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }

# Tells an outside monitor how it went (optional): "" for success, "/fail" for a failure. A
# monitor such as healthchecks.io emails you when the success ping doesn't arrive on time, which
# also catches a stopped container or a dead VM.
ping_monitor() {
  [ -n "${BACKUP_PING_URL:-}" ] || return 0
  wget -q -T 10 -O /dev/null "${BACKUP_PING_URL}$1" || log "could not reach BACKUP_PING_URL"
}

once() {
  : "${BACKUP_AGE_RECIPIENT:?BACKUP_AGE_RECIPIENT is not set}"
  : "${BACKUP_BUCKET:?BACKUP_BUCKET is not set}"
  name="blinks-$(date -u +%Y-%m-%dT%H%M%SZ).dump"
  month="blinks-$(date -u +%Y-%m).dump.age"

  rm -rf "$work"
  mkdir -p "$work" "$state"
  trap 'rm -rf "$work"' EXIT

  pg_dump --format=custom --no-password --file "$work/$name"
  # A dump pg_restore can't read is no backup: read its table of contents before going on.
  pg_restore --list "$work/$name" >/dev/null
  age --encrypt --recipient "$BACKUP_AGE_RECIPIENT" --output "$work/$name.age" "$work/$name"
  rm -f "$work/$name"

  rclone copyto "$work/$name.age" "$remote/daily/$name.age"
  # The first good backup of each month is also kept as that month's copy, which lives longer.
  # --ignore-existing leaves it alone on the other days.
  rclone copyto --ignore-existing "$work/$name.age" "$remote/monthly/$month"

  date -u +%s >"$state/last-ok"
  log "uploaded daily/$name.age ($(wc -c <"$work/$name.age") bytes)"
}

# Seconds from now until the next HH:MM (UTC). Zero means now.
seconds_until() {
  h=${1%:*} m=${1#*:}
  # Without the leading zero: $(( 08 )) would be read as a (bad) octal number.
  h=${h#0} m=${m#0}
  target=$((h * 3600 + m * 60))
  now=$(($(date -u +%s) % 86400))
  echo $(((target - now + 86400) % 86400))
}

loop() {
  at="${BACKUP_TIME:-02:30}"
  case "$at" in
  [0-1][0-9]:[0-5][0-9] | 2[0-3]:[0-5][0-9]) ;;
  *) log "BACKUP_TIME must be HH:MM, got '$at'" && exit 1 ;;
  esac
  mkdir -p "$state"
  date -u +%s >"$state/started"
  # `docker compose stop` sends a signal; the shell only acts on it between commands, so each
  # sleep runs in the background and the loop waits on it, and stopping takes no time.
  trap 'log "stopping"; exit 0' TERM INT
  log "daily backups at $at UTC to $remote"
  while :; do
    sleep "$(seconds_until "$at")" &
    wait $!
    # A separate process, so `set -e` stops that backup at its first error and the loop
    # carries on to tomorrow's.
    if "$0" once; then
      ping_monitor ""
    else
      log "FAILED: see the lines above"
      ping_monitor /fail
    fi
    # Never twice in the same minute, even if the backup took under a second.
    sleep 60 &
    wait $!
  done
}

# Healthy if a backup worked in the last 26 hours, or the container started less than 26 hours
# ago and hasn't reached its first backup yet. The files are in /tmp, so a restart starts over.
health() {
  last=$(cat "$state/last-ok" 2>/dev/null || cat "$state/started")
  [ $(($(date -u +%s) - last)) -lt $((26 * 3600)) ]
}

case "${1:-loop}" in
loop) loop ;;
once) once ;;
health) health ;;
*) echo "usage: backup.sh [loop|once|health]" >&2 && exit 2 ;;
esac
