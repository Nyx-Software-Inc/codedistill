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

package decompose

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const smallDoc = `# Export

Users must be able to download a project archive. It must not require support.

| Use Case # | Use Case Statement | Priority |
| --- | --- | --- |
| UC-01 | As a user, I want to download a project archive. | P0 |
| UC-02 | As a user, I want to invite a teammate by email. | P1 |
`

func runLines() []string { return strings.Split(strings.TrimPrefix(smallDoc, "\n"), "\n") }

func TestRunProducesCitedProposalsFromBothReadings(t *testing.T) {
	m := &fakeModel{replies: []string{
		// layer 2
		`{"moves":[
			{"sentence":0,"role":"introduce","label":"download a project archive"},
			{"sentence":1,"role":"elaborate","label":"download a project archive","target":0}
		]}`,
		// layer 4
		`{"items":[{"node":"n1","kind":"use_case","subject":"Download a project archive"}]}`,
	}}

	res, err := Run(context.Background(), m, runLines(), Options{}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.TablesRead != 1 || res.TablesDeclined != 0 {
		t.Errorf("tables read=%d declined=%d, want 1/0", res.TablesRead, res.TablesDeclined)
	}
	if res.SourceHash == "" {
		t.Error("no source hash — a re-drop cannot be recognised")
	}

	// Every proposal carries a citation computed at layer 1, before the model
	// ran. This is the guarantee the whole design exists to make structural.
	if len(res.Proposals) == 0 {
		t.Fatal("no proposals")
	}
	for _, p := range res.Proposals {
		if len(p.Lines) == 0 {
			t.Errorf("proposal %q has no citation", p.Subject)
		}
	}

	// The archive requirement is in BOTH the prose and the table, so it must
	// appear once, corroborated — not twice under two origins.
	var archive int
	for _, p := range res.Proposals {
		if strings.Contains(strings.ToLower(p.Subject), "archive") {
			archive++
			if !p.Corroborated {
				t.Errorf("%q found in both readings but not marked corroborated", p.Subject)
			}
		}
	}
	if archive != 1 {
		t.Errorf("the archive requirement appears %d times, want 1", archive)
	}
	// UC-02 is written down and never mentioned in prose: that is the "what
	// did it miss" bucket, and it must survive into the proposals.
	if len(res.Recon.TableOnly) != 1 || res.Recon.TableOnly[0].ExternalRef != "UC-02" {
		t.Errorf("table-only bucket = %+v, want UC-02", res.Recon.TableOnly)
	}
}

func TestRunReportsPhasedProgressHonestly(t *testing.T) {
	var seen []string
	_, err := Run(context.Background(), &fakeModel{}, runLines(), Options{Window: 1},
		func(phase string, done, total int) {
			seen = append(seen, phase)
			// A phase that has not sized itself must report 0, never a guess.
			if total < 0 || done > total && total != 0 {
				t.Errorf("%s reported %d/%d", phase, done, total)
			}
		})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Reading must be announced before sorting can be sized — that ordering is
	// why the job's total has to be revisable.
	first, last := seen[0], seen[len(seen)-1]
	if first != PhaseReading || last != PhaseReconciled {
		t.Fatalf("phases ran %v", seen)
	}
}

func TestRunStopsOnAModelFailureAndKeepsWhatItHad(t *testing.T) {
	res, err := Run(context.Background(), &fakeModel{err: errors.New("ollama unreachable")}, runLines(), Options{}, nil)
	if err == nil {
		t.Fatal("a dead model produced a successful run")
	}
	// The table rows were read WITHOUT the model, so they must not be lost to
	// its failure — a user with no model still gets their written-down
	// requirements.
	if res == nil || res.TablesRead != 1 {
		t.Fatalf("table reading was lost to a model failure: %+v", res)
	}
}

func TestRunIsCancellable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, &fakeModel{}, runLines(), Options{}, nil); err == nil {
		t.Fatal("a cancelled run reported success — fifteen minutes is long enough to change your mind")
	}
}

func TestHashIdentifiesTheDocument(t *testing.T) {
	a := HashLines(runLines())
	if a != HashLines(runLines()) {
		t.Fatal("hash is not stable")
	}
	edited := append([]string{}, runLines()...)
	edited[2] = edited[2] + " Also, it must be fast."
	if a == HashLines(edited) {
		t.Fatal("an edited document hashed the same — a revision would be mistaken for a re-drop")
	}
}
