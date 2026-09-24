package decompose

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Harness: check native reading against a real document. Skipped unless
// DECOMPOSE_DOC points at one.
func TestReadRealDocument(t *testing.T) {
	path := os.Getenv("DECOMPOSE_DOC")
	if path == "" {
		t.Skip("set DECOMPOSE_DOC to run")
	}
	src, err := ReadDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("\n  format %s, %d lines, %d images\n", src.Format, len(src.Lines), src.Images)

	doc := Segment(src.Lines)
	fmt.Printf("  %d sentences, %d excluded regions\n", len(doc.Sentences), len(doc.Excluded))
	for _, tb := range ExtractTables(src.Lines) {
		f, ok := tb.Fields()
		if !ok {
			fmt.Printf("  table at L%d DECLINED: %v\n", tb.Line1, tb.Header)
			continue
		}
		ps := tb.RecordProposals(KindForHeader(tb.Header[f.Subject]))
		fmt.Printf("  table at L%d-%d: %d rows -> %d %s proposals\n", tb.Line1, tb.Line2, len(tb.Rows), len(ps), ps[0].Kind)
		for _, p := range ps[:min(3, len(ps))] {
			fmt.Printf("      %-7s %-58s %s\n", p.ExternalRef, trunc(p.Subject, 58), p.Priority)
		}
	}
	fmt.Println("\n  first prose lines:")
	n := 0
	for _, l := range src.Lines {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "|") {
			continue
		}
		fmt.Printf("      %.96s\n", l)
		if n++; n >= 4 {
			break
		}
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
