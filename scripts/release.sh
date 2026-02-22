#!/usr/bin/env bash
set -euo pipefail

# Release script for Rewind
# Squashes all current master content into a single release commit on main,
# then pushes to both the private (origin) and public (public) remotes.
#
# Remotes:
#   origin → ThirdCoastInteractive/rewind-dev (private, has master + main)
#   public → ThirdCoastInteractive/Rewind     (public, main only)
#
# Usage: ./scripts/release.sh "v1.0.0 - Initial release"
#        ./scripts/release.sh  (defaults to "Release $(date)")

RELEASE_MSG="${1:-Release $(date +%Y-%m-%d)}"

# Public-facing identity (no personal info)
export GIT_AUTHOR_NAME="ThirdCoast"
export GIT_AUTHOR_EMAIL="git@thirdcoast.tv"
export GIT_COMMITTER_NAME="ThirdCoast"
export GIT_COMMITTER_EMAIL="git@thirdcoast.tv"

echo "==> Preparing release: $RELEASE_MSG"

# Ensure we're in a git repo
if ! git rev-parse --is-inside-work-tree &>/dev/null; then
  echo "ERROR: Not inside a git repository" >&2
  exit 1
fi

# Ensure working tree is clean
if ! git diff-index --quiet HEAD --; then
  echo "ERROR: Working tree has uncommitted changes. Commit or stash first." >&2
  exit 1
fi

# Save current branch to return to it later
ORIGINAL_BRANCH=$(git symbolic-ref --short HEAD)

# Fetch latest from origin
echo "==> Fetching latest from origin..."
git fetch origin

# Get the tree (snapshot) of master
echo "==> Reading master tree..."
MASTER_TREE=$(git rev-parse master^{tree})

# Check if main exists locally, create or update
if git show-ref --verify --quiet refs/heads/main; then
  echo "==> Updating existing main branch..."
  # Get current main commit to check if tree changed
  MAIN_TREE=$(git rev-parse main^{tree})
  if [ "$MASTER_TREE" = "$MAIN_TREE" ]; then
    echo "Nothing to release — main already matches master content."
    exit 0
  fi
  # Create a new commit on main with master's tree
  PARENT=$(git rev-parse main)
  COMMIT=$(git commit-tree "$MASTER_TREE" -p "$PARENT" -m "$RELEASE_MSG")
else
  echo "==> Creating new main branch..."
  # First release — orphan commit
  COMMIT=$(git commit-tree "$MASTER_TREE" -m "$RELEASE_MSG")
fi

# Point main at the new commit
git update-ref refs/heads/main "$COMMIT"

echo "==> Pushing main to origin (private) and public..."
git push --force origin main
git push --force public main

# Merge release commit back into master so it appears in dev history
echo "==> Merging release into master..."
git checkout master --quiet
git merge main --no-ff --allow-unrelated-histories -m "Merge release: $RELEASE_MSG" --quiet
git push origin master

# Return to original branch
git checkout "$ORIGINAL_BRANCH" --quiet

echo ""
echo "✓ Released to main: $RELEASE_MSG"
echo "  Release commit: $(git rev-parse --short main)"
echo "  Merged into master: $(git rev-parse --short master)"
echo "  Pushed to: origin (private) + public"
