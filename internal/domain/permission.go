// =============================================================================
//  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//  CodeDistill
//
//  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//  Public License v3.0 (see the LICENSE file) and, separately, a commercial
//  license available from Nyx Software, Inc. Use outside the terms of one of those
//  licenses is prohibited.
//
//  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
// =============================================================================

package domain

import "time"

// Permissions (docs/design/workflows-and-roles.md, Part 2).
//
// Code asks for a permission, never for a role: "may this person manage
// everyone's jobs?", not "is this person an admin?". A role is a named set of
// permissions; the built-in ones ship with the product and an owner can change
// them or make others.
const (
	// PermUseProduct: work in projects (items, reviews, analysis) and run
	// workflows. An account takes a license seat if and only if it has this.
	PermUseProduct = "use_product"
	// PermManageOwnJobs: pause, resume, cancel and reprioritise (up to High)
	// the jobs one started.
	PermManageOwnJobs = "manage_own_jobs"
	// PermManageAllJobs: the same for anyone's jobs, and use Immediate.
	PermManageAllJobs = "manage_all_jobs"
	// PermDesignWorkflows: create, edit, enable and disable workflows, and
	// choose the models their steps run on.
	PermDesignWorkflows = "design_workflows"
	// PermManageMembers: invite, deactivate and reactivate members, and give
	// them roles. Making someone an owner also needs to be an owner.
	PermManageMembers = "manage_members"
	// PermAdminister: the installation itself: models and their API keys, job
	// slots, server-wide settings, the license, security knowledge, system
	// information, the server's API token.
	PermAdminister = "administer"
)

// Permissions lists every permission, in the order the admin console shows
// them.
var Permissions = []string{
	PermUseProduct, PermManageOwnJobs, PermManageAllJobs,
	PermDesignWorkflows, PermManageMembers, PermAdminister,
}

// ValidPermission reports whether p is a known permission.
func ValidPermission(p string) bool {
	for _, q := range Permissions {
		if q == p {
			return true
		}
	}
	return false
}

// Built-in roles.
//
// Owner is accountable for the installation: it holds every permission except
// using the product, which is what a seat pays for. An owner who also works in
// projects holds Member (or Lead) as well; one who only administers (the
// person who installed the server) takes no seat.
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleLead   = "lead"
	RoleMember = "member"
)

// Role is a named set of permissions.
type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Builtin     bool      `json:"builtin"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PermSet is what a person may do: the union of their roles' permissions.
type PermSet map[string]bool

// Has reports whether the set holds p.
func (s PermSet) Has(p string) bool { return s[p] }

// List is the set as a sorted slice, in console order.
func (s PermSet) List() []string {
	out := []string{}
	for _, p := range Permissions {
		if s[p] {
			out = append(out, p)
		}
	}
	return out
}

// AllPermissions is the set for a caller trusted with everything: the
// single-user desktop's local user, and anyone presenting the server's own API
// token.
func AllPermissions() PermSet {
	s := PermSet{}
	for _, p := range Permissions {
		s[p] = true
	}
	return s
}

// OwnerPermissions is what the owner role always holds, whatever an edit to it
// says: every permission but using the product. An owner can never lock
// themselves out of administering the installation.
func OwnerPermissions() []string {
	var out []string
	for _, p := range Permissions {
		if p != PermUseProduct {
			out = append(out, p)
		}
	}
	return out
}
