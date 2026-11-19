#!/usr/bin/env bash
# Makes a release public key the one everything checks releases with:
# scripts/release-pubkey.pem (install.sh and install.ps1, the Windows and Mac
# apps' feeds, release-check.sh) and internal/update's publicKey (machines
# updating themselves). Its private half is the RELEASE_SIGNING_KEY secret.
#   scripts/release-usekey.sh <public key, PEM>
set -euo pipefail
cd "$(dirname "$0")/.."
in="${1:?usage: release-usekey.sh <public key PEM>}"
pem="$(openssl ec -pubin -in "$in" -pubout 2>/dev/null)" || { echo "$in is not an EC public key" >&2; exit 1; }
openssl ec -pubin -in "$in" -text -noout 2>/dev/null | grep -q 'prime256v1\|P-256' || { echo "$in is not a P-256 key" >&2; exit 1; }
der="$(openssl ec -pubin -in "$in" -outform DER 2>/dev/null | base64 | tr -d '\n')"
printf '%s\n' "$pem" > scripts/release-pubkey.pem
sed -i.bak -E "s#^(\tpublicKey = \")[^\"]*(\")#\1${der}\2#" internal/update/update.go && rm -f internal/update/update.go.bak
grep -qF "$der" internal/update/update.go || { echo "internal/update/update.go: publicKey not set" >&2; exit 1; }
echo "✓ release key: scripts/release-pubkey.pem and internal/update publicKey ($der)"
