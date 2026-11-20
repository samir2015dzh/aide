#!/usr/bin/env bash
# Builds release assets into dist/release: binaries for linux/windows
# on arm64/amd64, their version, a signed checksum list and install.sh with
# the public key. Windows also gets hermeshdw, hermeshd without a console
# window, which its background service runs (docs/windows.md §4.4). Linux
# and Windows also get her, Her's kernel, and her console, built beforehand,
# which install.sh and install.ps1 put beside hermeshd (docs/her-server.md §6.4).
#   RELEASE_SIGNING_KEY_FILE  path to the ECDSA P-256 private key (PEM)
#   VERSION                   e.g. v0.1.0
#   HER_LINUX_AMD64, HER_LINUX_ARM64  her for Linux, static with musl
#   HER_WINDOWS_AMD64         her for Windows, which install.ps1 puts beside hermeshd
#   HER_CONSOLE               her console's built files (her/apps/console/dist)
#   HER_PDF_LINUX_AMD64, HER_PDF_LINUX_ARM64  folders with her-doc-worker (her's PDF worker, against glibc) and
#                             PDFium (node apps/desktop/scripts/pdfium.mjs linux <x64|arm64> <folder> puts PDFium there)
#   HER_COMPUTER_LINUX_AMD64, HER_COMPUTER_LINUX_ARM64  folders with agent-desktop for Linux, which
#                             Her's computer use runs (node apps/desktop/scripts/agent-desktop.mjs linux <x64|arm64> <folder>)
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:?set VERSION}"
OUT=dist/release
rm -rf "$OUT" && mkdir -p "$OUT"
# No build names a server: logging in asks for it (docs/accounts.md).
LDFLAGS="-s -w -X github.com/hermeshai/hermesh/internal/machine.Version=${VERSION#v}"
for os in linux; do
  for arch in arm64 amd64; do
    for cmd in hermeshd hermesh hermesh-relay; do
      GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/$cmd-$os-$arch" "./cmd/$cmd"
    done
  done
done
for arch in arm64 amd64; do
  for cmd in hermeshd hermesh; do
    GOOS=windows GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/$cmd-windows-$arch.exe" "./cmd/$cmd"
  done
  GOOS=windows GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS -H windowsgui" -o "$OUT/hermeshdw-windows-$arch.exe" ./cmd/hermeshd
done
# The programs under the names they have since the product was called Aide
# too, the same files (RENAME-AIDE.md R2): aided, aide, aide-relay. Updates and
# install scripts from before then download the names they had.
for f in "$OUT"/hermeshd-* "$OUT"/hermesh-*; do
  b="$(basename "$f")"
  case "$b" in
    hermesh-relay-*) cp "$f" "$OUT/aide-relay-${b#hermesh-relay-}" ;;
    hermeshd-*) cp "$f" "$OUT/aided-${b#hermeshd-}" ;;
    hermesh-*) cp "$f" "$OUT/aide-${b#hermesh-}" ;;
  esac
done
[ -z "${HER_LINUX_AMD64:-}" ] || install -m 0755 "$HER_LINUX_AMD64" "$OUT/her-linux-amd64"
[ -z "${HER_LINUX_ARM64:-}" ] || install -m 0755 "$HER_LINUX_ARM64" "$OUT/her-linux-arm64"
[ -z "${HER_WINDOWS_AMD64:-}" ] || install -m 0644 "$HER_WINDOWS_AMD64" "$OUT/her-windows-amd64.exe"
[ -z "${HER_CONSOLE:-}" ] || COPYFILE_DISABLE=1 tar -czf "$OUT/her-console.tar.gz" -C "$HER_CONSOLE" .
[ -z "${HER_PDF_LINUX_AMD64:-}" ] || COPYFILE_DISABLE=1 tar -czf "$OUT/her-pdf-linux-amd64.tar.gz" -C "$HER_PDF_LINUX_AMD64" .
[ -z "${HER_PDF_LINUX_ARM64:-}" ] || COPYFILE_DISABLE=1 tar -czf "$OUT/her-pdf-linux-arm64.tar.gz" -C "$HER_PDF_LINUX_ARM64" .
[ -z "${HER_COMPUTER_LINUX_AMD64:-}" ] || COPYFILE_DISABLE=1 tar -czf "$OUT/her-computer-linux-amd64.tar.gz" -C "$HER_COMPUTER_LINUX_AMD64" .
[ -z "${HER_COMPUTER_LINUX_ARM64:-}" ] || COPYFILE_DISABLE=1 tar -czf "$OUT/her-computer-linux-arm64.tar.gz" -C "$HER_COMPUTER_LINUX_ARM64" .
# The version, signed with the programs, for installed ones to update (docs/machine-updates.md).
printf '%s\n' "${VERSION#v}" > "$OUT/version"
(cd "$OUT" && (command -v sha256sum >/dev/null && sha256sum her* aide* version || shasum -a 256 her* aide* version) > checksums.txt)
openssl dgst -sha256 -sign "${RELEASE_SIGNING_KEY_FILE:?set RELEASE_SIGNING_KEY_FILE}" -out "$OUT/checksums.txt.sig" "$OUT/checksums.txt"
PUBKEY_FILE="${RELEASE_PUBKEY_FILE:-scripts/release-pubkey.pem}"
openssl dgst -sha256 -verify "$PUBKEY_FILE" -signature "$OUT/checksums.txt.sig" "$OUT/checksums.txt"
python3 - "$OUT" "$PUBKEY_FILE" <<'PY'
import sys
pub = open(sys.argv[2]).read().strip()
for name in ("install.sh", "install.ps1"):
    src = open("scripts/" + name).read()
    open(sys.argv[1] + "/" + name, "w").write(src.replace("__HERMESH_RELEASE_PUBKEY__", pub))
PY
chmod +x "$OUT/install.sh"
echo "✓ release assets in $OUT"
