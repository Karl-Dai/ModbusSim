#!/usr/bin/env bash
set -euo pipefail

# Tauri copies this cached launcher into AppRun.wrapped with its mode intact.
# Its default 0770 mode fails when the SquashFS owner differs from the user.
tools_dir="${XDG_CACHE_HOME:-$HOME/.cache}/tauri"
mkdir -p "$tools_dir"
launcher="$tools_dir/AppRun-x86_64"
if [ ! -f "$launcher" ]; then
  curl --fail --location --retry 3 \
    https://github.com/tauri-apps/binary-releases/releases/download/apprun-old/AppRun-x86_64 \
    --output "$launcher"
fi
chmod 755 "$launcher"
