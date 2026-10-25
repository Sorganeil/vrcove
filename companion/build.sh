#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p dist
version="${VERSION:-dev}"
commit="${COMMIT:-$(git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)}"
build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
# Metadata is passed through Go's linker flag parser; reject spaces/options.
[[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9.+_-]*$ ]] || { echo 'Invalid VERSION' >&2; exit 2; }
[[ "$commit" =~ ^[A-Za-z0-9._-]+$ ]] || { echo 'Invalid COMMIT' >&2; exit 2; }
[[ "$build_date" =~ ^[0-9TZ:+.-]+$ ]] || { echo 'Invalid BUILD_DATE' >&2; exit 2; }
ldflags="-s -w -buildid= -X vrcove/companion/internal/buildinfo.Version=$version -X vrcove/companion/internal/buildinfo.Commit=$commit -X vrcove/companion/internal/buildinfo.Date=$build_date"
export CGO_ENABLED=1
# Strip symbols and size-optimize the Linux native window glue.
export CGO_CFLAGS="-Os -ffunction-sections -fdata-sections"
export CGO_CXXFLAGS="-Os -ffunction-sections -fdata-sections"
case "${1:-linux}" in
  linux)
    export CGO_LDFLAGS="-Wl,--gc-sections"
    go build -trimpath -buildvcs=false -tags 'novulkan netgo osusergo' -ldflags="$ldflags" -o dist/vrcove-logger ./cmd/vrcove-logger
    ;;
  windows)
    export GOOS=windows GOARCH=amd64 CGO_ENABLED=0
    go build -trimpath -buildvcs=false -tags 'noopengl netgo osusergo' -ldflags="$ldflags -H=windowsgui" -o dist/vrcove-logger.exe ./cmd/vrcove-logger
    ;;
  desktop-linux|desktop-windows)
    # The experimental desktop app embeds the website frontend built in desktop mode.
    (cd .. && npm run build:desktop)
    if [[ "$1" == desktop-linux ]]; then
      go build -trimpath -buildvcs=false -ldflags="$ldflags" -o dist/vrcove-desktop ./cmd/vrcove-desktop
    else
      GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="$ldflags -H=windowsgui" -o dist/vrcove-desktop.exe ./cmd/vrcove-desktop
    fi
    ;;
  *) echo 'Usage: bash build.sh [linux|windows|desktop-linux|desktop-windows]' >&2; exit 2 ;;
esac
