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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/git"
	"codedistill/internal/id"
)

// maxArchComponents is a sanity ceiling on discovered components, not a
// diagram-size choice: with async drafting the diagram carries ALL of a
// repo's components (structure is free; only characterization costs LLM
// calls). Repos past the ceiling get the truncation reported honestly via
// the delta's uncovered areas.
const maxArchComponents = 200

// maxOneShotNodes caps the no-repo conceptual fallback draft, which is a
// single whole-project LLM answer (it produces 4-10 nodes by prompt anyway).
const maxOneShotNodes = 24

// Architecture diagram (glass-box Phase 5, slice 5.3a). The LLM drafts the
// conceptual structure from the project brain + use cases + a code map; the
// human ratifies (proposed → ratified). Drift will later paint the as-built delta.

type architectureResp struct {
	Nodes []*domain.ArchitectureNode `json:"nodes"`
	Edges []*domain.ArchitectureEdge `json:"edges"`
	// Delta is the as-built overlay (slice 5.3c) — present only when a repo is
	// configured. The diagram is as-intended; the delta is where it disagrees
	// with the code.
	Delta          *archDelta `json:"delta,omitempty"`
	RepoConfigured bool       `json:"repo_configured"`
	// DraftJob reports the background enrichment run (async draft), and
	// UnenrichedNodes counts skeleton nodes still awaiting characterization —
	// >0 with no running job is the panel's cue to offer Resume.
	DraftJob        *archJobStatus `json:"draft_job,omitempty"`
	UnenrichedNodes int            `json:"unenriched_nodes"`
}

// archDelta is the as-built vs as-intended comparison. NodeStatus maps each
// node id to "matched" (its code area exists), "missing" (it claims an area with
// no code — the red delta), or "unmapped" (no area set). UncoveredAreas are
// top-level code dirs no node maps to — structure the diagram is missing.
type archDelta struct {
	NodeStatus     map[string]string `json:"node_status"`
	UncoveredAreas []string          `json:"uncovered_areas"`
}

// computeArchDelta compares the diagram (nodes) against the repo (paths + its
// top-level dirs). Pure, so it's unit-testable without a repo.
func computeArchDelta(paths, topDirs []string, nodes []*domain.ArchitectureNode) *archDelta {
	d := &archDelta{NodeStatus: map[string]string{}, UncoveredAreas: []string{}}
	covered := map[string]bool{}
	hasCodeUnder := func(area string) bool {
		area = strings.Trim(strings.TrimSpace(area), "/")
		if area == "" {
			return false
		}
		for _, p := range paths {
			if p == area || strings.HasPrefix(p, area+"/") {
				return true
			}
		}
		return false
	}
	for _, n := range nodes {
		area := strings.TrimSpace(n.Area)
		switch {
		case area == "":
			d.NodeStatus[n.ID] = "unmapped"
		case hasCodeUnder(area):
			d.NodeStatus[n.ID] = "matched"
			covered[firstSegment(area)] = true
		default:
			d.NodeStatus[n.ID] = "missing"
		}
	}
	for _, dir := range topDirs {
		if !covered[dir] {
			d.UncoveredAreas = append(d.UncoveredAreas, dir)
		}
	}
	if len(d.UncoveredAreas) > 15 {
		d.UncoveredAreas = d.UncoveredAreas[:15]
	}
	return d
}

func firstSegment(p string) string {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}

func validArchKind(k string) bool {
	switch k {
	case "ui", "service", "store", "external", "component":
		return true
	}
	return false
}

func (s *Server) listArchitecture(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.writeArchitecture(w, r, pid)
}

