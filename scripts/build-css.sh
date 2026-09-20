#!/usr/bin/env bash
# Rebuilds internal/web/static/app.css from internal/web/assets/input.css.
#
# MAINTAINER STEP ONLY. It is never invoked by `go build`, `go generate` or
# `go test`: app.css and htmx.min.js are committed, so a clean checkout builds
# and tests with zero network access.
#
# Run it after changing anything under internal/web/templates/ - Tailwind scans
# those files (@source) and emits only the utility classes they actually use.
#
# Pinned versions:
#   Tailwind CSS standalone CLI  v4.3.3
#   htmx                         2.0.4  (internal/web/static/htmx.min.js,
#                                        from https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js)
set -euo pipefail

TAILWIND_VERSION="v4.3.3"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CACHE_DIR="${REPO_ROOT}/.cache"
INPUT="${REPO_ROOT}/internal/web/assets/input.css"
OUTPUT="${REPO_ROOT}/internal/web/static/app.css"

case "$(uname -s)-$(uname -m)" in
  Darwin-arm64)  ASSET="tailwindcss-macos-arm64"  ;;
  Darwin-x86_64) ASSET="tailwindcss-macos-x64"    ;;
  Linux-aarch64) ASSET="tailwindcss-linux-arm64"  ;;
  Linux-x86_64)  ASSET="tailwindcss-linux-x64"    ;;
  *) echo "unsupported platform: $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

BIN="${CACHE_DIR}/${ASSET}-${TAILWIND_VERSION}"
if [ ! -x "${BIN}" ]; then
  echo "downloading Tailwind CLI ${TAILWIND_VERSION} (${ASSET})..."
  mkdir -p "${CACHE_DIR}"
  curl -sSL --fail -o "${BIN}" \
    "https://github.com/tailwindlabs/tailwindcss/releases/download/${TAILWIND_VERSION}/${ASSET}"
  chmod +x "${BIN}"
fi

"${BIN}" --input "${INPUT}" --output "${OUTPUT}" --minify
echo "wrote ${OUTPUT} ($(wc -c < "${OUTPUT}" | tr -d ' ') bytes)"
