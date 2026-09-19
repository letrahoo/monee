#!/bin/bash
set -euo pipefail

# Explicit HTTPS test mode, isolated from the ordinary local app's data/session scope.
project_dir="$(cd "$(dirname "$0")/.." && pwd)"
app_binary="$project_dir/desktopApp/build/compose/binaries/main/app/Monee.app/Contents/MacOS/Monee"
if [ ! -x "$app_binary" ]; then
  echo 'Build the Mac distributable before running this launcher.' >&2
  exit 1
fi
export MONEE_SERVER_URL="${MONEE_SERVER_URL-https://monee.test.letra.xin}"
export MONEE_DATA_DIR="${MONEE_DATA_DIR:-$HOME/Library/Application Support/Monee-Test}"
# Optional MONEE_SERVER_CA_FILE must point to a trusted public CA certificate.
# Do not disable hostname/certificate validation or copy OAuth secrets into the app.
exec "$app_binary"
