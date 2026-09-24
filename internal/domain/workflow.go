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

// Workflow is a kind of job, defined as data rather than code.
//
// Its id matches jobs.type, so a run points back at the definition that
// produced it. Built-ins are ordinary rows carrying builtin=1 — which is the
// test of whether user-defined workflows will actually work, rather than a
// second mechanism bolted alongside.
type Workflow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Builtin definitions are not deletable and their steps are not editable:
	// decompose's four layers are code, and offering an edit that silently
	// does nothing is worse than offering none.
	Builtin bool `json:"builtin"`

	// Resumable means a stopped run continues where it left off rather than
	// starting over. Declared, not inferred, because the UI must not offer a
	// Resume button that quietly restarts from zero.
	Resumable bool `json:"resumable"`

	Enabled bool           `json:"enabled"`
	Steps   []WorkflowStep `json:"steps"`

	// Params are what the submission dialog asks for. Empty means the workflow
	// needs nothing beyond a project, which is a real answer.
	Params []WorkflowParam `json:"params"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WorkflowStep is one worker the workflow needs.
type WorkflowStep struct {
	Ordinal    int    `json:"ordinal"`
	WorkerType string `json:"worker_type"`
	Label      string `json:"label,omitempty"`

	// NeedsContext is what this step must be able to read. Compared against the
	// chosen provider's window so a mismatch surfaces at configuration time
	// rather than as a silently truncated answer forty minutes into a run.
	NeedsContext int `json:"needs_context,omitempty"`

	// ProviderID is the model serving this step. Empty falls back to the global
	// worker binding, then to the model the app was started with — so an
	// install with nothing configured still runs.
	ProviderID string `json:"provider_id,omitempty"`

	// Resolved for display. Never a source of truth.
	ProviderName  string `json:"provider_name,omitempty"`
	ProviderLocal bool   `json:"provider_local,omitempty"`
	ContextTokens int    `json:"context_tokens,omitempty"`
}

// ContextShortfall reports how far under the step's requirement its provider
// falls, or 0. A step whose model reads less than the work needs will truncate
// silently, which is the failure that cost this codebase a year.
func (s WorkflowStep) ContextShortfall() int {
	if s.NeedsContext == 0 || s.ContextTokens == 0 {
		return 0
	}
	if s.ContextTokens >= s.NeedsContext {
		return 0
	}
	return s.NeedsContext - s.ContextTokens
}

// WorkflowParam is a question the workflow must be asked before it can run.
//
// Declared as data so the submission dialog is generic: a workflow nobody has
// written yet gets a working form with no front-end change. That is the whole
// reason workflows became rows.
type WorkflowParam struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Help     string `json:"help,omitempty"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Default  string `json:"default,omitempty"`

	// Options for Type == ParamSelect, as value/label pairs.
	Options []ParamOption `json:"options,omitempty"`

	// Multiple accepts many values, comma-separated on the wire. This is how
	// batching works — "run this over the twelve bugs in that scratchpad" is a
	// multi-valued item param, not a separate feature.
	Multiple bool `json:"multiple,omitempty"`
}

type ParamOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Param types. OPEN: an unknown type renders as free text rather than being
// dropped, because a param that silently vanishes submits a job missing an
// argument and the failure surfaces somewhere else entirely.
const (
	ParamFile           = "file"
	ParamDirectory      = "directory"
	ParamScratchpad     = "scratchpad"
	ParamScratchpadItem = "scratchpad_item"
	ParamProject        = "project"
	ParamText           = "text"
	ParamBool           = "bool"
	ParamSelect         = "select"
)

// ValidateParams checks a submission against the declaration and returns the
// values to store, or the first thing wrong with it.
//
// Required-and-missing is caught HERE rather than in the runner: a job row that
// exists, starts, and fails immediately on a blank argument looks to the user
// like the workflow is broken. Refusing the submission says what to fix.
func ValidateParams(decl []WorkflowParam, given map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(decl))
	for _, p := range decl {
		v, ok := given[p.Key]
		if !ok || v == "" {
			v = p.Default
		}
		if p.Required && v == "" {
			return nil, &ParamError{Key: p.Key, Msg: p.Label + " is required"}
		}
		if p.Type == ParamSelect && v != "" && len(p.Options) > 0 {
			found := false
			for _, o := range p.Options {
				if o.Value == v {
					found = true
					break
				}
			}
			if !found {
				return nil, &ParamError{Key: p.Key, Msg: v + " is not one of the choices for " + p.Label}
			}
		}
		if v != "" {
			out[p.Key] = v
		}
	}
	return out, nil
}

// ParamError names the field, so the dialog can mark it rather than showing a
// sentence under the OK button.
type ParamError struct {
	Key string `json:"key"`
	Msg string `json:"msg"`
}

func (e *ParamError) Error() string { return e.Msg }
