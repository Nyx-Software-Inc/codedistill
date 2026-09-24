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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"codedistill/internal/decompose"
	"codedistill/internal/domain"
	"codedistill/internal/id"
	"codedistill/internal/jobrun"
	"codedistill/internal/storage"
)

// Submitting a job.
//
// One endpoint for every workflow, because the parameters are DATA: the dialog
// reads a workflow's declared params, renders them, and posts the answers back
// by key. A workflow nobody has written yet submits through this same handler.
//
// What is NOT generic is the runner — decomposition is four layers of Go, not a
// script — so the handler validates generically and dispatches by workflow id.
// When the external worker contract lands, that dispatch gains a default branch
// that spawns a process, and every workflow reaching it needs no code here.

type submitRequest struct {
	WorkflowID string            `json:"workflow_id"`
	ProjectID  string            `json:"project_id"`
	Params     map[string]string `json:"params"`
}

func (s *Server) submitJob(w http.ResponseWriter, r *http.Request) {
	var req submitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.ProjectID == "" {
		writeMsg(w, http.StatusBadRequest, "project_id is required")
		return
	}
	wf, err := s.store.GetWorkflow(r.Context(), req.WorkflowID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if wf == nil {
		writeMsg(w, http.StatusNotFound, "no such workflow")
		return
	}
	if !wf.Enabled {
		writeMsg(w, http.StatusConflict, wf.Name+" is turned off")
		return
	}

	// Validated against the DECLARATION before anything is created. A job row
	// that exists, starts, and dies on a blank argument reads to the user as a
	// broken workflow; refusing the submission says which field to fix.
	params, err := domain.ValidateParams(wf.Params, req.Params)
	if err != nil {
		var pe *domain.ParamError
		if ok := asParamError(err, &pe); ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": pe.Msg, "field": pe.Key,
			})
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	switch wf.ID {
	case domain.JobDecompose:
		s.submitDecompose(w, r, req.ProjectID, params)
	case domain.JobArchDraft:
		// Architecture drafting owns its own launch path: it derives its work
		// from the repository tree, which has to be walked before there is
		// anything to run.
		writeMsg(w, http.StatusConflict,
			"start architecture drafting from the architecture panel — it needs the component list it builds there")
	default:
		// Said plainly rather than failing at run time. A workflow with no
		// runner is a definition waiting on the worker contract, not a bug.
		writeMsg(w, http.StatusNotImplemented,
			wf.Name+" has no runner yet — external workers are not wired up")
	}
}

func asParamError(err error, out **domain.ParamError) bool {
	pe, ok := err.(*domain.ParamError)
	if ok {
		*out = pe
	}
	return ok
}

