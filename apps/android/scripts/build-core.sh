#!/usr/bin/env bash
# Build the owner role (cmd/aide-mobile/android, the iPhone's core) as an
# Android library with gomobile, for phones (arm64) and the emulator (x86_64):
#   apps/android/scripts/build-core.sh   →   apps/android/build/core/aidecore.aar
# It does nothing when the library is newer than every Go source file
# (the Gradle build runs it before each build). VERSION stamps the version.
# No build names a server: logging in asks for it (docs/accounts.md).
# Needs ANDROID_HOME (or ANDROID_SDK_ROOT) with an NDK in it, or
# ANDROID_NDK_HOME; gomobile and gobind come from go.mod's tools.
set -euo pipefail
cd "$(dirname "$0")/../../.."
OUT=apps/android/build/core/aidecore.aar
VERSION="${VERSION:-0.0.0-dev}"
if [[ -f "$OUT" && -z "${FORCE:-}" ]] && [[ -z "$(find cmd internal pkg gen go.mod go.sum -newer "$OUT" -name '*.go' -o -newer "$OUT" -name 'go.*' | head -1)" ]]; then
  echo "› aidecore.aar is up to date"
  exit 0
fi
export ANDROID_HOME="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
if [[ -z "$ANDROID_HOME" ]]; then
  echo "build-core.sh: set ANDROID_HOME to the Android SDK" >&2
  exit 1
fi
if [[ -z "${ANDROID_NDK_HOME:-}" ]]; then
  ANDROID_NDK_HOME="$(ls -d "$ANDROID_HOME"/ndk/* 2>/dev/null | sort -V | tail -1)"
  export ANDROID_NDK_HOME
fi
if [[ ! -d "$ANDROID_NDK_HOME" ]]; then
  echo "build-core.sh: no NDK (sdkmanager \"ndk;27.3.13750724\", or set ANDROID_NDK_HOME)" >&2
  exit 1
fi
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
EXE=""
case "$(uname -s 2>/dev/null || true)" in
  MINGW*|MSYS*|CYGWIN*)
    EXE=".exe"
    ;;
esac
mkdir -p "$TMP/bin"
BIN_OUT="$TMP/bin"
if [[ -n "$EXE" ]] && command -v cygpath >/dev/null 2>&1; then
  BIN_OUT="$(cygpath -m "$TMP/bin")"
fi
# gomobile runs gobind from PATH: the version go.mod names.
go build -o "$BIN_OUT/gobind$EXE" golang.org/x/mobile/cmd/gobind
go build -o "$BIN_OUT/gomobile$EXE" golang.org/x/mobile/cmd/gomobile
export PATH="$TMP/bin:$PATH"
mkdir -p "$(dirname "$OUT")"
echo "› building the core for android/arm64 and android/amd64"
gomobile bind -target=android/arm64,android/amd64 -androidapi 30 -javapkg io.aide.android \
  -trimpath -ldflags "-s -w -X github.com/samir2015dzh/aide/internal/machine.Version=${VERSION#v}" \
  -o "$OUT" ./cmd/aide-mobile/android
rm -f "${OUT%.aar}-sources.jar"
echo "› $OUT"
