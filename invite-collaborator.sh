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

# Invites someone to the private full-source repo.
#
#   ./invite-collaborator.sh <github-username> [permission]
#
# permission defaults to `push` — clone, push branches, open PRs. That is what
# a collaborator needs; see the note at the bottom about why it also lets them
# do the one thing they should not.
#
# This is the same thing as Settings -> Collaborators -> Add people, with the
# checks a web form does not do: that the account exists, that the target really
# is private, and that you are not re-inviting someone who already has access.

set -euo pipefail

REPO="${CODEDISTILL_GITHUB_REPO:-Nyx-Software-Inc/codedistill-private}"
USER_NAME="${1:-}"
PERM="${2:-push}"

if [ -z "$USER_NAME" ]; then
  echo "usage: ./invite-collaborator.sh <github-username> [pull|triage|push|maintain|admin]" >&2
  echo "  repo: ${REPO}  (override with CODEDISTILL_GITHUB_REPO)" >&2
  exit 2
fi

command -v gh >/dev/null 2>&1 || { echo "gh is not installed" >&2; exit 1; }
gh auth status >/dev/null 2>&1 || { echo "not signed in — run 'gh auth login'" >&2; exit 1; }

case "$PERM" in
  pull|triage|push|maintain|admin) ;;
  *) echo "unknown permission '${PERM}' — use pull, triage, push, maintain or admin" >&2; exit 2 ;;
esac

# The repo must be PRIVATE. Inviting someone to a public repo is harmless, but
# being wrong about WHICH repo this is means the rest of the message below —
# "here is the full source" — is wrong too, and this org has a public
# `codedistill` sitting one character away from the private one.
VIS="$(gh api "repos/${REPO}" --jq '.private' 2>/dev/null)" || {
  echo "cannot read ${REPO} — check the name and that you have access" >&2; exit 1; }
if [ "$VIS" != "true" ]; then
  echo "refusing: ${REPO} is PUBLIC." >&2
  echo "  This script invites people to the private full-source repo; a public one" >&2
  echo "  needs no invitation and this is probably the wrong repository." >&2
  exit 1
fi

gh api "users/${USER_NAME}" --jq '.login' >/dev/null 2>&1 || {
  echo "no GitHub account named '${USER_NAME}'" >&2; exit 1; }

# Never target yourself. This is an invite script, and the only thing pointing
# it at your own account can do is take access away — which it will do silently
# if you are a plain collaborator rather than an org owner.
ME="$(gh api user --jq .login 2>/dev/null || true)"
if [ "${USER_NAME,,}" = "${ME,,}" ]; then
  echo "refusing: ${USER_NAME} is you." >&2
  echo "  Inviting yourself can only lower your own access." >&2
  exit 1
fi

# rank turns a permission into a number so "already has enough" is a comparison
# rather than a string match. GitHub reports write/read where it accepts
# push/pull, and those pairs are the same thing.
rank() {
  case "$1" in
    admin)            echo 5 ;;
    maintain)         echo 4 ;;
    push|write)       echo 3 ;;
    triage)           echo 2 ;;
    pull|read)        echo 1 ;;
    *)                echo 0 ;;
  esac
}

# Already in? Say so rather than sending a second invitation, which GitHub
# accepts silently and which reads to the recipient as a mistake.
#
# Only ever ESCALATES. A repo grant does not override an org role — asking to
# set an owner to `push` reports success and changes nothing, which is a lie
# told confidently. Lowering someone's access is a deliberate act and belongs in
# the web UI where it says what it is doing.
CURRENT="$(gh api "repos/${REPO}/collaborators/${USER_NAME}/permission" --jq '.permission' 2>/dev/null || true)"
if [ -n "$CURRENT" ] && [ "$CURRENT" != "none" ]; then
  if [ "$(rank "$CURRENT")" -ge "$(rank "$PERM")" ]; then
    echo "==> ${USER_NAME} already has ${CURRENT} on ${REPO} — nothing to do"
    echo "    (to REDUCE access, use the web UI: it will tell you what it is changing)"
    exit 0
  fi
  echo "==> ${USER_NAME} currently has ${CURRENT}; raising to ${PERM}"
fi

PENDING="$(gh api "repos/${REPO}/invitations" --jq ".[] | select(.invitee.login==\"${USER_NAME}\") | .id" 2>/dev/null || true)"
if [ -n "$PENDING" ]; then
  echo "==> ${USER_NAME} already has an invitation pending on ${REPO}"
  echo "    They need to accept it: https://github.com/${REPO}/invitations"
  exit 0
fi

gh api -X PUT "repos/${REPO}/collaborators/${USER_NAME}" -f permission="${PERM}" >/dev/null

echo "==> invited ${USER_NAME} to ${REPO} with ${PERM}"
echo
echo "  They get an email and must ACCEPT before they can clone:"
echo "    https://github.com/${REPO}/invitations"
echo
echo "  Tell them the one rule this setup depends on:"
echo
echo "    Branch and open a PR. Do NOT press Merge on GitHub."
echo
echo "  This repo is published from our own repository, one snapshot commit per"
echo "  publish. A PR merged on GitHub is a commit we do not have, and the next"
echo "  publish would refuse rather than overwrite it — recoverable, but it stops"
echo "  publishing until someone fetches the branch and merges it on our side."
echo "  That is what ./merge-pr.sh does."
