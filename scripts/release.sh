#!/usr/bin/env bash
# Builds release assets into dist/release: binaries for linux/windows
# on arm64/amd64, their version, a signed checksum list and install.sh with
# the public key. Windows also gets aidedw, aided without a console
# window, which its background service runs (docs/windows.md §4.4).
#   RELEASE_SIGNING_KEY_FILE  path to the ECDSA P-256 private key (PEM)
#   VERSION                   e.g. v0.1.0
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:?set VERSION}"
OUT=dist/release
rm -rf "$OUT" && mkdir -p "$OUT"
# No build names a server: logging in asks for it (docs/accounts.md).
LDFLAGS="-s -w -X github.com/samir2015dzh/aide/internal/machine.Version=${VERSION#v}"
for os in linux; do
  for arch in arm64 amd64; do
    for cmd in aided aide aide-relay; do
      GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/$cmd-$os-$arch" "./cmd/$cmd"
    done
  done
done
for arch in arm64 amd64; do
  for cmd in aided aide; do
    GOOS=windows GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/$cmd-windows-$arch.exe" "./cmd/$cmd"
  done
  GOOS=windows GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS -H windowsgui" -o "$OUT/aidedw-windows-$arch.exe" ./cmd/aided
done
# The programs under the names they had before the rename too, the same
# files: installations from before it update by downloading the names they
# know (docs/aide-rename.md; internal/legacy names them for the Go code).
LEGACY=hermesh
for f in "$OUT"/aided-* "$OUT"/aidedw-* "$OUT"/aide-*; do
  b="$(basename "$f")"
  cp "$f" "$OUT/$LEGACY${b#aide}"
done
# The version, signed with the programs, for installed ones to update (docs/machine-updates.md).
printf '%s\n' "${VERSION#v}" > "$OUT/version"
(cd "$OUT" && (command -v sha256sum >/dev/null && sha256sum aide* hermesh* version || shasum -a 256 aide* hermesh* version) > checksums.txt)
openssl dgst -sha256 -sign "${RELEASE_SIGNING_KEY_FILE:?set RELEASE_SIGNING_KEY_FILE}" -out "$OUT/checksums.txt.sig" "$OUT/checksums.txt"
PUBKEY_FILE="${RELEASE_PUBKEY_FILE:-scripts/release-pubkey.pem}"
openssl dgst -sha256 -verify "$PUBKEY_FILE" -signature "$OUT/checksums.txt.sig" "$OUT/checksums.txt"
python3 - "$OUT" "$PUBKEY_FILE" <<'PY'
import sys
pub = open(sys.argv[2]).read().strip()
for name in ("install.sh", "install.ps1"):
    src = open("scripts/" + name).read()
    open(sys.argv[1] + "/" + name, "w").write(src.replace("__AIDE_RELEASE_PUBKEY__", pub))
PY
chmod +x "$OUT/install.sh"
echo "✓ release assets in $OUT"