func (s *Server) writeArchitecture(w http.ResponseWriter, r *http.Request, pid string) {
	nodes, err := s.store.ListArchitectureNodes(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	edges, err := s.store.ListArchitectureEdges(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	resp := architectureResp{Nodes: nodes, Edges: edges}
	if j := s.archJobFor(r.Context(), pid); j != nil {
		resp.DraftJob = archStatusOf(j, time.Now().UTC())
	}
	for _, n := range nodes {
		if n.Provenance == "proposed" && n.Description == "" && n.Area != "" {
			resp.UnenrichedNodes++
		}
	}
	// As-built overlay: compare the diagram against the repo when one is configured.
	if proj, err := s.store.GetProject(r.Context(), pid); err == nil && proj.RepoRoot != "" {
		if repo, err := git.Open(proj.RepoRoot); err == nil {
			resp.RepoConfigured = true
			if paths, err := repo.Tree(); err == nil {
				resp.Delta = computeArchDelta(paths, topLevelDirs(repo), nodes)
				// Cache each node's status so the governance gate can read it
				// without git (Phase 6 architecture-as-governance). Best-effort.
				for id, status := range resp.Delta.NodeStatus {
					_ = s.store.SetArchitectureNodeStatus(r.Context(), id, status)
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// draftArchitecture (re)drafts the proposed diagram and returns the full result.
// Preferred path: draft component-by-component from the REAL repo structure —
// characterizing one directory at a time is a bounded task a small local model
// handles well, and the nodes are real directories with structural edges,
// instead of a whole-tree one-shot that small models answer generically. When no
// repo is configured it falls back to the brain/use-case one-shot draft.
func (s *Server) draftArchitecture(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	proj, err := s.store.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if s.suggester == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("AI drafting is unavailable (no model configured)"))
		return
	}
	// Optional body selects the mode. "auto" (default): resume enrichment if
	// unenriched skeleton nodes exist, else fresh skeleton + enrichment.
	// "redraft": explicit start-over — the only path that deletes work.
	var req struct {
		Mode string `json:"mode"`
		// Scope narrows enrichment to one top-level directory — the
		// "characterize this first" action on a container. Auto mode only.
		Scope string `json:"scope"`
	}
	if r.ContentLength > 0 {
		if err := readJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	if req.Mode != "" && req.Mode != "auto" && req.Mode != "redraft" {
		writeMsg(w, http.StatusBadRequest, "mode must be auto or redraft")
		return
	}
	if req.Scope != "" && (req.Mode == "redraft" || strings.Contains(req.Scope, "..")) {
		writeMsg(w, http.StatusBadRequest, "scope applies to auto mode only")
		return
	}

	if proj.RepoRoot != "" {
		if repo, err := git.Open(proj.RepoRoot); err == nil {
			if comps := discoverComponents(repo, maxArchComponents); len(comps) > 0 {
				s.draftArchitectureByDir(w, r, pid, proj.RepoRoot, comps, req.Mode == "redraft", req.Scope)
				return
			}
		}
	}
	s.draftArchitectureOneShot(w, r, pid, proj.RepoRoot)
}

// draftArchitectureByDir builds the diagram bottom-up, split by cost:
// the SKELETON — every component as a path-derived node (real directory as
// area) plus structural edges — is created synchronously here, so the full
// shape of the system renders immediately. Node names/kinds/descriptions are
// then enriched by a background job, one bounded LLM call per component (the
// form small local models answer well), persisted incrementally and resumable
// after restarts. Ratified nodes survive DeleteProposedArchitecture.
func (s *Server) draftArchitectureByDir(w http.ResponseWriter, r *http.Request, pid, repoRoot string, comps []component, redraft bool, scope string) {
	ctx := r.Context()

	// A running job means the draft is already happening — report, don't
	// restart. Explicit redraft is the exception: restartArchJob (below)
	// supersedes the running job and starts a fresh one for the new graph.
	if !redraft {
		if j := s.archJobFor(ctx, pid); j != nil && j.Active() {
			s.writeArchitecture(w, r, pid)
			return
		}
	}

	// Auto mode never deletes work: with unenriched skeleton nodes present
	// (server restart, cancel, model failures) it resumes enrichment; with a
	// fully-enriched diagram present it's a view refresh — starting over
	// requires the explicit redraft mode.
	if !redraft {
		if nodes, err := s.store.ListArchitectureNodes(ctx, pid); err == nil && len(nodes) > 0 {
			for _, n := range nodes {
				if n.Provenance == "proposed" && n.Description == "" && n.Area != "" && inArchScope(n.Area, scope) {
					s.startArchJob(ctx, pid, comps, scope)
					break
				}
			}
			s.writeArchitecture(w, r, pid)
			return
		}
	}

	if err := s.store.DeleteProposedArchitecture(ctx, pid); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	now := time.Now().UTC()
	dirToID := map[string]string{}
	for _, c := range comps {
		// Path-derived skeleton node; Description stays empty as the honest
		// "not yet characterized" marker the enrichment job keys off.
		node := &domain.ArchitectureNode{
			ID: id.New(), ProjectID: pid, Name: prettyName(c.Dir), Kind: "component",
			Area:       c.Dir, // area = the REAL directory
			Provenance: "proposed", CreatedAt: now, UpdatedAt: now,
		}
		if err := s.store.CreateArchitectureNode(ctx, node); err == nil {
			dirToID[c.Dir] = node.ID
		}
	}
	for _, e := range deriveArchEdges(repoRoot, comps) {
		from, to := dirToID[e[0]], dirToID[e[1]]
		if from == "" || to == "" || from == to {
			continue
		}
		_ = s.store.CreateArchitectureEdge(ctx, &domain.ArchitectureEdge{
			ID: id.New(), ProjectID: pid, FromNode: from, ToNode: to,
			Label: "uses", Provenance: "proposed", CreatedAt: now,
		})
	}
	// restartArchJob (not startArchJob): this path just deleted+recreated the
	// proposed graph, so any job still running on the OLD node IDs must be
	// superseded and the NEW skeleton enriched (bug 100 redraft race).
	s.restartArchJob(ctx, pid, comps, scope)
	s.writeArchitecture(w, r, pid)
}

// componentDraft is the per-directory LLM answer: what this one component is.
type componentDraft struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

// characterizeComponent asks the model to label ONE component from its file
// list — a bounded task, so a small local model gives a specific answer instead
// of the generic mush a whole-tree draft produces.
func (s *Server) characterizeComponent(ctx context.Context, c component) (componentDraft, bool) {
	var b strings.Builder
	b.WriteString("You are labeling ONE component of a software system for an architecture diagram — the directory `")
	b.WriteString(c.Dir)
	b.WriteString("`.\n\nFiles in this component:\n")
	for _, f := range capLines(c.Files, 40) {
		b.WriteString("- " + f + "\n")
	}
	b.WriteString(`
Reply as STRICT JSON describing THIS component only:
{"name":"<short specific name>","kind":"ui|service|store|external|component","description":"<one sentence: what it is responsible for>"}
- name: concise and specific (e.g. "HTTP API", "SQLite Storage", "Code Analysis") — not the raw path.
- kind: ui (user interface) | service (logic/handlers) | store (data/persistence) | external (third-party/integration) | component (anything else).
- description: ONE sentence, specific to what these files actually do.`)

	raw, err := s.suggester.GenerateJSON(ctx, b.String())
	if err != nil {
		return componentDraft{}, false
	}
	var d componentDraft
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return componentDraft{}, false
	}
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" {
		return componentDraft{}, false
	}
	d.Kind = strings.ToLower(strings.TrimSpace(d.Kind))
	if !validArchKind(d.Kind) {
		d.Kind = "component"
	}
	d.Description = strings.TrimSpace(d.Description)
	return d, true
}

// draftArchitectureOneShot is the fallback used when no repo is configured: a
// single LLM draft from the project brain + use cases (no code to walk).
func (s *Server) draftArchitectureOneShot(w http.ResponseWriter, r *http.Request, pid, repoRoot string) {
	prompt := s.architecturePrompt(r.Context(), pid, repoRoot)
	raw, err := s.suggester.GenerateJSON(r.Context(), prompt)
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("architecture draft: %w", err))
		return
	}
	var draft struct {
		Nodes []struct {
			Name, Kind, Description, Area string
		} `json:"nodes"`
		Edges []struct {
			From, To, Label string
		} `json:"edges"`
	}
	if err := json.Unmarshal([]byte(raw), &draft); err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("parse architecture draft: %w", err))
		return
	}
	if err := s.store.DeleteProposedArchitecture(r.Context(), pid); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	now := time.Now().UTC()
	nameToID := map[string]string{}
	for i, n := range draft.Nodes {
		if i >= maxOneShotNodes {
			break
		}
		name := strings.TrimSpace(n.Name)
		if name == "" {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(n.Kind))
		if !validArchKind(kind) {
			kind = "component"
		}
		node := &domain.ArchitectureNode{
			ID: id.New(), ProjectID: pid, Name: name, Kind: kind,
			Description: strings.TrimSpace(n.Description), Area: strings.TrimSpace(n.Area),
			Provenance: "proposed", CreatedAt: now, UpdatedAt: now,
		}
		if err := s.store.CreateArchitectureNode(r.Context(), node); err == nil {
			nameToID[name] = node.ID
		}
	}
	for _, e := range draft.Edges {
		from, to := nameToID[strings.TrimSpace(e.From)], nameToID[strings.TrimSpace(e.To)]
		if from == "" || to == "" || from == to {
			continue
		}
		_ = s.store.CreateArchitectureEdge(r.Context(), &domain.ArchitectureEdge{
			ID: id.New(), ProjectID: pid, FromNode: from, ToNode: to,
			Label: strings.TrimSpace(e.Label), Provenance: "proposed", CreatedAt: now,
		})
	}
	s.writeArchitecture(w, r, pid)
}

