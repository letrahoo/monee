#!/usr/bin/env bash
# No Docker daemon or credentials needed: verify full references are not altered.
set -euo pipefail
cd "$(dirname "$0")/.."
if docker compose version >/dev/null 2>&1; then
  compose=(docker compose)
else
  compose=(docker-compose)
fi
export MONEE_PUBLIC_URL=https://monee.test
export MONEE_API_IMAGE=ghcr.io/example/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
export MONEE_WEB_IMAGE=ghcr.io/example/web@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
check_references() {
  "${compose[@]}" --file compose.yaml config --format json | jq -e \
    --arg api "$MONEE_API_IMAGE" --arg web "$MONEE_WEB_IMAGE" \
    '.services.api.image == $api and .services.web.image == $web' >/dev/null
}
check_references
export MONEE_API_IMAGE=monee-api:smoke MONEE_WEB_IMAGE=monee-web:smoke
check_references
echo 'Independent digest references and local smoke tags resolve unchanged.'
