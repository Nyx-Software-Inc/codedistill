#!/usr/bin/env bash
# =============================================================================
#  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
#
#  CodeDistill
#
#  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
#  Public License v3.0 (see the LICENSE file) and, separately, a commercial
#  license available from Nyx Software, Inc. Use outside the terms of one of those
#  licenses is prohibited.
#
#  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
# =============================================================================

# Brings a collaborator's pull request into THIS repository.
#
#   ./merge-pr.sh <pr-number> [--publish]
#
# The private GitHub repo is a published mirror, not the source of truth, so a
# PR cannot be merged there — a merge on GitHub is a commit we do not have, and
# the publisher refuses to run until it is brought across. This script is the
# bringing across:
#
#   1. fetch the PR's commits
#   2. REPLAY them onto main, keeping their authorship
#   3. build and test the result
#   4. --publish: publish the snapshot and close the PR
#
# Step 2 is a cherry-pick, not a merge, and that is not a style preference. A
# collaborator branches from GitHub's main — a SNAPSHOT commit, which exists
# only there and shares no ancestry with this repository. `git merge-base` on
# such a branch returns nothing. Merging it would be merging unrelated
# histories: git refuses outright, and forcing it splices two trees that only
# coincidentally match. Replaying the diff is the operation that matches what
# actually happened — they changed X relative to what we published, so apply X
# to what we have.
#
# Step 3 is why this is a script rather than two git commands. A PR that does
# not compile must not sit on main unnoticed, and the undo is printed rather
# than performed, because whether to back out or fix forward is yours.
#
# GitHub will never mark the PR merged on its own: what lands there is a
# snapshot commit containing the change, not the collaborator's commits. So the
# PR is closed explicitly, with a comment saying where the work went.

set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

REPO="${CODEDISTILL_GITHUB_REPO:-Nyx-Software-Inc/codedistill-private}"
PR="${1:-}"
PUBLISH=0
[ "${2:-}" = "--publish" ] && PUBLISH=1

if [ -z "$PR" ]; then
  echo "usage: ./merge-pr.sh <pr-number> [--publish]" >&2
  echo "  repo: ${REPO}  (override with CODEDISTILL_GITHUB_REPO)" >&2
  echo >&2
  echo "  open pull requests:" >&2
  gh pr list --repo "$REPO" --state open 2>/dev/null >&2 || true
  exit 2
fi

command -v gh >/dev/null 2>&1 || { echo "gh is not installed" >&2; exit 1; }
gh auth status >/dev/null 2>&1 || { echo "not signed in — run 'gh auth login'" >&2; exit 1; }

# ─── Refuse to start from a state where the result would be ambiguous ────────
BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [ "$BRANCH" != "main" ]; then
  echo "refusing: you are on '${BRANCH}', not main." >&2
  echo "  The mirror publishes main; merging a PR anywhere else would not reach it." >&2
  exit 1
fi
if [ -n "$(git status --porcelain)" ]; then
  echo "refusing: the working tree has uncommitted changes." >&2
  echo "  A merge on top of them would be impossible to tell apart afterwards." >&2
  git status --short >&2
  exit 1
fi

# ─── What we are about to take ──────────────────────────────────────────────
INFO="$(gh pr view "$PR" --repo "$REPO" --json title,author,state,headRefName,url 2>/dev/null)" || {
  echo "no pull request #${PR} on ${REPO}" >&2; exit 1; }
TITLE="$(jq -r .title <<<"$INFO")"
AUTHOR="$(jq -r .author.login <<<"$INFO")"
STATE="$(jq -r .state <<<"$INFO")"
HEADREF="$(jq -r .headRefName <<<"$INFO")"
URL="$(jq -r .url <<<"$INFO")"

if [ "$STATE" != "OPEN" ]; then
  echo "refusing: PR #${PR} is ${STATE}, not open." >&2
  if [ "$STATE" = "MERGED" ]; then
    echo "  It was merged ON GITHUB, which this setup cannot use — the publisher" >&2
    echo "  will refuse until GitHub's main is brought back here. Fetch it by hand:" >&2
    echo "    git fetch github main && git merge FETCH_HEAD" >&2
  fi
  exit 1
fi

echo "PR #${PR}: ${TITLE}"
echo "  by ${AUTHOR}, from ${HEADREF}"
echo "  ${URL}"
echo

# A named remote, created once, so ordinary git commands work here afterwards.
# No credentials in the URL: gh's git credential helper supplies them, and a
# token baked into .git/config outlives the reason it was there.
if ! git remote get-url github >/dev/null 2>&1; then
  git remote add github "https://github.com/${REPO}.git"
  echo "==> added remote 'github' -> ${REPO}"
fi

