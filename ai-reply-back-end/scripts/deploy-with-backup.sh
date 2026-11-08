#!/usr/bin/env bash
# Run after updating this checkout and configuring its existing .env.
# See ../../docs/PRODUCTION_RELEASE.md for configuration and recovery.
set -Eeuo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
umask 077

aireply_container=$(docker compose ps -q backend)
if [[ -z "$aireply_container" ]] || [[ "$(docker inspect --format '{{.State.Running}}' "$aireply_container")" != true ]]; then
  printf 'No running backend in this Compose project; deployment stopped.\n' >&2
  exit 1
fi
aireply_image=$(docker inspect --format '{{.Image}}' "$aireply_container")
aireply_volume=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{.Name}}{{end}}{{end}}' "$aireply_container")
if [[ -z "$aireply_volume" ]]; then
  printf 'The existing backend has no named /app/data volume; deployment stopped.\n' >&2
  exit 1
fi

aireply_stamp=$(date -u +%Y%m%dT%H%M%SZ)
aireply_backup="/home/ai-reply-backups/$aireply_stamp"
# A running container can outlive a removed image. Retain its actual filesystem
# before replacement; named data volumes are backed up separately below.
if ! docker image inspect "$aireply_image" >/dev/null 2>&1; then
  printf 'Original image was removed; retaining the running container image.\n'
  aireply_image=$(docker commit --pause=true "$aireply_container")
fi
docker tag "$aireply_image" "ai-reply-backend:before-$aireply_stamp"
mkdir -p /home/ai-reply-backups
chmod 700 /home/ai-reply-backups
mkdir -m 700 "$aireply_backup" "$aireply_backup/data"
cp -p .env "$aireply_backup/.env"
chmod 600 "$aireply_backup/.env"
printf '%s\n' "$aireply_image" > "$aireply_backup/image-id"
printf '%s\n' "$aireply_volume" > "$aireply_backup/volume-name"
printf 'Backup directory: %s\n' "$aireply_backup"

# These checks leave the running backend and both databases untouched.
docker compose config --quiet
docker compose build backend
docker compose run --rm --no-deps backend -check

aireply_restore_before_migration() {
  local aireply_status=$?
  trap - EXIT
  if (( aireply_status != 0 )); then
    printf 'Backup stage failed; restarting the previous backend.\n' >&2
    docker compose start backend >&2 || true
  fi
  exit "$aireply_status"
}
trap aireply_restore_before_migration EXIT
docker compose stop -t 30 backend
docker cp "$aireply_container:/app/data/." "$aireply_backup/data/"
test -s "$aireply_backup/data/aireply.db"
test -s "$aireply_backup/data/aireply-sandbox.db"

# From this point migrations may run. Never restart an old image over the
# migrated databases automatically; retain the full backup for recovery.
trap - EXIT
docker compose up -d --no-deps --wait --wait-timeout 90 backend
aireply_new_container=$(docker compose ps -q backend)
test -n "$aireply_new_container"
test "$aireply_volume" = "$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{.Name}}{{end}}{{end}}' "$aireply_new_container")"
docker compose ps backend
curl --fail --silent --show-error --max-time 15 https://ai-reply.kz/healthz
curl --fail --silent --show-error --max-time 15 https://ai-reply.kz/sandbox/healthz
curl --fail --silent --show-error --max-time 15 https://ai-reply.kz/api/v1/billing/catalog
curl --fail --silent --show-error --max-time 15 https://ai-reply.kz/sandbox/api/v1/billing/catalog
printf '\nDeployment verified. Backup: %s\n' "$aireply_backup"
