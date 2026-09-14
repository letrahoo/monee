#!/bin/bash
set -euo pipefail

# Format-only derivatives: preserve the complete brand artwork and its proportions.
source_png="$1"
output_icns="$2"
icon_workspace=$(mktemp -d "${TMPDIR:-/tmp}/monee-icon.XXXXXX")
trap 'rm -rf "$icon_workspace"' EXIT
iconset="$icon_workspace/Monee.iconset"
mkdir -p "$iconset" "$(dirname "$output_icns")"

for size in 16 32 128 256 512; do
    /usr/bin/sips -z "$size" "$size" "$source_png" --out "$iconset/icon_${size}x${size}.png" >/dev/null
    retina_size=$((size * 2))
    /usr/bin/sips -z "$retina_size" "$retina_size" "$source_png" --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done

/usr/bin/iconutil -c icns "$iconset" -o "$output_icns"
