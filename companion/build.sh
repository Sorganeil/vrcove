#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p dist
default_version=$(tr -d '[:space:]' < ../VERSION 2>/dev/null || true)
version="${VERSION:-${default_version:-dev}}"
commit="${COMMIT:-$(git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)}"
build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
# Metadata is passed through Go's linker flag parser; reject spaces/options.
[[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9.+_-]*$ ]] || { echo 'Invalid VERSION' >&2; exit 2; }
[[ "$commit" =~ ^[A-Za-z0-9._-]+$ ]] || { echo 'Invalid COMMIT' >&2; exit 2; }
[[ "$build_date" =~ ^[0-9TZ:+.-]+$ ]] || { echo 'Invalid BUILD_DATE' >&2; exit 2; }
ldflags="-buildid= -X vrcove/companion/internal/buildinfo.Version=$version -X vrcove/companion/internal/buildinfo.Commit=$commit -X vrcove/companion/internal/buildinfo.Date=$build_date"
export CGO_ENABLED=1
# Strip symbols and size-optimize the Linux native window glue.
export CGO_CFLAGS="-Os -ffunction-sections -fdata-sections"
export CGO_CXXFLAGS="-Os -ffunction-sections -fdata-sections"
case "${1:-linux}" in
  linux|windows|desktop-linux|desktop-windows)
    # wails3 build makes a release build: the production tag (no developer
    # tools), stripped symbols, and on Windows the icon and version details.
    # It also builds the website frontend in desktop mode unless
    # SKIP_FRONTEND_BUILD=1. See cmd/vrcove-desktop/Taskfile.yml.
    goos=linux
    [[ "$1" == *windows ]] && goos=windows
    (cd cmd/vrcove-desktop && wails3 build GOOS="$goos" LDFLAGS="$ldflags")
    ;;
  *) echo 'Usage: bash build.sh [linux|windows]' >&2; exit 2 ;;
esac
