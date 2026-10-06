#!/usr/bin/env bash
set -euo pipefail

# Prepares a release by verifying every module, then pointing the nested modules
# at the new version. Tagging is not done here: pushing the root tag triggers
# .github/workflows/release.yml, which creates and pushes the nested tags.

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

echo "Committing dependency update..."
git commit -m "release: update submodule dependencies to ${VERSION}"

echo ""
echo "Done. Next steps:"
echo "  1. git push origin dev"
echo "  2. Merge dev into main (PR or fast-forward)"
echo "  3. git checkout main && git tag ${VERSION} && git push origin main --tags"
echo ""
echo "Step 3 triggers release.yml, which tags and pushes:"
for module in "${SUBMODULES[@]}"; do
  echo "  ${module}/${VERSION}"
done
