#!/usr/bin/env bash
# Guardrails for a public Rewind cut (ThirdCoastInteractive/Rewind).
#
#   scripts/check-release.sh              # service Dockerfiles in the worktree
#   scripts/check-release.sh --commit REF # identity + private files + Dockerfiles
#
# Exit 1 on the first failure. This is the source of truth for what a public
# commit is allowed to contain; release.sh, make release, and GHCR preflight
# all call it.
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

PUBLIC_NAME="ThirdCoast"
PUBLIC_EMAIL="git@thirdcoast.tv"

# Service images GHCR builds from a v* tag (see build-containers.yml).
# Local workstation bases (rewind-runtime-*:local) are fine under docker/.
SERVICE_DOCKERFILES=(
  rewind.Dockerfile
  ml.Dockerfile
  docker/postgres-vector.Dockerfile
)

# Paths that belong on the private repo only. Root-only; vendor copies are OK.
PRIVATE_PATHS=(AGENTS.md CLAUDE.md .grok .claude .codex .agents)
PRIVATE_PATH_GREP='^(AGENTS\.md$|CLAUDE\.md$|\.(grok|claude|codex|agents)(/|$))'

fail() { echo "FAIL: $*" >&2; exit 1; }

check_dockerfile_text() {
  local label="$1"
  local text="$2"
  local hits
  hits="$(printf '%s\n' "$text" | grep -niE '^[[:space:]]*FROM[[:space:]].*(rewind-runtime|:local([[:space:]]|$))' || true)"
  if [[ -n "$hits" ]]; then
    echo "$hits" >&2
    fail "$label FROMs a private/local image. GHCR cannot pull :local or rewind-runtime-*. Use a public base (debian, alpine, golang, nvidia/cuda, …) and install ffmpeg in the Dockerfile."
  fi
}

check_dockerfiles_worktree() {
  local f
  for f in "${SERVICE_DOCKERFILES[@]}"; do
    [[ -f "$f" ]] || fail "missing $f"
    check_dockerfile_text "$f" "$(cat "$f")"
  done
}

check_dockerfiles_commit() {
  local ref="$1"
  local f
  for f in "${SERVICE_DOCKERFILES[@]}"; do
    git cat-file -e "$ref:$f" 2>/dev/null || fail "$ref is missing $f"
    check_dockerfile_text "$ref:$f" "$(git show "$ref:$f")"
  done
}

check_private_files() {
  local ref="$1"
  local hits paths
  paths="$(git ls-tree -r --name-only "$ref")" || fail "cannot read tree: $ref"
  hits="$(printf '%s\n' "$paths" | grep -E "$PRIVATE_PATH_GREP" || true)"
  if [[ -n "$hits" ]]; then
    echo "$hits" >&2
    fail "$ref contains private agent/docs paths. Strip them before pushing public/main."
  fi
}

check_identity() {
  local ref="$1"
  local an ae cn ce
  an="$(git log -1 --format='%an' "$ref")"
  ae="$(git log -1 --format='%ae' "$ref")"
  cn="$(git log -1 --format='%cn' "$ref")"
  ce="$(git log -1 --format='%ce' "$ref")"
  [[ "$an" == "$PUBLIC_NAME" && "$ae" == "$PUBLIC_EMAIL" ]] || \
    fail "$ref author is '$an <$ae>'; public commits must be $PUBLIC_NAME <$PUBLIC_EMAIL>"
  [[ "$cn" == "$PUBLIC_NAME" && "$ce" == "$PUBLIC_EMAIL" ]] || \
    fail "$ref committer is '$cn <$ce>'; public commits must be $PUBLIC_NAME <$PUBLIC_EMAIL>"
}

strip_index() {
  git rm -r --cached -f --ignore-unmatch -- "${PRIVATE_PATHS[@]}" >/dev/null || \
    fail "cannot strip private paths from index"
}

usage() {
  echo "Usage: $0 [--commit REF | --strip-index]" >&2
  exit 2
}

if [[ $# -eq 0 ]]; then
  check_dockerfiles_worktree
  echo "OK: service Dockerfiles use public bases"
  exit 0
fi

if [[ $# -eq 1 && "$1" == "--strip-index" ]]; then
  strip_index
  exit 0
fi

if [[ $# -eq 2 && "$1" == "--commit" ]]; then
  REF="$2"
  git rev-parse -q --verify "$REF^{commit}" >/dev/null || fail "not a commit: $REF"
  check_identity "$REF"
  check_private_files "$REF"
  check_dockerfiles_commit "$REF"
  echo "OK: $REF is a legal public commit ($PUBLIC_NAME <$PUBLIC_EMAIL>, no private paths, public Docker bases)"
  exit 0
fi

usage
