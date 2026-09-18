#!/usr/bin/env bash
# Uses only synthetic credentials and uniquely named disposable resources.
# Requires locally loaded monee-api:smoke and monee-web:smoke images.
set -euo pipefail
cd "$(dirname "$0")/.."
if docker compose version >/dev/null 2>&1; then
  compose=(docker compose)
else
  compose=(docker-compose)
fi
scratch=$(mktemp -d "${TMPDIR:-/tmp}/monee-compose.XXXXXXXX")
run_id="monee-smoke-$(date +%s)-$$"
export MONEE_API_IMAGE=monee-api:smoke MONEE_WEB_IMAGE=monee-web:smoke
# Browsers normalize this spelling before sending Host/Origin.
export MONEE_PUBLIC_URL=HTTPS://MONEE.TEST:443
export MONEE_AUTH_DIR="$scratch/auth"
export MONEE_EDGE_NETWORK="$run_id-edge" MONEE_DATA_VOLUME="$run_id-data"
compose+=(--project-name "$run_id" --file compose.yaml)
cleanup() {
  status=$?
  if [[ "$status" != 0 ]]; then
    "${compose[@]}" ps --all || true
    "${compose[@]}" logs --no-color --tail 80 || true
  fi
  docker rm --force "$run_id-tls" "$run_id-ip-holder" >/dev/null 2>&1 || true
  "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  docker network rm "$MONEE_EDGE_NETWORK" "$run_id-subnet-probe" >/dev/null 2>&1 || true
  # mktemp-generated directory contains only this test's certificates/config.
  rm -rf -- "$scratch"
  exit "$status"
}
trap cleanup EXIT
mkdir "$scratch/auth" "$scratch/certs"
cp fixtures/synthetic/deployment/auth.synthetic.json "$scratch/auth/auth.json"
chmod 700 "$scratch/auth"
chmod 600 "$scratch/auth/auth.json"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj /CN=monee.test -addext subjectAltName=DNS:monee.test \
  -keyout "$scratch/certs/key.pem" -out "$scratch/certs/cert.pem" 2>/dev/null
docker network create "$MONEE_EDGE_NETWORK" >/dev/null
# Docker requires an explicitly configured subnet for --ip. Ask its
# allocator for an unused subnet, then declare it only in this test's override.
docker network create "$run_id-subnet-probe" >/dev/null
MONEE_SMOKE_SUBNET=$(docker network inspect "$run_id-subnet-probe" | jq -er '.[0].IPAM.Config[0].Subnet')
export MONEE_SMOKE_SUBNET
compose+=(--file fixtures/synthetic/deployment/compose.override.yaml)
"${compose[@]}" config --quiet
docker network rm "$run_id-subnet-probe" >/dev/null
# Reserve it before Compose can allocate the egress network. The ownership
# labels allow Compose to adopt and clean up this precreated private network.
docker network create --internal --subnet "$MONEE_SMOKE_SUBNET" \
  --label "com.docker.compose.project=$run_id" \
  --label com.docker.compose.network=private "${run_id}_private" >/dev/null
"${compose[@]}" up --detach --wait --wait-timeout 120
api_id=$("${compose[@]}" ps --quiet api)
web_id=$("${compose[@]}" ps --quiet web)
for service_id in "$api_id" "$web_id"; do
  docker inspect "$service_id" | jq -e '.[0].HostConfig.LogConfig | .Type == "json-file" and .Config["max-size"] == "10m" and .Config["max-file"] == "3"' >/dev/null
done
# The deployment must not expose either application container on host ports.
test -z "$(docker port "$api_id")"
test -z "$(docker port "$web_id")"
docker run --detach --name "$run_id-tls" --network "$MONEE_EDGE_NETWORK" \
  --network-alias monee.test \
  --mount "type=bind,src=$scratch/certs,dst=/certs,readonly" \
  --mount "type=bind,src=$PWD/fixtures/synthetic/deployment/nginx.conf,dst=/etc/nginx/conf.d/default.conf,readonly" \
  nginx:1.29-alpine >/dev/null
request() {
  docker run --rm --network "$MONEE_EDGE_NETWORK" \
    --mount "type=bind,src=$scratch/certs/cert.pem,dst=/cert.pem,readonly" \
    curlimages/curl:8.12.1 --silent --show-error --max-time 20 \
    --cacert /cert.pem "$@"
}
request --retry 10 --retry-connrefused --retry-delay 1 --fail \
  https://monee.test/api/v1/health >/dev/null
test "$(request --output /dev/null --write-out '%{http_code}' https://monee.test/api/v1/dashboard)" = 401
test "$(request --output /dev/null --write-out '%{http_code}' -H 'Origin: https://evil.test' https://monee.test/api/v1/health)" = 403
test "$(request --output /dev/null --write-out '%{http_code}' -H 'Host: evil.test' https://monee.test/api/v1/health)" = 403
request --fail https://monee.test/index.html > "$scratch/index.html"
request --fail https://monee.test/transactions > "$scratch/route.html"
cmp "$scratch/index.html" "$scratch/route.html"
request --fail https://monee.test/monee.js >/dev/null
request --fail --head https://monee.test/index.html | grep -qi '^strict-transport-security:'
request --fail -H 'Origin: https://monee.test' -H 'Content-Type: application/json' \
  --data '{"provider":"github","client":"web"}' \
  https://monee.test/api/v1/auth/start > "$scratch/start.json"
