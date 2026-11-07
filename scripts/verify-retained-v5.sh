#!/usr/bin/env bash
# Build genuine, unmodified v5 programs; never run against an operator's database.
set -euo pipefail
revision=dab74a5fdcfb403c47900dbb21023540517978d2
root=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
python=${PYTHON:-python3}
# Ambient overlays/workspaces/flags must not silently change the retained build.
export GOENV=off GOWORK=off GOFLAGS=
unset GOOS GOARCH
command -v git >/dev/null
command -v go >/dev/null
command -v "$python" >/dev/null
case $(uname -s) in Darwin|Linux) ;; *) echo 'Retained-v5 verification requires macOS or Linux.' >&2; exit 1 ;; esac
# A shallow CI checkout may not yet contain the exact pinned object. Never use a
# branch tip as a substitute, nor alter the current checkout or its dependencies.
if ! git -C "$root" cat-file -e "$revision^{commit}" 2>/dev/null; then
  git -C "$root" fetch --no-tags origin "$revision"
fi
test "$(git -C "$root" rev-parse "$revision^{commit}")" = "$revision"
# Deliberately retain the immutable checkout and manifest for diagnosis. No rm,
# worktree removal, or cleanup of user-provided paths is performed by this script.
artifact=$(mktemp -d "${TMPDIR:-/tmp}/monee-retained-v5.XXXXXX")
artifact=$(cd "$artifact" && pwd -P)
chmod 700 "$artifact"
printf 'Retained-v5 artifact: %s\n' "$artifact"
if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  printf 'artifact=%s\n' "$artifact" >> "$GITHUB_OUTPUT"
fi
# Use a real .git directory: the selected Go toolchain does not recognize a
# linked-worktree .git file for VCS stamping, even with -buildvcs=true.
git clone --shared --no-checkout "$root" "$artifact/source"
# Cloning a shallow repository need not transfer an unreferenced FETCH_HEAD,
# even after the outer checkout fetched the pinned object. Fetch inside too.
if ! git -C "$artifact/source" cat-file -e "$revision^{commit}" 2>/dev/null; then
  git -C "$artifact/source" fetch --no-tags origin "$revision"
fi
git -C "$artifact/source" checkout --detach "$revision"
go -C "$artifact/source/server" build -trimpath -buildvcs=true -o "$artifact/monee-v5" ./cmd/monee
go -C "$artifact/source/server" build -trimpath -buildvcs=true -o "$artifact/database-backup-v5" ./cmd/database-backup
go -C "$artifact/source/server" test -c -trimpath -buildvcs=true -o "$artifact/fixed-schema5-tests" ./cmd/monee
"$python" "$root/scripts/retained_v5_manifest.py" "$artifact" "$revision"
# The tagged suite fails if its manifest/binaries are missing; no environment skip.
MONEE_RETAINED_V5_DIR="$artifact" go -C "$root/server" test -race -count=1 -v -tags=retained_v5 -run '^TestRetainedV5BinaryBoundary$' ./internal/ledger
