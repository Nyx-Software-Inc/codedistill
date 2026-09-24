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

# Publishes the FULL source tree to a PRIVATE GitHub repo, for collaborators.
#
# Same snapshot-commit model as the CE mirror (publish-oss-mirror.sh): each run
# becomes ONE commit on the target's main branch, parented on the previous
# publish. GitHub accumulates a real, diffable history; this repository's
# history — every message, branch and false start — stays here.
#
#   PRIVATE_MIRROR_REMOTE=https://user:token@github.com/org/repo \
#     scripts/publish-private-mirror.sh
#
# This repository stays the source of truth. The flow is one-way:
#
#   here ──publish──▶ GitHub main        (this script)
#   here ◀──fetch──── GitHub slavko/*    (git fetch + merge, by hand)
#
# A collaborator branches, pushes, opens a PR. You fetch the BRANCH here, merge
# it here — which keeps their real commits and authorship — and the next publish
# carries it back. Their work does a round trip; this repository stays a strict
# superset of GitHub, which is what makes moving the source of truth to GitHub
# later a push rather than a reconstruction.
#
# DO NOT merge a PR on GitHub. See the "main has moved" guard below.

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
: "${PRIVATE_MIRROR_REMOTE:?set PRIVATE_MIRROR_REMOTE to the push URL (with credentials)}"

OUT="${1:-dist/private-mirror}"
REF="${PUBLISH_REF:-HEAD}"

cd "$ROOT"

SHA="$(git rev-parse --short "$REF")"
SUBJECT="$(git log -1 --format=%s "$REF")"
V="$(cat "$ROOT/VERSION")"

# ─── Gate 1: the target must be PRIVATE ──────────────────────────────────────
#
# The mirror image of the CE publisher's leak check. That one asks whether the
# tree is clean enough to be public; this one asks whether the target is private
# enough for the tree, because the tree is everything — paid features, business
# docs, the licensing internals.
#
# Two publishers live in this repository, one aimed at a public repo and one at
# a private one, and they differ by an environment variable. A mis-set variable
# must fail here, not be discovered afterwards by someone reading the news.
SLUG="$(sed -E 's#^.*github\.com[:/]##; s#\.git$##' <<<"$PRIVATE_MIRROR_REMOTE")"
if command -v gh >/dev/null 2>&1; then
  # gh writes its error body to STDOUT, so a 404 would otherwise be captured as
  # the "visibility" and reported back to the user as noise.
  VIS="$(gh api "repos/${SLUG}" --jq '.private' 2>/dev/null)" || VIS='unknown'
  case "$VIS" in true|false) ;; *) VIS='unknown' ;; esac
  case "$VIS" in
    true) ;;
    false)
      echo "publish ABORTED: ${SLUG} is PUBLIC and this tree is the full commercial source." >&2
      exit 1 ;;
    *)
      # Not knowing is not permission. A network blip must not be the reason
      # the full source lands somewhere unverified.
      echo "publish aborted: could not confirm ${SLUG} is private (gh said '${VIS}')" >&2
      echo "  authenticate with 'gh auth login', or set PRIVATE_MIRROR_FORCE=1 if you are certain." >&2
      [ "${PRIVATE_MIRROR_FORCE:-}" = "1" ] || exit 1 ;;
  esac
else
  echo "publish aborted: gh is not installed, so the target's visibility cannot be checked" >&2
  [ "${PRIVATE_MIRROR_FORCE:-}" = "1" ] || exit 1
fi

# ─── Export the tree at REF ──────────────────────────────────────────────────
#
# git archive, never the working copy: an uncommitted experiment must not reach
# a collaborator, and a publish should be reproducible from a SHA.
rm -rf "$ROOT/$OUT"
mkdir -p "$ROOT/$OUT"
git archive "$REF" | tar -x -C "$ROOT/$OUT"

cd "$ROOT/$OUT"
rm -rf .git
git init -q -b main
git config user.name  "CodeDistill Publish"
git config user.email "publish@codedistill.invalid"
git remote add origin "$PRIVATE_MIRROR_REMOTE"

# ─── Gate 2: GitHub's main must not have moved since the last publish ────────
#
# THE failure this guard exists for: a PR merged on GitHub puts a commit there
# that is not here. The next publish parents its snapshot on that merge and
# writes OUR tree — which lacks the merged change. The publish silently reverts
# the collaborator's work, and it looks like an ordinary commit rather than a
# revert. That is a bad afternoon, discovered late.
#
# State lives on GitHub as the `last-publish` tag rather than in a local file:
# a file can be lost with a workspace, and the question is about GitHub anyway.
FIRST=0
if git fetch -q origin main 2>/dev/null; then
  # Captured BEFORE anything else is fetched. FETCH_HEAD is a single file that
  # every fetch overwrites, so reading it after the tag fetch below compares the
  # tag against itself — a guard that can never fire. That bug shipped once and
  # was caught only by deliberately staging the disaster it was meant to stop.
  MAIN_SHA="$(git rev-parse FETCH_HEAD)"
  git reset -q --soft FETCH_HEAD
  if git fetch -q origin 'refs/tags/last-publish:refs/tags/last-publish' 2>/dev/null; then
    LAST_SHA="$(git rev-parse last-publish^{commit})"
    if [ "$MAIN_SHA" != "$LAST_SHA" ]; then
      echo "publish ABORTED: ${SLUG} main has moved since the last publish." >&2
      echo >&2
      echo "  last published: ${LAST_SHA}" >&2
      echo "  main is now:    ${MAIN_SHA}" >&2
      echo >&2
      echo "  Something was merged on GitHub. Publishing now would overwrite it" >&2
      echo "  with this tree and silently revert it. Bring it here first:" >&2
      echo >&2
      echo "    git fetch <github-remote> main" >&2
      echo "    git log --oneline ${LAST_SHA}..FETCH_HEAD    # what is only there" >&2
      echo >&2
      echo "  then merge it here and publish again." >&2
      exit 1
    fi
  else
    # main exists but the tag does not: a repo published before this guard, or
    # a tag deleted by hand. Refuse rather than assume — the whole point is not
    # to overwrite work nobody remembered was there.
    echo "publish aborted: ${SLUG} has a main branch but no last-publish tag," >&2
    echo "  so there is no way to tell whether it carries unpublished work." >&2
    echo "  Check it, then set PRIVATE_MIRROR_FORCE=1 to publish anyway." >&2
    [ "${PRIVATE_MIRROR_FORCE:-}" = "1" ] || exit 1
  fi
else
  FIRST=1
fi

# ─── The snapshot ────────────────────────────────────────────────────────────
#
# The message carries this repository's commit subject and SHA, unlike the CE
# publisher's fixed string — a public mirror should not leak commit subjects,
# but a collaborator's repo reads far better with them, and the SHA is the
# correspondence back to the history that stayed here.
git add -A
if git commit -q -m "${SUBJECT}" -m "CodeDistill v${V} — source commit ${SHA}" >/dev/null 2>&1; then
  git push -q origin HEAD:main
  git tag -f last-publish >/dev/null
  git push -qf origin last-publish
  if [ "$FIRST" = "1" ]; then
    echo "==> published the first snapshot of v${V} (${SHA}) to ${SLUG}"
  else
    echo "==> published v${V} (${SHA}) to ${SLUG}"
  fi
else
  echo "==> no changes since the last publish — nothing pushed"
fi
