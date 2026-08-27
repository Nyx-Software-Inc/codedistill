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

package api

import (
	"context"
	"strings"

	"codedistill/internal/events"
)

// Async "Draft with AI" for acceptance criteria. The draft is a 30-45s local
// model call plus DB writes; running it inside the HTTP request meant closing
// the modal (or reloading, or closing the tab) could cancel it mid-generation
// and lose the result, with no way to tell it was even running. This runs it as
// a background job on context.Background() — decoupled from the request — and
// exposes an in-flight set so the card and the modal can show "drafting…".

func criteriaJobKey(ownerType, ownerID string) string { return ownerType + "\x00" + ownerID }

// startCriteriaDraft launches (or no-ops on) a background criteria draft for one
// item. Returns false if a draft for that item is already running, so the caller
// doesn't stack duplicates. The job clears itself and publishes ItemsChanged on
// completion so open views refresh and the "drafting…" indicator clears.
func (s *Server) startCriteriaDraft(ownerType, ownerID string) bool {
	key := criteriaJobKey(ownerType, ownerID)
	s.criteriaJobsMu.Lock()
	if s.criteriaJobs == nil {
		s.criteriaJobs = map[string]bool{}
	}
	if s.criteriaJobs[key] {
		s.criteriaJobsMu.Unlock()
		return false
	}
	s.criteriaJobs[key] = true
	s.criteriaJobsMu.Unlock()

	go func() {
		defer func() {
			s.criteriaJobsMu.Lock()
			delete(s.criteriaJobs, key)
			s.criteriaJobsMu.Unlock()
			if s.bus != nil {
				s.bus.Publish(events.ItemsChanged)
			}
		}()
		// context.Background(): the client's request context is irrelevant — the
		// whole point is that the draft survives the modal/tab closing.
		if _, err := s.agent.DraftCriteriaForItem(context.Background(), ownerType, ownerID); err != nil {
			s.log.Warn("criteria draft job failed", "owner_type", ownerType, "owner_id", ownerID, "err", err)
		}
	}()
	return true
}

// criteriaDrafting reports whether a draft is currently running for an item.
func (s *Server) criteriaDrafting(ownerType, ownerID string) bool {
	s.criteriaJobsMu.Lock()
	defer s.criteriaJobsMu.Unlock()
	return s.criteriaJobs[criteriaJobKey(ownerType, ownerID)]
}

// criteriaDraftRef is one in-flight draft, for the drafting-set endpoint.
type criteriaDraftRef struct {
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`
}

// listCriteriaDrafts returns every owner currently drafting criteria. The UI
// builds a set of owner ids from it to badge cards + the modal.
func (s *Server) listCriteriaDrafts() []criteriaDraftRef {
	s.criteriaJobsMu.Lock()
	defer s.criteriaJobsMu.Unlock()
	out := []criteriaDraftRef{}
	for key := range s.criteriaJobs {
		if i := strings.IndexByte(key, '\x00'); i >= 0 {
			out = append(out, criteriaDraftRef{OwnerType: key[:i], OwnerID: key[i+1:]})
		}
	}
	return out
}
