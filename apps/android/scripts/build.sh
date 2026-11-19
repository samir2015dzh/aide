#!/usr/bin/env bash
# Build the Android app, debug-signed, in one command:
#   apps/android/scripts/build.sh   →   apps/android/build/Aide-<version>-debug.apk
# It builds the shared Go core first (build-core.sh, through Gradle's preBuild),
# then the app and its JVM unit tests. Needs Go, a JDK 17+, and the Android
# SDK in ANDROID_HOME (or ANDROID_SDK_ROOT) with platforms;android-36,
# build-tools;35.0.0 and an NDK (sdkmanager "ndk;27.3.13750724").
# VERSION stamps the version; SKIP_TESTS=1 skips the unit tests.
set -euo pipefail
cd "$(dirname "$0")/.."
export ANDROID_HOME="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
if [[ -z "$ANDROID_HOME" ]]; then
  echo "build.sh: set ANDROID_HOME to the Android SDK" >&2
  exit 1
fi
VERSION="${VERSION:-0.0.0-dev}"
export VERSION
tasks=(:app:assembleDebug)
[[ -n "${SKIP_TESTS:-}" ]] || tasks+=(:app:testDebugUnitTest)
./gradlew --console=plain "${tasks[@]}" -PappVersion="${VERSION#v}"
out="build/Aide-${VERSION#v}-debug.apk"
mkdir -p build
cp app/build/outputs/apk/debug/app-debug.apk "$out"
echo "› apps/android/$out"
