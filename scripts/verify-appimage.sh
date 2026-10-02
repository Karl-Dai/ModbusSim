#!/usr/bin/env bash
set -euo pipefail

image="$(realpath "$1")"
binary="$2"
window_pattern="${3:-.}"
work_dir="$(mktemp -d)"
trap 'rm -rf -- "$work_dir"' EXIT
chmod +x "$image"
cd "$work_dir"
"$image" --appimage-extract > /dev/null

for entry in AppRun AppRun.wrapped "usr/bin/$binary"; do
  mode="$(stat -c '%a' "squashfs-root/$entry")"
  if (( (8#$mode & 0055) != 0055 )); then
    echo "::error::$entry has mode $mode; every user needs read/execute permission"
    exit 1
  fi
done

# Validate the actual packaged launcher and libraries, without a desktop window.
xvfb-run -a bash -s -- "$work_dir/squashfs-root" "$window_pattern" <<'SMOKE'
set -euo pipefail
cd "$1"
./AppRun > appimage-smoke.log 2>&1 &
app_pid=$!
trap 'kill "$app_pid" 2>/dev/null || true' EXIT
window_seen=false
for attempt in {1..15}; do
  sleep 1
  if ! kill -0 "$app_pid" 2>/dev/null; then
    cat appimage-smoke.log
    echo "::error::AppImage exited before the startup smoke test completed"
    exit 1
  fi
  # AppRun may keep a parent process while the GTK window belongs to its child.
  # This display is isolated, so match the application title rather than that PID.
  if xdotool search --onlyvisible --name "$2" > /dev/null 2>&1; then
    window_seen=true
  fi
done
if [ "$window_seen" != true ]; then
  cat appimage-smoke.log
  xwininfo -root -tree || true
  echo "::error::AppImage did not show a window"
  exit 1
fi
echo "AppImage permissions and startup verified"
SMOKE
