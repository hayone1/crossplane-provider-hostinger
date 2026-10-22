#!/usr/bin/env bash
# Prints the next release version (vX.Y.Z) from the commits since the last
# v* tag, or nothing if there is nothing to release. Semantic versioning with
# Conventional Commit subjects:
#   - breaking change ("type!:" or "BREAKING CHANGE" in the body): major,
#     or minor while the major version is 0
#   - "feat": minor
#   - anything else: patch
#   - only "docs", "ci", "chore", "test" or "style" commits: no release
# The first release is v0.1.0.
#
#   hack/next-version.sh [revision]   (default HEAD)
set -euo pipefail

rev=${1:-HEAD}
last=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' "$rev" 2>/dev/null || true)
if [[ -z $last ]]; then
  echo v0.1.0
  exit 0
fi

range="$last..$rev"
[[ -n $(git rev-list --no-merges "$range") ]] || exit 0

subjects=$(git log --no-merges --format=%s "$range")
bodies=$(git log --no-merges --format=%b "$range")
if ! grep -qvE '^(docs|ci|chore|test|style)(\(.+\))?!?:' <<<"$subjects"; then
  exit 0
fi

IFS=. read -r major minor patch <<<"${last#v}"
if grep -qE '^[a-z]+(\(.+\))?!:' <<<"$subjects" || grep -q 'BREAKING CHANGE' <<<"$bodies"; then
  if ((major == 0)); then
    minor=$((minor + 1)) patch=0
  else
    major=$((major + 1)) minor=0 patch=0
  fi
elif grep -qE '^feat(\(.+\))?:' <<<"$subjects"; then
  minor=$((minor + 1)) patch=0
else
  patch=$((patch + 1))
fi
echo "v$major.$minor.$patch"
