#!/usr/bin/env bash
# Exercise the real guard against synthetic commits; never use the dev index.
set -euo pipefail
guard="$(cd "$(dirname "$0")" && pwd)/check-release.sh"
temp_base="$(cd "${TMPDIR:-/tmp}" && pwd -P)"
fixture="$(mktemp -d "$temp_base/rewind-release.XXXXXXXX")"
fixture="$(cd "$fixture" && pwd -P)"
cleanup() {
  # Only remove the verified, isolated fixture, never a caller's checkout.
  if [[ "$fixture" == "$temp_base"/rewind-release.* ]]; then
    rm -rf -- "$fixture"
  fi
}
trap cleanup EXIT
unset GIT_INDEX_FILE GIT_DIR GIT_WORK_TREE
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
export GIT_AUTHOR_NAME=ThirdCoast GIT_AUTHOR_EMAIL=git@thirdcoast.tv
export GIT_COMMITTER_NAME=ThirdCoast GIT_COMMITTER_EMAIL=git@thirdcoast.tv
cd "$fixture"
git init -q
git config core.autocrlf false
git config core.excludesFile /dev/null
git config commit.gpgsign false
files=(rewind.Dockerfile ml.Dockerfile docker/postgres-vector.Dockerfile)
mkdir -p docker
for f in "${files[@]}"; do printf 'FROM alpine:3.21\n' > "$f"; done
git add .
commit() { git commit-tree "$(git write-tree)" -m fixture; }
checks=0
expect() {
  local expected="$1" label="$2" actual=0
  shift 2
  "$@" > "$fixture/result.log" 2>&1 || actual=$?
  if [[ "$actual" != "$expected" ]]; then
    cat "$fixture/result.log" >&2
    echo "FAIL: $label (expected $expected, got $actual)" >&2
    exit 1
  fi
  checks=$((checks + 1))
  echo "PASS: $label"
}
baseline="$(commit)"
expect 0 'public commit' bash "$guard" --commit "$baseline"
expect 0 'public worktree' bash "$guard"
expect 1 'invalid commit' bash "$guard" --commit no-such-commit
expect 2 'unknown option' bash "$guard" --unknown
bad_author="$(GIT_AUTHOR_NAME=Private commit)"
expect 1 'private author' bash "$guard" --commit "$bad_author"
bad_committer="$(GIT_COMMITTER_EMAIL=private@example.com commit)"
expect 1 'private committer' bash "$guard" --commit "$bad_committer"

for p in AGENTS.md CLAUDE.md .grok/skills/test.md .claude/settings.json .codex/skills/test.md .agents/skills/test.md; do
  mkdir -p "$(dirname "$p")"
  printf 'private\n' > "$p"
  git add -- "$p"
  expect 1 "reject $p" bash "$guard" --commit "$(commit)"
  expect 0 "strip $p" bash "$guard" --strip-index
  [[ -f "$p" ]] || { echo "FAIL: strip removed working file $p"; exit 1; }
  [[ "$(git write-tree)" == "$(git rev-parse "$baseline^{tree}")" ]] || exit 1
done
mkdir -p vendor/example
printf 'public vendor documentation\n' > vendor/example/AGENTS.md
printf 'public example\n' > AGENTS.md.example
git add vendor AGENTS.md.example
expect 0 'nested docs and similarly named public files' bash "$guard" --commit "$(commit)"
git read-tree "$baseline"

for f in "${files[@]}"; do
  printf '  from --platform=linux/amd64 example:local AS builder\n' > "$f"
  expect 1 "local base in worktree: $f" bash "$guard"
  git add "$f"
  expect 1 "local base in commit: $f" bash "$guard" --commit "$(commit)"
  printf 'FROM alpine:3.21\n' > "$f"
  git add "$f"
done
printf 'FROM rewind-runtime-ffmpeg:stable\n' > rewind.Dockerfile
expect 1 'private runtime with non-local tag' bash "$guard"
printf 'FROM alpine:3.21\n' > rewind.Dockerfile
git rm --cached -q rewind.Dockerfile
expect 1 'missing published Dockerfile' bash "$guard" --commit "$(commit)"
git add rewind.Dockerfile

# A locked index must fail visibly instead of reporting a successful strip.
printf 'private\n' > AGENTS.md
git add AGENTS.md
: > .git/index.lock
expect 1 'locked index' bash "$guard" --strip-index
rm .git/index.lock
echo "OK: $checks release guard checks passed"