// submitDecompose does the work that must fail EARLY — resolving the document,
// reading it, checking the model — and only then goes async.
//
// Everything reportable as a bad request happens while the caller is still on
// the phone. A run that starts and dies two seconds later on an unreadable file
// is a worse answer than a 400 saying which field is wrong.
//
// The input is a scratchpad ITEM, not a path. A browser cannot supply a path,
// and a path would be read on the server — which is only the same machine by
// accident. Both honest routes in (drop, upload) already make the document an
// item, so that is what this takes.
func (s *Server) submitDecompose(w http.ResponseWriter, r *http.Request, projectID string, params map[string]string) {
	itemID := params["source_item"]
	item, err := s.store.GetScratchpadItem(r.Context(), itemID)
	// A document that has been deleted since the dialog listed it is a BAD
	// REQUEST naming the field, not a 500 leaking "scratchpad item x: not
	// found" — the caller can fix the former and can do nothing with the latter.
	if errors.Is(err, storage.ErrNotFound) || (err == nil && item == nil) {
		writeJSON(w, http.StatusBadRequest,
			map[string]any{"error": "that document is no longer there", "field": "source_item"})
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	src, label, err := s.documentFromItem(r.Context(), item)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "field": "source_item"})
		return
	}

	hash := decompose.HashLines(src.Lines)
	again := params["again"] == "true" || params["again"] == "1"

	// Already read, byte for byte? Re-deriving an answer that is on disk costs
	// tens of minutes, so say so and let the caller decide.
	if !again {
		if priors, err := s.store.FindDecomposeRuns(r.Context(), projectID, hash, label); err == nil {
			for _, prior := range priors {
				if prior.SourceHash == hash {
					writeJSON(w, http.StatusConflict, map[string]any{
						"error": fmt.Sprintf(
							"this document was already read on %s — same bytes, so it would produce the same answer. Tick \"read it again\" to run anyway.",
							prior.CreatedAt.Format("2 Jan 2006 15:04")),
						"field": "again", "job_id": prior.JobID,
					})
					return
				}
			}
		}
	}

	// Which model reads the document is the STEP's choice, then the worker's.
	gen, prov, err := StepClient(r.Context(), s.store, domain.JobDecompose, 0,
		domain.WorkerDecomposer, projectID, decompose.MaxCallTimeout+time.Minute)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if gen == nil {
		writeMsg(w, http.StatusConflict,
			"no model is configured for the decomposer — choose one in Settings › Workflows")
		return
	}

	// The document is the SOURCE, not a candidate. Left alone, the classifier
	// reads a 159-line specification and proposes it as one knowledge entry —
	// a nonsense item that also hides the real ones. jobrun marks documents it
	// creates this way; an uploaded or dropped one never passed through there,
	// so it is marked here instead, at the moment we learn it is a document.
	if item.ClassificationState != "skipped" {
		item.ClassificationState = "skipped"
		item.SkippedReason = "override_skip"
		item.UpdatedAt = time.Now().UTC()
		if err := s.store.UpdateClassification(r.Context(), item); err != nil {
			// Not fatal: a misfiled source item is untidy, a refused
			// decomposition is not.
			s.log.Warn("could not mark the document as a source", "item", item.ID, "err", err)
		}
	}

	jobID := id.New()
	runCtx, cancel := context.WithCancel(context.Background())
	s.trackJob(jobID, cancel)

	go func() {
		defer s.untrackJob(jobID)
		_, err := jobrun.RunDecompose(runCtx, s.store, jobrun.DecomposeInput{
			ProjectID: projectID, ScratchpadID: item.ScratchpadID,
			SourceItemID: item.ID, Label: label,
			Lines: src.Lines, SourceHash: hash, Format: src.Format,
			Again: again, Gen: gen, JobID: jobID, NewID: id.New,
			OnJobCreated: func(j *domain.Job) {
				// Kept with the run rather than reconstructed from the
				// workflow: a definition can be edited afterwards, and history
				// that reports today's declaration instead of what was actually
				// submitted is a quiet lie about what happened.
				if err := s.store.SaveJobParams(runCtx, j.ID, params); err != nil {
					s.log.Warn("could not record job params", "job", j.ID, "err", err)
				}
			},
		}, nil)
		if err != nil {
			// The job row already carries the failure; this is for the log.
			s.log.Warn("decompose run failed", "job", jobID, "err", err)
		}
	}()

	resp := map[string]any{
		"job_id": jobID, "label": label, "lines": len(src.Lines),
		"model": gen.Model(),
	}
	if prov != nil {
		resp["provider"] = prov.Name
		// Said out loud, every time. A document leaving the machine is a thing
		// nobody should discover afterwards.
		resp["leaves_machine"] = !prov.IsLocal
	}
	if src.Images > 0 {
		// A spec whose wireframes were dropped looks exactly like one that
		// never had any.
		resp["note"] = fmt.Sprintf("%d embedded image(s) were not read — text only for now", src.Images)
	}
	writeJSON(w, http.StatusAccepted, resp)
}

// documentFromItem gets the text out of whichever kind of item this is.
//
// A dropped or uploaded .odt arrives as a BLOB item — bytes in the blob store
// with a filename — while a pasted spec is a text item. Both are documents; the
// difference is where the bytes live, and the format reader only needs a name
// and a byte slice.
func (s *Server) documentFromItem(ctx context.Context, item *domain.ScratchpadItem) (*decompose.Source, string, error) {
	label := firstNonEmpty(item.FileName, item.Name)

	if item.BlobSHA == "" {
		// A text item. Its content IS the document.
		if strings.TrimSpace(item.Content) == "" {
			return nil, "", fmt.Errorf("that item is empty — there is nothing to read")
		}
		if label == "" {
			label = "pasted document"
		}
		// Named .md so the reader treats it as text rather than guessing: the
		// content is already plain text by construction.
		src, err := decompose.ReadDocumentBytes(label+".md", []byte(item.Content))
		return src, label, err
	}

	if s.blobs == nil {
		return nil, "", fmt.Errorf("this document is a stored file and the blob store is not configured")
	}
	rc, err := s.blobs.Get(ctx, item.BlobSHA)
	if err != nil {
		return nil, "", fmt.Errorf("could not read the stored document: %w", err)
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, "", fmt.Errorf("could not read the stored document: %w", err)
	}
	if label == "" {
		label = "document"
	}
	src, err := decompose.ReadDocumentBytes(label, raw)
	return src, label, err
}

// Running jobs this server owns, so Cancel can actually stop one.
//
// Without this a cancel marks the row and the goroutine keeps burning a model
// for another ten minutes, which is a cancel button that lies.
type jobRegistry struct {
	mu   sync.Mutex
	runs map[string]context.CancelFunc
}

func (s *Server) trackJob(jobID string, cancel context.CancelFunc) {
	s.jobRuns.mu.Lock()
	defer s.jobRuns.mu.Unlock()
	if s.jobRuns.runs == nil {
		s.jobRuns.runs = map[string]context.CancelFunc{}
	}
	s.jobRuns.runs[jobID] = cancel
}

func (s *Server) untrackJob(jobID string) {
	s.jobRuns.mu.Lock()
	defer s.jobRuns.mu.Unlock()
	delete(s.jobRuns.runs, jobID)
}

// stopJob cancels a run this server started. Reports whether it found one:
// a job submitted by the command line is not ours to stop, and pretending
// otherwise would mark it cancelled while it kept running.
func (s *Server) stopJob(jobID string) bool {
	s.jobRuns.mu.Lock()
	cancel := s.jobRuns.runs[jobID]
	s.jobRuns.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}
