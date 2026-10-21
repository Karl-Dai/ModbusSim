#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root/go/cmd/slavegui"
if [[ "$(uname -s)" == Darwin ]]; then
  export CGO_ENABLED=1
  export CGO_CFLAGS="${CGO_CFLAGS:+$CGO_CFLAGS }-mmacosx-version-min=13.0"
  export CGO_LDFLAGS="${CGO_LDFLAGS:+$CGO_LDFLAGS }-framework UniformTypeIdentifiers -mmacosx-version-min=13.0"
fi
if [[ $# -eq 0 ]]; then set -- dev; fi
case "$1" in
  bindings) shift; set -- generate module "$@" ;;
  dev) ;;
  build) ;;
  test) shift; cd "$repo_root/go"; exec go test "$@" ;;
  *) echo 'Usage: slavegui.sh [dev|build|bindings|test] [Wails flags]' >&2; exit 2 ;;
esac
# Reuse a matching CLI when present, so repeated builds do not depend on the
# module proxy's version lookup. The pinned go-run fallback bootstraps new hosts.
wails_binary="$(command -v wails || true)"
if [[ -z "$wails_binary" ]]; then
  go_path="$(go env GOPATH)"
  wails_binary="${go_path%%:*}/bin/wails"
fi
wails_command=(go run github.com/wailsapp/wails/v2/cmd/wails@v2.14.0)
if [[ -x "$wails_binary" ]]; then
  wails_version="$("$wails_binary" version)"
  if [[ "${wails_version%%$'\n'*}" == v2.14.0 ]]; then
    wails_command=("$wails_binary")
  fi
fi
if [[ "$1" == build ]]; then
  "${wails_command[@]}" "$@"
  # Wails dev removes the executable under build/bin when it exits. Keep the
  # validated standalone bundle outside that development output directory.
  if [[ "$(uname -s)" == Darwin ]]; then
    mkdir -p build/release
    ditto build/bin/ModbusSlave.app build/release/ModbusSlave.app
    printf 'Standalone app: %s\n' "$PWD/build/release/ModbusSlave.app"
  fi
else
  exec "${wails_command[@]}" "$@"
fi
