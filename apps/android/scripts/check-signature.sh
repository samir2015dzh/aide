#!/usr/bin/env bash
# Checks that an APK is signed with the app's own key, the one every build is
# signed with (docs/android-app.md §6.1), by its certificate's SHA-256:
#   apps/android/scripts/check-signature.sh apps/android/build/Aide-<version>-debug.apk
# Needs the Android SDK's build-tools (apksigner) in ANDROID_HOME (or ANDROID_SDK_ROOT).
set -euo pipefail
# The certificate's SHA-256 (public: anyone can read it from an APK).
want=DE:97:97:AE:37:45:09:54:6B:D5:34:DF:69:C5:91:21:0B:33:D9:61:20:E0:8C:38:09:2C:3C:8E:15:2D:D1:79
apk="${1:?usage: check-signature.sh <apk>}"
sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
apksigner="$(ls -d "$sdk"/build-tools/*/apksigner 2>/dev/null | sort -V | tail -1)"
if [[ -z "$apksigner" ]]; then
  echo "check-signature.sh: no apksigner under ANDROID_HOME's build-tools" >&2
  exit 1
fi
certs="$("$apksigner" verify --print-certs "$apk")"
echo "$certs"
# One line per signer and scheme ("Signer #1 certificate SHA-256 digest: …",
# or "V2 Signer: certificate SHA-256 digest: …" by newer apksigners): every
# one must be the app's certificate.
digests="$(sed -n 's/^.*[Ss]igner.*certificate SHA-256 digest: //p' <<<"$certs" | tr '[:upper:]' '[:lower:]' | sort -u)"
want_hex="$(tr -d ':' <<<"$want" | tr '[:upper:]' '[:lower:]')"
if [[ "$digests" != "$want_hex" ]]; then
  echo "check-signature.sh: $apk is not signed with the app's key alone (SHA-256: ${digests:-none}; want $want)" >&2
  exit 1
fi
echo "› signed with the app's key ($want)"
