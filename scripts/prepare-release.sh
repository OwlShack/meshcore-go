#!/usr/bin/env bash
set -euo pipefail

# Prepares a release on its release/vX.Y.Z branch: verifies every module, points the
# nested modules at the new version and names the CHANGELOG.md section for it. Tagging
# is not done here: pushing the root tag triggers .github/workflows/release.yml.

usage() {
  echo "Usage: $0 <version>"
  echo "  version: semver tag, e.g. v0.5.0"
  exit 1
}

if [ $# -ne 1 ]; then
  usage
fi

VERSION="$1"

if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Error: version must match vX.Y.Z (got: $VERSION)"
  exit 1
fi

REPO_ROOT="$(git rev-parse --show-toplevel)"

# Nested modules, each of which needs its own path-prefixed tag to be fetchable.
# release.yml tags exactly this set, so the two must stay in step.
SUBMODULES=(
  companion/transport
  hardware/transport
  hardware/sx12xx
  hardware/openhop
)

if [ -n "$(git status --porcelain)" ]; then
  echo "Error: working tree is dirty; commit or stash before releasing"
  exit 1
fi

if [ "$(git branch --show-current)" != "release/${VERSION}" ]; then
  echo "Error: run this on a release/${VERSION} branch cut from dev"
  exit 1
fi

CHANGELOG="${REPO_ROOT}/CHANGELOG.md"
if [ -z "$(awk '/^## Unreleased$/ { p = 1; next } p && /^## / { exit } p && NF' "$CHANGELOG")" ]; then
  echo "Error: CHANGELOG.md has no entries under ## Unreleased"
  exit 1
fi
PREVIOUS="$(git tag --sort=-v:refname --list 'v[0-9]*' | head -n 1)"

echo "Verifying every module..."
for module in "." "${SUBMODULES[@]}"; do
  echo "  ${module}"
  (
    cd "${REPO_ROOT}/${module}"
    go build ./...
    go vet ./...
    go test -race ./...
  )
done

echo "Updating submodule go.mod files to ${VERSION}..."
for module in "${SUBMODULES[@]}"; do
  sed -i "s|github.com/OwlShack/meshcore-go v.*|github.com/OwlShack/meshcore-go ${VERSION}|" \
    "${REPO_ROOT}/${module}/go.mod"
  git add "${REPO_ROOT}/${module}/go.mod"
done

echo "Naming the CHANGELOG.md section ${VERSION}..."
sed -i "s|^## Unreleased\$|## ${VERSION} - $(date +%F)\n\nBaseline \`${PREVIOUS}\`.|" "$CHANGELOG"
git add "$CHANGELOG"

echo "Committing..."
git commit -m "release: prepare ${VERSION}"

echo ""
echo "Release notes for ${VERSION}:"
echo "----"
"${REPO_ROOT}/scripts/release-notes.sh" "${VERSION}"
echo "----"
echo ""
echo "Done. Next steps:"
echo "  1. Add a one-line summary after \"Baseline \`${PREVIOUS}\`.\" in CHANGELOG.md and amend the commit"
echo "  2. Push release/${VERSION} and open a PR into dev"
echo "  3. Open a PR from dev into main"
echo "  4. On main's merge commit, check scripts/release-notes.sh ${VERSION} prints the notes, then"
echo "     git tag -m ${VERSION} ${VERSION} <sha> && git push origin ${VERSION}"
echo "  5. PR main back into dev as a merge commit, so dev's pseudo-versions build on ${VERSION}"
echo ""
echo "Step 4 triggers release.yml, which publishes the notes above and tags and pushes:"
for module in "${SUBMODULES[@]}"; do
  echo "  ${module}/${VERSION}"
done
