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
# Installed by the codedistill / codedistill-server packages to
# /etc/profile.d/codedistill.sh — puts the CodeDistill CLI on PATH for
# login shells. The systemd units use the absolute path and don't need this.
case ":$PATH:" in
  *":/opt/CodeDistill/bin:"*) ;;
  *) PATH="$PATH:/opt/CodeDistill/bin" ;;
esac
export PATH
