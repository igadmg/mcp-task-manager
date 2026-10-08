#!/usr/bin/env bash
# Mirrors the plugin's phase skills into .claude/skills/ for dogfooding.
#
# plugins/mcp-task-manager/skills/ is the only place to edit skills: it is the
# package both marketplaces install. .claude/skills/ is a generated copy, so
# Claude Code sessions in this repository use the same skills without
# installing the plugin (and without its second, binary-backed MCP server).
# A copy rather than a symlink, because symlinks do not survive Windows
# checkouts with core.symlinks=false.
#
# Usage:
#   scripts/sync-skills.sh          # rewrite .claude/skills/ from the plugin
#   scripts/sync-skills.sh --check  # exit 1 if .claude/skills/ has drifted
set -euo pipefail

# superpowers-workflow is left out on purpose: this repository runs the
# phase workflow on itself.
SKILLS=(begin_task research design planning implementation workflow)

# Skills that live only in this repository: they drive development *of* this
# repository (building and running the binary from source), so they are not
# part of the installed package. Not mirrored, and kept across a sync.
LOCAL_ONLY=(open_board)

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="${REPO_ROOT}/plugins/mcp-task-manager/skills"
DST="${REPO_ROOT}/.claude/skills"

if [[ "${1:-}" == "--check" ]]; then
  status=0
  for s in "${SKILLS[@]}"; do
    if ! diff -r "${SRC}/${s}" "${DST}/${s}" >/dev/null 2>&1; then
      echo "drifted: .claude/skills/${s} (edit plugins/mcp-task-manager/skills/${s}, then run scripts/sync-skills.sh)" >&2
      status=1
    fi
  done
  for d in "${DST}"/*/; do
    [[ -d "$d" ]] || continue
    name="$(basename "$d")"
    if [[ " ${LOCAL_ONLY[*]} " == *" ${name} "* ]]; then
      continue
    fi
    if [[ ! " ${SKILLS[*]} " == *" ${name} "* ]]; then
      echo "unexpected: .claude/skills/${name} is not mirrored from the plugin" >&2
      status=1
    fi
  done
  exit "$status"
fi

mkdir -p "${DST}"
for d in "${DST}"/*/; do
  [[ -d "$d" ]] || continue
  name="$(basename "$d")"
  [[ " ${LOCAL_ONLY[*]} " == *" ${name} "* ]] && continue
  rm -rf "$d"
done
for s in "${SKILLS[@]}"; do
  cp -R "${SRC}/${s}" "${DST}/${s}"
done
echo "synced ${#SKILLS[@]} skills into .claude/skills/"
