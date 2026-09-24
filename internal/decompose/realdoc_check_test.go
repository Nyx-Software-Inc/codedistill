// A harness, not a unit test: it runs the table path and the reconciliation
// against a real document and prints what the two readings disagree about.
// Skipped unless DECOMPOSE_DOC points at one, because the interesting documents
// are private and the useful output is a report rather than an assertion.
//
//	DECOMPOSE_DOC=/path/doc.md PROSE_FILE=/path/subjects.txt \
//	  go test ./internal/decompose -run TestAgainstRealDocument -count=1 -v
package decompose

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestAgainstRealDocument(t *testing.T) {
	path := os.Getenv("DECOMPOSE_DOC")
	if path == "" {
		t.Skip("set DECOMPOSE_DOC to run")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")

	doc := Segment(lines)
	fmt.Printf("\n  prose  : %d sentences, %d regions excluded\n", len(doc.Sentences), len(doc.Excluded))

	var fromTable []Proposal
	for _, tb := range ExtractTables(lines) {
		f, ok := tb.Fields()
		if !ok {
			fmt.Printf("  table  : %d rows at L%d — DECLINED, no subject column %v\n", len(tb.Rows), tb.Line1, tb.Header)
			continue
		}
		ps := tb.RecordProposals(KindForHeader(tb.Header[f.Subject]))
		fmt.Printf("  table  : %d rows at L%d-%d -> %d %s proposals\n", len(tb.Rows), tb.Line1, tb.Line2, len(ps), ps[0].Kind)
		fromTable = append(fromTable, ps...)
	}

	var fromProse []Proposal
	if b, err := os.ReadFile(os.Getenv("PROSE_FILE")); err == nil {
		for _, s := range strings.Split(string(b), "\n") {
			if s = strings.TrimSpace(s); s != "" {
				fromProse = append(fromProse, Proposal{Subject: s, Kind: "use_case"})
			}
		}
	}

	r := Reconcile(fromTable, fromProse, 0.5)
	fmt.Printf("\n  RECONCILIATION — %d table rows vs %d prose proposals\n", len(fromTable), len(fromProse))
	fmt.Printf("    corroborated %3d\n    table only   %3d\n    prose only   %3d\n",
		len(r.Corroborated), len(r.TableOnly), len(r.ProseOnly))
	fmt.Println("\n  WHAT THE PROSE PIPELINE MISSED (the answer coverage cannot give):")
	for _, p := range r.TableOnly {
		fmt.Printf("    %-7s %.76s\n", p.ExternalRef, p.Subject)
	}
}
