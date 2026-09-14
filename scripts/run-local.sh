#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export GOPATH="${GOPATH:-$root/.local/go}"
export GOCACHE="${GOCACHE:-$root/.local/go-build}"
export GRADLE_USER_HOME="${GRADLE_USER_HOME:-$root/.local/gradle}"
if [[ -z "${JAVA_HOME:-}" && "$(uname -s)" == Darwin ]]; then
  export JAVA_HOME="$(/usr/libexec/java_home -v 17)"
fi
cd "$root"
./gradlew --no-daemon :webApp:wasmJsBrowserDistribution
mkdir -p "$root/.local/bin"
go -C server build -o "$root/.local/bin/monee" ./cmd/monee
exec "$root/.local/bin/monee" -web-dir "$root/webApp/build/dist/wasmJs/productionExecutable" "$@"