// architecturePrompt assembles the conceptual-draft prompt from the project
// brain (architecture/convention/decision KB), use cases, and a top-level code map.
func (s *Server) architecturePrompt(ctx context.Context, pid, repoRoot string) string {
	var b strings.Builder
	b.WriteString(`You are drafting a CONCEPTUAL architecture diagram for a software project — its major components/subsystems and how they relate, as a senior engineer would whiteboard it. Do NOT produce a file or import graph. Use the project's own knowledge and intent below.

`)
	if kb, err := s.store.ListKnowledgeEntries(ctx, pid); err == nil {
		var brain []string
		for _, e := range kb {
			if e.Kind == "architecture" || e.Kind == "convention" || e.Kind == "decision" {
				brain = append(brain, "- ("+e.Kind+") "+e.Title+": "+firstN(e.Content, 200))
			}
		}
		if len(brain) > 0 {
			b.WriteString("PROJECT BRAIN:\n" + strings.Join(capLines(brain, 20), "\n") + "\n\n")
		}
	}
	if ucs, err := s.store.ListUseCaseItems(ctx, pid); err == nil && len(ucs) > 0 {
		var lines []string
		for _, u := range ucs {
			lines = append(lines, "- "+u.Subject)
		}
		b.WriteString("USE CASES:\n" + strings.Join(capLines(lines, 25), "\n") + "\n\n")
	}
	if repoRoot != "" {
		if repo, err := git.Open(repoRoot); err == nil {
			if dirs := codeDirs(repo, 2); len(dirs) > 0 {
				b.WriteString("CODE MAP — the project's REAL directories. When you set node.area, copy ONE of these paths EXACTLY. Never invent a path:\n" + strings.Join(dirs, ", ") + "\n\n")
			}
		}
	}
	b.WriteString(`Produce 4 to 10 nodes and the edges between them.
- node.kind is one of: ui | service | store | external | component
- node.area maps the node to its code: it MUST be one of the EXACT paths from the CODE MAP above (copied verbatim), or "" when no directory fits. Do NOT guess or invent a path that isn't in the CODE MAP — a made-up path renders as a "path not found" error on the diagram.
- edges connect nodes by their EXACT name, with a short relationship label (e.g. "calls", "reads", "persists to")

Respond as strict JSON:
{"nodes":[{"name":"...","kind":"...","description":"...","area":"..."}],"edges":[{"from":"...","to":"...","label":"..."}]}`)
	return b.String()
}

