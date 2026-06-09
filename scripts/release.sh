#!/usr/bin/env bash
#
# Cut a release. Tags vX.Y.Z and pushes it; the GitHub Actions "release"
# workflow then cross-builds every platform, writes SHA256SUMS, and publishes
# the GitHub release (which `pixeltui update` pulls from).
#
#   scripts/release.sh v0.2.0      # stable
#   scripts/release.sh v0.2.0-rc1  # pre-release (won't become "latest")
#
set -euo pipefail

ver="${1:-}"
if [ -z "$ver" ]; then
	echo "usage: scripts/release.sh vX.Y.Z" >&2
	exit 1
fi
[[ "$ver" == v* ]] || ver="v${ver}"

if ! [[ "$ver" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
	echo "error: '${ver}' is not a vMAJOR.MINOR.PATCH tag (e.g. v0.2.0 or v0.2.0-rc1)" >&2
	exit 1
fi

branch="$(git rev-parse --abbrev-ref HEAD)"
if [ "$branch" != "main" ]; then
	echo "error: cut releases from main (currently on '${branch}')" >&2
	exit 1
fi
if ! git diff --quiet || ! git diff --cached --quiet; then
	echo "error: working tree is dirty -- commit or stash first" >&2
	exit 1
fi
if git rev-parse "$ver" >/dev/null 2>&1; then
	echo "error: tag ${ver} already exists" >&2
	exit 1
fi

echo ">> sanity build"
go build ./...

echo ">> tagging ${ver}"
git tag -a "$ver" -m "$ver"

echo ">> pushing ${ver}"
git push origin "$ver"

echo ""
echo "Pushed ${ver} -- GitHub Actions is building and publishing the release."
echo "  Watch:  gh run watch"
echo "  After:  gh release view ${ver}"
