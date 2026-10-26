#!/usr/bin/env bash
# Build/package by default; only --publish writes to GitHub. Never creates tags.
set -euo pipefail
cd "$(dirname "$0")"

publish=false
if [[ "${1:-}" == --publish ]]; then publish=true; shift; fi
if [[ $# == 0 && -f ../VERSION ]]; then
  set -- "$(tr -d '[:space:]' < ../VERSION)"
fi
if [[ $# != 1 || ! "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9]+([.-][A-Za-z0-9]+)*)?$ ]]; then
  echo 'Usage: bash companion/release.sh [--publish] [vMAJOR.MINOR.PATCH[-prerelease]]' >&2
  exit 2
fi
tag="$1"
if [[ -f ../VERSION ]]; then
  file_version=$(tr -d '[:space:]' < ../VERSION)
  if [[ "$file_version" != "$tag" ]]; then
    echo "Release tag ($tag) does not match VERSION ($file_version)." >&2
    exit 1
  fi
fi
export VERSION="$tag"
export COMMIT
COMMIT=$(git rev-parse --verify HEAD)
export BUILD_DATE
BUILD_DATE=$(git show -s --format=%cI HEAD)
export SOURCE_DATE_EPOCH
SOURCE_DATE_EPOCH=$(git show -s --format=%ct HEAD)
export GOARCH=amd64

if "$publish"; then
  if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then
    echo 'Refusing to publish an uncommitted working tree. Commit and tag the reviewed source first.' >&2
    exit 1
  fi
  tagged_commit=$(git rev-parse --verify "refs/tags/$tag^{commit}")
  if [[ "$tagged_commit" != "$COMMIT" ]]; then
    echo 'The release tag must point at the checked-out commit.' >&2; exit 1
  fi
  command -v gh >/dev/null || { echo 'Publishing requires the GitHub CLI (gh).' >&2; exit 1; }
fi

packages=$(go list ./... || true)
go test -race $packages
go vet $packages
command -v wails3 >/dev/null || { echo 'Release builds need the Wails CLI (wails3) matching go.mod.' >&2; exit 1; }
bash build.sh linux
SKIP_FRONTEND_BUILD=1 bash build.sh windows
# This path must work headlessly; metadata is checked before packaging.
desktop_version_output=$(dist/vrcove-desktop --version)
[[ "$desktop_version_output" == "VRCove Desktop $tag ("* ]] || { echo 'Embedded desktop version did not match the release tag.' >&2; exit 1; }
python3 package-release.py "$tag"

out="dist/releases/$tag"
assets=(
  "$out/vrcove-desktop-$tag-linux-amd64.tar.gz"
  "$out/vrcove-desktop-$tag-windows-amd64.zip"
  "$out/SHA256SUMS"
)
if ! "$publish"; then
  printf 'Packages ready in %s. Nothing published.\n' "$out"
  exit 0
fi

# Only the explicitly named remote tag may be released. --verify-tag prevents
# gh from creating a tag on the default branch when a tag is missing remotely.
repo="${GH_REPO:-${GITHUB_REPOSITORY:-}}"
if [[ -z "$repo" ]]; then repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner); fi
remote_commit=$(gh api "repos/$repo/commits/$tag" --jq .sha)
if [[ "$remote_commit" != "$COMMIT" ]]; then
  echo 'The GitHub tag does not match the built commit. Nothing published.' >&2; exit 1
fi
state=$(gh release view "$tag" --repo "$repo" --json isDraft --jq .isDraft 2>/dev/null || true)
if [[ "$state" == false ]]; then
  echo 'This release is already published; refusing to replace its assets.' >&2; exit 1
fi
if [[ "$state" == true ]]; then
  # A previous failed upload can be retried while the release is still a draft.
  gh release upload "$tag" "${assets[@]}" --repo "$repo" --clobber
else
  flags=(--draft --verify-tag --generate-notes --title "VRCove Desktop $tag")
  if [[ "$tag" == *-* ]]; then flags+=(--prerelease --latest=false); fi
  gh release create "$tag" "${assets[@]}" --repo "$repo" "${flags[@]}"
fi
# Re-query before publishing: a partial upload must never become public.
asset_names=$(gh release view "$tag" --repo "$repo" --json assets --jq '.assets[].name')
for asset in "${assets[@]}"; do
  name="${asset##*/}"
  if ! printf '%s\n' "$asset_names" | grep -Fxq -- "$name"; then
    echo "Missing release asset: $name. Release left as a draft." >&2; exit 1
  fi
done
gh release edit "$tag" --repo "$repo" --draft=false