// topLevelDirs returns just the repo's first-level directories — used by the
// delta to report which top-level code areas no diagram node covers.
func topLevelDirs(repo *git.Repo) []string {
	paths, err := repo.Tree()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, p := range paths {
		if i := strings.IndexByte(p, '/'); i > 0 {
			d := p[:i]
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	sort.Strings(out)
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}

// codeDirs returns the repo's directories up to maxDepth levels deep, sorted and
// de-duplicated, skipping vendored/generated noise. Giving the drafting LLM the
// REAL directory list (not just top-level) is what stops it inventing area paths
// like "internal/analysis" when the package is actually "internal/codeanalysis"
// — it can only pick a path it's actually shown.
func codeDirs(repo *git.Repo, maxDepth int) []string {
	paths, err := repo.Tree()
	if err != nil {
		return nil
	}
	skip := map[string]bool{".git": true, "vendor": true, "node_modules": true, "dist": true}
	seen := map[string]bool{}
	out := []string{}
	for _, p := range paths {
		segs := strings.Split(p, "/")
		if len(segs) < 2 || skip[segs[0]] {
			continue // root-level file (no dir), or noise tree
		}
		// segs[:len-1] are the file's directories; emit each prefix up to maxDepth.
		for d := 1; d < len(segs) && d <= maxDepth; d++ {
			dir := strings.Join(segs[:d], "/")
			if !seen[dir] {
				seen[dir] = true
				out = append(out, dir)
			}
		}
	}
	sort.Strings(out)
	if len(out) > 120 {
		out = out[:120]
	}
	return out
}

// component is one node candidate: a real directory and the source files under it.
type component struct {
	Dir   string
	Files []string
}

// archCodeExts are the extensions we treat as "code" for component discovery —
// so a docs/ or config-only directory doesn't become an architecture component.
var archCodeExts = map[string]bool{
	".go": true, ".kt": true, ".kts": true, ".java": true, ".ts": true, ".tsx": true,
	".js": true, ".jsx": true, ".svelte": true, ".vue": true, ".py": true, ".rb": true,
	".rs": true, ".cs": true, ".c": true, ".cc": true, ".cpp": true, ".h": true,
	".hpp": true, ".php": true, ".swift": true, ".scala": true, ".m": true,
}

var archSkipDir = map[string]bool{
	".git": true, "vendor": true, "node_modules": true, "dist": true,
	"build": true, ".gradle": true, "testdata": true, ".idea": true, ".vscode": true,
}

// discoverComponents finds the repo's component directories (real diagram nodes).
func discoverComponents(repo *git.Repo, max int) []component {
	paths, err := repo.Tree()
	if err != nil {
		return nil
	}
	return discoverComponentsFromPaths(paths, max)
}

// discoverComponentsFromPaths rolls each source file up to a depth-2 directory —
// internal/api, cmd/foo, web/src — yielding the real component set. Sorted by
// file count (most significant first), capped at max, then alphabetized. Pure,
// so it's unit-testable without a repo.
func discoverComponentsFromPaths(paths []string, max int) []component {
	files := map[string][]string{}
	var order []string
	for _, p := range paths {
		segs := strings.Split(p, "/")
		if len(segs) < 2 || archSkipDir[segs[0]] {
			continue // root-level file, or a noise tree
		}
		if !archCodeExts[strings.ToLower(filepath.Ext(p))] {
			continue
		}
		depth := 2
		if len(segs)-1 < depth {
			depth = len(segs) - 1
		}
		dir := strings.Join(segs[:depth], "/")
		if _, ok := files[dir]; !ok {
			order = append(order, dir)
		}
		files[dir] = append(files[dir], p)
	}
	comps := make([]component, 0, len(order))
	for _, d := range order {
		comps = append(comps, component{Dir: d, Files: files[d]})
	}
	sort.SliceStable(comps, func(i, j int) bool { return len(comps[i].Files) > len(comps[j].Files) })
	if len(comps) > max {
		comps = comps[:max]
	}
	sort.SliceStable(comps, func(i, j int) bool { return comps[i].Dir < comps[j].Dir })
	return comps
}

// deriveArchEdges reads each component's source and derives structural "uses"
// edges from it (see componentEdges). Bounded read per component.
func deriveArchEdges(repoRoot string, comps []component) [][2]string {
	// Read component source through a root-scoped handle so a component
	// file path can never traverse out of repoRoot (G304/CWE-22). c.Files
	// is repo-walk-derived today; os.Root confines the read by construction
	// (refusing ../ escapes and symlinks out of root) in case that changes.
	root, err := os.OpenRoot(repoRoot)
	if err != nil {
		return nil
	}
	defer root.Close()

	bodies := make(map[string]string, len(comps))
	for _, c := range comps {
		var sb strings.Builder
		for _, f := range c.Files {
			if sb.Len() > 150_000 {
				break
			}
			if data, err := readWithinRoot(root, f); err == nil {
				sb.Write(data)
				sb.WriteByte('\n')
			}
		}
		bodies[c.Dir] = sb.String()
	}
	return componentEdges(comps, func(dir string) string { return bodies[dir] })
}

// readWithinRoot reads name (relative to root) through the root handle,
// returning an error for any path that escapes root or resolves through a
// symlink out of it. The whole-file read matches the previous os.ReadFile
// behavior; the per-component size budget is enforced by the caller.
func readWithinRoot(root *os.Root, name string) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// componentEdges derives A→B "uses" edges structurally — no LLM, no invented
// arrows. Two passes: first by B's exact directory path (path-based imports —
// Go, TS, Python — reliable, low-noise); only if that finds nothing (a
// package-name language like Kotlin/Java, whose imports don't contain the dir
// path) does it fall back to B's last path segment as a whole word. bodyOf
// supplies A's concatenated source. Pure, for testing.
func componentEdges(comps []component, bodyOf func(dir string) string) [][2]string {
	if e := scanEdges(comps, bodyOf, false); len(e) > 0 {
		return e
	}
	return scanEdges(comps, bodyOf, true)
}

func scanEdges(comps []component, bodyOf func(dir string) string, useLastSegment bool) [][2]string {
	lastRe := make([]*regexp.Regexp, len(comps))
	if useLastSegment {
		for i, c := range comps {
			seg := c.Dir
			if j := strings.LastIndexByte(seg, '/'); j >= 0 {
				seg = seg[j+1:]
			}
			if len([]rune(seg)) >= 5 {
				lastRe[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(seg) + `\b`)
			}
		}
	}
	var edges [][2]string
	seen := map[string]bool{}
	for a := range comps {
		body := bodyOf(comps[a].Dir)
		if body == "" {
			continue
		}
		for b := range comps {
			if a == b {
				continue
			}
			hit := strings.Contains(body, comps[b].Dir)
			if !hit && lastRe[b] != nil {
				hit = lastRe[b].MatchString(body)
			}
			if hit {
				key := comps[a].Dir + "\x00" + comps[b].Dir
				if !seen[key] {
					seen[key] = true
					edges = append(edges, [2]string{comps[a].Dir, comps[b].Dir})
				}
			}
		}
	}
	return edges
}

// prettyName is the fallback node name when the model can't characterize a
// component: the directory's last path segment.
func prettyName(dir string) string {
	if j := strings.LastIndexByte(dir, '/'); j >= 0 {
		return dir[j+1:]
	}
	return dir
}

func firstN(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func capLines(lines []string, max int) []string {
	if len(lines) > max {
		return lines[:max]
	}
	return lines
}

type updateArchNodeReq struct {
	Name        *string  `json:"name,omitempty"`
	Kind        *string  `json:"kind,omitempty"`
	Description *string  `json:"description,omitempty"`
	Area        *string  `json:"area,omitempty"`
	PosX        *float64 `json:"pos_x,omitempty"`
	PosY        *float64 `json:"pos_y,omitempty"`
	Provenance  *string  `json:"provenance,omitempty"` // proposed | ratified
}

func (s *Server) updateArchNode(w http.ResponseWriter, r *http.Request) {
	nid := r.PathValue("id")
	nodes, err := s.store.GetArchitectureNode(r.Context(), nid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req updateArchNodeReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Name != nil {
		nodes.Name = strings.TrimSpace(*req.Name)
	}
	if req.Kind != nil {
		if !validArchKind(*req.Kind) {
			writeMsg(w, http.StatusBadRequest, "invalid kind")
			return
		}
		nodes.Kind = *req.Kind
	}
	if req.Description != nil {
		nodes.Description = *req.Description
	}
	if req.Area != nil {
		nodes.Area = strings.TrimSpace(*req.Area)
	}
	if req.PosX != nil {
		nodes.PosX = *req.PosX
	}
	if req.PosY != nil {
		nodes.PosY = *req.PosY
	}
	if req.Provenance != nil {
		if *req.Provenance != "proposed" && *req.Provenance != "ratified" {
			writeMsg(w, http.StatusBadRequest, "provenance must be proposed or ratified")
			return
		}
		nodes.Provenance = *req.Provenance
	}
	nodes.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateArchitectureNode(r.Context(), nodes); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (s *Server) deleteArchNode(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteArchitectureNode(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ratifyArchitecture promotes every proposed node + edge in a project to ratified.
func (s *Server) ratifyArchitecture(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	// One atomic promotion of every proposed node + edge (bug 100): the old
	// per-row loop swallowed errors and delete-then-recreated edges, so a
	// transient failure between the delete and the recreate lost an edge for
	// good while still returning 200. Now it's all-or-nothing, and a failure is
	// a real 5xx.
	if err := s.store.RatifyProposedArchitecture(r.Context(), pid, time.Now().UTC()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.writeArchitecture(w, r, pid)
}
