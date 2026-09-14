#!/usr/bin/env bash
set -euo pipefail
: "${MONEE_VERSION:?}"
: "${MONEE_PLATFORM:?}"
bundle="monee-${MONEE_VERSION}-${MONEE_PLATFORM}"
staging=$(mktemp -d)
package="$staging/$bundle"
mkdir -p "$package/web" dist
go -C server build -trimpath -o "$package/monee-server" ./cmd/monee
cp -R webApp/build/dist/wasmJs/productionExecutable/. "$package/web/"
cp scripts/configure-login.py "$package/"
cp docs/releases/0.0.1.md "$package/README.md"
cp LICENSE "$package/"
printf '%s\n' "$MONEE_VERSION" > "$package/VERSION"
cat > "$package/start.command" <<'START'
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
exec ./monee-server -web-dir "$PWD/web" "$@"
START
chmod +x "$package/start.command"
if [[ "$MONEE_PLATFORM" == macos-* ]]; then
  app=desktopApp/build/compose/binaries/main/app/Monee.app
  version=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$app/Contents/Info.plist")
  test "$version" = "$MONEE_VERSION"
  cp -R "$app" "$package/"
  cp desktopApp/build/compose/binaries/main/dmg/*.dmg "dist/Monee-${MONEE_VERSION}-${MONEE_PLATFORM}.dmg"
fi
tar -czf "dist/$bundle.tar.gz" -C "$staging" "$bundle"
(cd dist && shasum -a 256 "$bundle.tar.gz" > "$bundle.tar.gz.sha256")
if [[ "$MONEE_PLATFORM" == macos-* ]]; then
  (cd dist && shasum -a 256 "Monee-${MONEE_VERSION}-${MONEE_PLATFORM}.dmg" > "Monee-${MONEE_VERSION}-${MONEE_PLATFORM}.dmg.sha256")
fi
