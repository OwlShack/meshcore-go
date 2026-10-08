#!/usr/bin/env bash
set -euo pipefail

# Prints the CHANGELOG.md section for a tag, from "## <tag>" up to the next "## " heading.

if [ $# -ne 1 ]; then
  echo "Usage: $0 <tag>" >&2
  exit 1
fi

awk -v tag="$1" '
  BEGIN { gsub(/\./, "\\.", tag) }
  $0 ~ "^## " tag "( |$)" { p = 1; next }
  p && /^## / { exit }
  p
' "$(git rev-parse --show-toplevel)/CHANGELOG.md"