auth_url=$(jq -er '.url | select(startswith("https://monee.test/auth/begin?ticket="))' "$scratch/start.json")
# Capture response headers on the host (the curl container cannot write host paths).
request --fail --dump-header - --output /dev/null "$auth_url" > "$scratch/headers"
grep -qi '^set-cookie: .*; HttpOnly; Secure; SameSite=Lax' "$scratch/headers"
grep -qi '^location: https://github.com/login/oauth/authorize?' "$scratch/headers"
grep -q 'redirect_uri=https%3A%2F%2Fmonee.test%2Fauth%2Fcallback%2Fgithub' "$scratch/headers"
# Neither successful OAuth requests nor a failed upstream may log credentials.
log_marker="synthetic-oauth-log-probe-$run_id"
request --output /dev/null "https://monee.test/auth/callback/github?state=$log_marker&code=$log_marker"
# Probe outbound TLS from the API's network namespace, without OAuth credentials.
for provider_url in https://accounts.google.com/.well-known/openid-configuration https://github.com/login; do
  docker run --rm --network "container:$api_id" curlimages/curl:8.12.1 \
    --fail --silent --show-error --max-time 30 "$provider_url" >/dev/null
done
docker run --rm --network none --mount "type=volume,src=$MONEE_DATA_VOLUME,dst=/data,readonly" \
  alpine:3.22 sh -ec '
    test "$(stat -c "%u:%g:%a" /data)" = "65532:65532:700"
    test "$(stat -c "%u:%g:%a" /data/auth.json)" = "65532:65532:600"
    test -s /data/application.db
  '
"${compose[@]}" restart api
"${compose[@]}" up --detach --wait --wait-timeout 120
request --fail https://monee.test/api/v1/health >/dev/null
test "$(request --output /dev/null --write-out '%{http_code}' https://monee.test/api/v1/dashboard)" = 401
# A restart retains the IP, unlike replacement during recovery or deployment.
# Reserve the former private IP so this regression cannot pass by IP reuse.
private_network="${run_id}_private"
old_api_ip=$(docker inspect "$api_id" | jq -er --arg network "$private_network" '.[0].NetworkSettings.Networks[$network].IPAddress')
"${compose[@]}" rm --stop --force api
request --output /dev/null "https://monee.test/auth/begin?ticket=$log_marker"
request --output /dev/null "https://monee.test/api/v1/dashboard?q=$log_marker"
docker run --detach --name "$run_id-ip-holder" --network "$private_network" \
  --ip "$old_api_ip" --read-only --cap-drop ALL --security-opt no-new-privileges \
  alpine:3.22 sleep 300 >/dev/null
"${compose[@]}" up --detach --no-deps --force-recreate --wait --wait-timeout 120 api
replacement_api_id=$("${compose[@]}" ps --quiet api)
new_api_ip=$(docker inspect "$replacement_api_id" | jq -er --arg network "$private_network" '.[0].NetworkSettings.Networks[$network].IPAddress')
test "$old_api_ip" != "$new_api_ip"
test "$web_id" = "$("${compose[@]}" ps --quiet web)"
request --retry 12 --retry-all-errors --retry-delay 1 --fail \
  https://monee.test/api/v1/health >/dev/null
test "$(request --output /dev/null --write-out '%{http_code}' https://monee.test/api/v1/dashboard)" = 401
request --fail -H 'Origin: https://monee.test' -H 'Content-Type: application/json' \
  --data '{"provider":"github","client":"web"}' \
  https://monee.test/api/v1/auth/start > "$scratch/recreated-start.json"
replacement_auth_url=$(jq -er '.url | select(startswith("https://monee.test/auth/begin?ticket="))' "$scratch/recreated-start.json")
request --fail --dump-header - --output /dev/null "$replacement_auth_url" > "$scratch/recreated-headers"
grep -qi '^location: https://github.com/login/oauth/authorize?' "$scratch/recreated-headers"
docker logs "$web_id" > "$scratch/web.log" 2>&1
docker logs "$run_id-tls" > "$scratch/tls.log" 2>&1
if grep -Eq 'ticket=|state=|code=|q=' "$scratch/web.log" "$scratch/tls.log" || \
   grep -Fq "$log_marker" "$scratch/web.log" "$scratch/tls.log"; then
  echo 'Sensitive query parameters appeared in proxy logs' >&2
  exit 1
fi
echo 'Compose TLS, proxy, authorization boundary, OAuth redirect, egress, volume, restart and API replacement checks passed.'
