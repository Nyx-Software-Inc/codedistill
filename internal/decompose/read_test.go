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
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeZip builds a real archive so the readers are exercised against the
// format rather than against a mock of it.
func writeZip(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for n, body := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

const odtContent = `<?xml version="1.0" encoding="UTF-8"?>
<office:document-content xmlns:office="urn:office" xmlns:text="urn:text" xmlns:table="urn:table" xmlns:draw="urn:draw">
 <office:body><office:text>
  <text:h text:outline-level="1">Overview</text:h>
  <text:p>Users must be able to export an archive. It must include attachments.</text:p>
  <text:list><text:list-item><text:p>Rate-limit the endpoint</text:p></text:list-item>
             <text:list-item><text:p>Cap attachments at 25 MB</text:p></text:list-item></text:list>
  <text:p><draw:frame><draw:image xlink:href="Pictures/a.png"/></draw:frame></text:p>
  <table:table>
   <table:table-row><table:table-cell><text:p>Use Case #</text:p></table:table-cell>
    <table:table-cell><text:p>Use Case Statement</text:p></table:table-cell>
    <table:table-cell><text:p>Priority</text:p></table:table-cell></table:table-row>
   <table:table-row><table:table-cell><text:p>UC-01</text:p></table:table-cell>
    <table:table-cell><text:p>As a user, I want to export an archive.</text:p></table:table-cell>
    <table:table-cell><text:p>P0</text:p></table:table-cell></table:table-row>
  </table:table>
 </office:text></office:body></office:document-content>`

const docxContent = `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="urn:w">
 <w:body>
  <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Overview</w:t></w:r></w:p>
  <w:p><w:r><w:t>Users must be able to export an archive.</w:t></w:r><w:r><w:t> It must include attachments.</w:t></w:r></w:p>
  <w:p><w:pPr><w:numPr><w:ilvl w:val="0"/></w:numPr></w:pPr><w:r><w:t>Rate-limit the endpoint</w:t></w:r></w:p>
  <w:p><w:r><w:drawing/></w:r></w:p>
  <w:tbl>
   <w:tr><w:tc><w:p><w:r><w:t>Use Case #</w:t></w:r></w:p></w:tc>
         <w:tc><w:p><w:r><w:t>Use Case Statement</w:t></w:r></w:p></w:tc>
         <w:tc><w:p><w:r><w:t>Priority</w:t></w:r></w:p></w:tc></w:tr>
   <w:tr><w:tc><w:p><w:r><w:t>UC-01</w:t></w:r></w:p></w:tc>
         <w:tc><w:p><w:r><w:t>As a user, I want to export an archive.</w:t></w:r></w:p></w:tc>
         <w:tc><w:p><w:r><w:t>P0</w:t></w:r></w:p></w:tc></w:tr>
  </w:tbl>
 </w:body></w:document>`

// Both office formats must survive the trap that cost thirty requirements: a
// table row is ONE line, not one line per cell. Emitted per cell, the table has
// no subject column, is correctly declined as a non-record table, and the
// requirements vanish with no error anywhere.
func TestOfficeFormatsYieldRowsNotCells(t *testing.T) {
	for _, tc := range []struct {
		name, file string
		files      map[string]string
	}{
		{"odt", "d.odt", map[string]string{"content.xml": odtContent}},
		{"docx", "d.docx", map[string]string{"word/document.xml": docxContent}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, err := ReadDocument(writeZip(t, tc.file, tc.files))
			if err != nil {
				t.Fatal(err)
			}
			if src.Format != tc.name {
				t.Errorf("format = %q", src.Format)
			}
			if src.Images != 1 {
				t.Errorf("images = %d, want 1 — a dropped wireframe must be countable", src.Images)
			}

			tables := ExtractTables(src.Lines)
			if len(tables) != 1 {
				t.Fatalf("got %d tables from %v", len(tables), src.Lines)
			}
			f, ok := tables[0].Fields()
			if !ok {
				t.Fatalf("table not recognised as records — header was %v", tables[0].Header)
			}
			props := tables[0].RecordProposals(KindForHeader(tables[0].Header[f.Subject]))
			if len(props) != 1 || props[0].ExternalRef != "UC-01" || props[0].Priority != "P0" {
				t.Fatalf("row fields lost: %+v", props)
			}

			// Prose still segments, headings still give section context, and a
			// list item is its own unit rather than glued to its neighbour.
			doc := Segment(src.Lines)
			if len(doc.Sentences) < 3 {
				t.Fatalf("got %d sentences from %v", len(doc.Sentences), src.Lines)
			}
			if doc.Sentences[0].Section != "Overview" {
				t.Errorf("heading lost: section = %q", doc.Sentences[0].Section)
			}
			var joined string
			for _, s := range doc.Sentences {
				joined += s.Text + "|"
			}
			if !strings.Contains(joined, "Rate-limit the endpoint") {
				t.Errorf("list item lost: %q", joined)
			}
		})
	}
}

// Formats that cannot yield a stable line span are refused with a way forward,
// not a shrug. A citation is the guarantee everything rests on.
func TestUnsupportedFormatsSayWhatToDoInstead(t *testing.T) {
	dir := t.TempDir()
	for ext, want := range map[string]string{
		".doc":   "save as .docx",
		".pages": "Export To",
		".pdf":   "not supported",
	} {
		p := filepath.Join(dir, "x"+ext)
		if err := os.WriteFile(p, []byte("whatever"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := ReadDocument(p)
		if err == nil {
			t.Fatalf("%s was accepted", ext)
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s error does not say what to do: %v", ext, err)
		}
	}
}

// A binary renamed to .txt must be refused rather than half-read into
// confident nonsense.
func TestBinaryMasqueradingAsTextIsRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sneaky.txt")
	if err := os.WriteFile(p, []byte{'h', 'i', 0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDocument(p); err == nil {
		t.Fatal("a binary file was accepted as text")
	}
}
