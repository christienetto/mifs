#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# Prefer an installed SDK/JDK; otherwise use this checkout's isolated tools.
export ANDROID_HOME="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$PWD/.tools/sdk}}"
if [[ -z "${JAVA_HOME:-}" ]]; then
  for candidate in "$PWD"/.tools/jdk-*/Contents/Home; do
    if [[ -x "$candidate/bin/java" ]]; then export JAVA_HOME="$candidate"; break; fi
  done
fi
export GRADLE_USER_HOME="${GRADLE_USER_HOME:-$PWD/.tools/gradle-home}"
adb="$ANDROID_HOME/platform-tools/adb"
if [[ ! -x "$adb" ]]; then
  echo "Install the Android SDK and set ANDROID_HOME first." >&2
  exit 1
fi
if [[ $# -gt 0 ]]; then
  device="$1"
else
  devices=$("$adb" devices | awk 'NR > 1 && $2 == "device" { print $1 }')
  count=$(printf '%s\n' "$devices" | awk 'NF { count++ } END { print count+0 }')
  if [[ "$count" != 1 ]]; then
    "$adb" devices -l
    echo "Connect and authorize one device, or pass its serial: ./scripts/run-device.sh SERIAL" >&2
    exit 1
  fi
  device="$devices"
fi
./gradlew :app:assembleDebug --console=plain
"$adb" -s "$device" reverse tcp:8080 tcp:8080
"$adb" -s "$device" install -r app/build/outputs/apk/debug/app-debug.apk
"$adb" -s "$device" shell am start -n com.mifs.android/.MainActivity