# refs/pull/N/head rather than the branch name: it works for a PR from a fork
# as well as one from a branch in the repo, and it is what the PR actually
# points at even if the author force-pushes later.
git fetch -q github "refs/pull/${PR}/head:refs/heads/pr-${PR}" --force
echo "==> fetched ${PR} as local branch pr-${PR}"

BEFORE="$(git rev-parse HEAD)"

# Where the branch left our published tree. Their commits sit on top of a
# snapshot commit; everything from there to the tip is theirs.
git fetch -q github main
FORK="$(git merge-base github/main "pr-${PR}" 2>/dev/null || true)"
if [ -z "$FORK" ]; then
  echo "cannot tell where PR #${PR} branched from GitHub's main." >&2
  echo "  Inspect it by hand:  git log --oneline github/main..pr-${PR}" >&2
  exit 1
fi
COUNT="$(git rev-list --count "${FORK}..pr-${PR}")"
echo
git --no-pager log --oneline --no-decorate --format='    %h %an: %s' "${FORK}..pr-${PR}"
echo
if [ "$COUNT" = "0" ]; then
  echo "PR #${PR} has no commits of its own — nothing to bring across." >&2
  exit 1
fi

# Cherry-pick, for the reason at the top of this file: their commits are
# parented on a snapshot that is not in our history, so there is nothing to
# merge WITH. Each commit keeps its author; you become the committer, which is
# exactly what happened.
echo "==> replaying ${COUNT} commit(s) onto main"
if ! git cherry-pick -x "${FORK}..pr-${PR}"; then
  echo >&2
  echo "CONFLICT replaying PR #${PR}." >&2
  echo >&2
  echo "  Their change was made against what we published; main has moved since" >&2
  echo "  in a way that touches the same lines. Resolve and continue:" >&2
  echo "    git add -A && git cherry-pick --continue" >&2
  echo >&2
  echo "  Or give up on it cleanly:" >&2
  echo "    git cherry-pick --abort" >&2
  exit 1
fi
AFTER="$(git rev-parse --short HEAD)"
echo "==> replayed onto main, now at ${AFTER}"
echo

# ─── Does it actually work? ─────────────────────────────────────────────────
#
# Printed, not performed: backing out and fixing forward are both reasonable
# and only you know which. But main must never be left broken silently.
FAILED=""
echo "==> building"
go build ./... || FAILED="go build"
if [ -z "$FAILED" ]; then
  echo "==> testing"
  go test ./... >/dev/null || FAILED="go test"
fi
if [ -z "$FAILED" ] && [ -d web/node_modules ]; then
  echo "==> checking the front end"
  (cd web && npm run check >/dev/null 2>&1) || FAILED="svelte-check"
fi

if [ -n "$FAILED" ]; then
  echo >&2
  echo "${FAILED} FAILED on the merged tree. main is not publishable." >&2
  echo >&2
  echo "  Fix forward, or undo the merge with:" >&2
  echo "    git reset --hard ${BEFORE}" >&2
  echo >&2
  echo "  Re-run the failing step by hand to see why." >&2
  exit 1
fi
echo "==> green"
echo

if [ "$PUBLISH" != "1" ]; then
  echo "Not published. When you are ready:"
  echo "    ./merge-pr.sh ${PR} --publish     # or publish by hand, then close the PR"
  exit 0
fi

# ─── Publish, then close ────────────────────────────────────────────────────
#
# In that order. The PR is only closed once GitHub's main actually carries the
# work; closing first would tell the author it landed while it had not.
if [ -z "${PRIVATE_MIRROR_REMOTE:-}" ]; then
  TOKEN="$(gh auth token 2>/dev/null || true)"
  GHUSER="$(gh api user --jq .login 2>/dev/null || true)"
  if [ -z "$TOKEN" ] || [ -z "$GHUSER" ]; then
    echo "cannot publish: set PRIVATE_MIRROR_REMOTE, or sign in with gh" >&2
    exit 1
  fi
  export PRIVATE_MIRROR_REMOTE="https://${GHUSER}:${TOKEN}@github.com/${REPO}.git"
fi

# The publisher's own guard will stop this if GitHub's main moved for some other
# reason — another PR merged there while we worked. Let it.
scripts/publish-private-mirror.sh dist/private-mirror

gh pr comment "$PR" --repo "$REPO" --body \
"Merged into the source repository and published here as a snapshot commit.

GitHub cannot mark this merged: what lands on \`main\` is one snapshot per publish, carrying the change but not the individual commits. Your commits are preserved with your authorship in the repository of record.

Closing — the work is in \`main\`."
gh pr close "$PR" --repo "$REPO" >/dev/null

echo "==> published and closed PR #${PR}"
echo
echo "  Still to do: push to the NAS —  git push origin main"
