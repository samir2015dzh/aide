#!/usr/bin/env bash
# A phone's frame times while a page of Aide scrolls (工单 #55), from the system's
# own counters (dumpsys gfxinfo), for before/after on the same phone:
#   apps/android/scripts/frame-stats.sh [rounds]     (default 10)
# Open Aide on the page to measure (Machines, Tasks…) first; the phone on adb.
# It swipes the page up and down `rounds` times, then prints the frames drawn,
# the janky share and the 50/90/95/99th percentile frame times.
set -euo pipefail
PKG="${PKG:-io.aide.android.debug}"
ROUNDS="${1:-10}"
size="$(adb shell wm size | awk '/size/ {print $NF}' | tail -1)"
w="${size%x*}"; h="${size#*x}"
x=$((w / 2)); top=$((h / 4)); bottom=$((h * 3 / 4))
adb shell dumpsys gfxinfo "$PKG" reset >/dev/null
for _ in $(seq "$ROUNDS"); do
  adb shell input swipe "$x" "$bottom" "$x" "$top" 250
  adb shell input swipe "$x" "$top" "$x" "$bottom" 250
done
adb shell dumpsys gfxinfo "$PKG" | grep -E "Total frames rendered|Janky frames|50th percentile|90th percentile|95th percentile|99th percentile" | head -6
