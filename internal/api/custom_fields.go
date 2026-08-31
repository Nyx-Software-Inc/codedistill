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
	"net/http"
	"strconv"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
	"codedistill/internal/storage"
)

// Custom fields (backlog item #1): project-scoped typed field definitions +
// polymorphic per-item values. Reads are free; writes are write-auth gated.

var customFieldOwnerTypes = map[string]bool{
	ownerTodoItem: true, ownerBugItem: true, ownerUseCaseItem: true, ownerKnowledgeEntry: true,
}

type customFieldDefReq struct {
	Name      string   `json:"name"`
	FieldType string   `json:"field_type"`
	Options   []string `json:"options"`
	AppliesTo []string `json:"applies_to"`
	Position  int      `json:"position"`
}

// validateDefReq normalizes + validates a definition request, returning an error
// message (empty = ok).
func validateDefReq(req *customFieldDefReq) string {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return "name is required"
	}
	if req.FieldType == "" {
		req.FieldType = domain.CustomFieldText
	}
	if !domain.ValidCustomFieldType(req.FieldType) {
		return "field_type must be one of text, number, select, date"
	}
	cleanOpts := []string{}
	for _, o := range req.Options {
		if o = strings.TrimSpace(o); o != "" {
			cleanOpts = append(cleanOpts, o)
		}
	}
	req.Options = cleanOpts
	if req.FieldType == domain.CustomFieldSelect && len(req.Options) == 0 {
		return "a select field needs at least one option"
	}
	clean := []string{}
	for _, t := range req.AppliesTo {
		if customFieldOwnerTypes[t] {
			clean = append(clean, t)
		}
	}
	if len(clean) == 0 {
		return "applies_to must list at least one of: todo_item, bug_item, use_case_item, knowledge_entry"
	}
	req.AppliesTo = clean
	return ""
}

func (s *Server) listCustomFieldDefs(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	defs, err := s.store.ListCustomFieldDefs(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, defs)
}

func (s *Server) createCustomFieldDef(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req customFieldDefReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if msg := validateDefReq(&req); msg != "" {
		writeMsg(w, http.StatusBadRequest, msg)
		return
	}
	now := time.Now().UTC()
	d := &domain.CustomFieldDef{
		ID: id.New(), ProjectID: pid, Name: req.Name, FieldType: req.FieldType,
		Options: req.Options, AppliesTo: req.AppliesTo, Position: req.Position,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateCustomFieldDef(r.Context(), d); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) updateCustomFieldDef(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.GetCustomFieldDef(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req customFieldDefReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if msg := validateDefReq(&req); msg != "" {
		writeMsg(w, http.StatusBadRequest, msg)
		return
	}
	d.Name, d.FieldType, d.Options, d.AppliesTo, d.Position = req.Name, req.FieldType, req.Options, req.AppliesTo, req.Position
	d.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateCustomFieldDef(r.Context(), d); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) deleteCustomFieldDef(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCustomFieldDef(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// customFieldsView merges an item's applicable definitions with its set values.
type customFieldView struct {
	*domain.CustomFieldDef
	Value string `json:"value"`
}

func (s *Server) listItemCustomFields(w http.ResponseWriter, r *http.Request, ownerType string) {
	ownerID := r.PathValue("id")
	projectID, err := s.itemProjectID(r, ownerType, ownerID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	defs, err := s.store.ListCustomFieldDefs(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	vals, err := s.store.ListCustomFieldValues(r.Context(), ownerType, ownerID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	byID := map[string]string{}
	for _, v := range vals {
		byID[v.FieldID] = v.Value
	}
	out := []customFieldView{}
	for _, d := range defs {
		applies := false
		for _, t := range d.AppliesTo {
			if t == ownerType {
				applies = true
				break
			}
		}
		if applies {
			out = append(out, customFieldView{CustomFieldDef: d, Value: byID[d.ID]})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type setValueReq struct {
	Value string `json:"value"`
}

func (s *Server) setItemCustomField(w http.ResponseWriter, r *http.Request, ownerType string) {
	ownerID := r.PathValue("id")
	fieldID := r.PathValue("field")
	if _, err := s.itemProjectID(r, ownerType, ownerID); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	def, err := s.store.GetCustomFieldDef(r.Context(), fieldID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req setValueReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	val := strings.TrimSpace(req.Value)
	if msg := validateValue(def, val); msg != "" {
		writeMsg(w, http.StatusBadRequest, msg)
		return
	}
	v := &domain.CustomFieldValue{
		FieldID: fieldID, OwnerType: ownerType, OwnerID: ownerID,
		Value: val, UpdatedAt: time.Now().UTC(),
	}
	if err := s.store.SetCustomFieldValue(r.Context(), v); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// validateValue checks a value against its field type ("" always allowed = clear).
func validateValue(def *domain.CustomFieldDef, val string) string {
	if val == "" {
		return ""
	}
	switch def.FieldType {
	case domain.CustomFieldNumber:
		if _, err := strconv.ParseFloat(val, 64); err != nil {
			return "value must be a number"
		}
	case domain.CustomFieldSelect:
		for _, o := range def.Options {
			if o == val {
				return ""
			}
		}
		return "value must be one of the field's options"
	}
	return ""
}

// itemProjectID resolves an item's project for any supported owner type.
func (s *Server) itemProjectID(r *http.Request, ownerType, ownerID string) (string, error) {
	switch ownerType {
	case ownerTodoItem:
		it, err := s.store.GetTodoItem(r.Context(), ownerID)
		if err != nil {
			return "", err
		}
		return it.ProjectID, nil
	case ownerBugItem:
		it, err := s.store.GetBugItem(r.Context(), ownerID)
		if err != nil {
			return "", err
		}
		return it.ProjectID, nil
	case ownerUseCaseItem:
		it, err := s.store.GetUseCaseItem(r.Context(), ownerID)
		if err != nil {
			return "", err
		}
		return it.ProjectID, nil
	case ownerKnowledgeEntry:
		it, err := s.store.GetKnowledgeEntry(r.Context(), ownerID)
		if err != nil {
			return "", err
		}
		return it.ProjectID, nil
	default:
		return "", storage.ErrNotFound
	}
}
