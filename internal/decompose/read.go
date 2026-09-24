// =============================================================================
//
//	Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//	CodeDistill
//
//	Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//	Public License v3.0 (see the LICENSE file) and, separately, a commercial
//	license available from Nyx Software, Inc. Use outside the terms of one of those
//	licenses is prohibited.
//
//	SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
//
// =============================================================================
// Reading a document off disk.
//
// Real specifications arrive as .odt and .docx, not markdown. Refusing them was
// honest but useless: the first real document anyone tried was an .odt.
//
// An ODT is a ZIP with content.xml inside, and both halves are in the standard
// library, so this needs no dependency. It flattens to the same line-oriented
// markdown layer 1 already reads: headings become #, list items become
// bullets, and a table row becomes one pipe-delimited LINE.
//
// That last part is the whole reason this is not trivial. Emitting one line per
// CELL turns a four-column use case table into 124 single-column rows; the
// table then has no subject column, gets correctly declined as a non-record
// table, and thirty written-down requirements vanish with no error anywhere.
// That bug cost an afternoon before the row/cell distinction was noticed.
package decompose

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Source is a document read off disk, plus what could not be read from it.
type Source struct {
	Lines  []string
	Format string

	// Images counts embedded pictures. They are NOT extracted yet, and the
	// count exists so a run can say so: a spec whose wireframes were silently
	// dropped looks identical to one that never had any, and the reviewer
	// cannot tell which they are looking at.
	Images int
}

// ReadDocument loads a document, converting it to lines if the format needs it.
func ReadDocument(path string) (*Source, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ReadDocumentBytes(filepath.Base(path), raw)
}

// ReadDocumentBytes reads a document that is already in memory.
//
// The UI path needs this: a document arrives as an upload, and a browser cannot
// hand over a filesystem path — only bytes. Writing them to a temp file just to
// read them back would add a failure mode (a full or read-only temp directory)
// to a step that has no need of one.
//
// The name is used only to choose a format. It never touches the filesystem.
func ReadDocumentBytes(name string, raw []byte) (*Source, error) {
	switch ext := strings.ToLower(filepath.Ext(name)); ext {
	case ".odt":
		return readODT(name, raw)
	case ".docx":
		return readDOCX(name, raw)
	case ".doc":
		return nil, fmt.Errorf(".doc is the pre-2007 binary Word format (an OLE compound file, not a zip) — open it and save as .docx")
	case ".pages":
		return nil, fmt.Errorf(".pages stores its content as compressed protobuf, not markup — in Pages use File > Export To > Word or Plain Text")
	case ".pdf":
		return nil, fmt.Errorf(".pdf is not supported: page-and-block positions are not line spans, so citations would point at nothing")
	default:
		if isBinary(raw) {
			return nil, fmt.Errorf("%s looks binary — decomposition needs text, and half-reading it would produce confident nonsense", filepath.Base(name))
		}
		return &Source{
			Lines:  strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n"),
			Format: strings.TrimPrefix(ext, "."),
		}, nil
	}
}

// isBinary is the cheap check that catches a file renamed to .txt. A NUL byte
// in the first few KB is not something prose contains.
func isBinary(b []byte) bool {
	n := len(b)
	if n > 4096 {
		n = 4096
	}
	for _, c := range b[:n] {
		if c == 0 {
			return true
		}
	}
	return false
}

// ODF element names, namespace-independent: Go's decoder gives us Local names
// and matching on those avoids carrying three namespace URLs that vary between
// producers.
const (
	odfHeading   = "h"
	odfParagraph = "p"
	odfList      = "list"
	odfListItem  = "list-item"
	odfTable     = "table"
	odfTableRow  = "table-row"
	odfTableCell = "table-cell"
	odfImage     = "image"
	odfTab       = "tab"
	odfLineBreak = "line-break"
	odfSpace     = "s"
)

var wsRun = regexp.MustCompile(`\s+`)

func readODT(name string, raw []byte) (*Source, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filepath.Base(name), err)
	}

	var content io.ReadCloser
	for _, f := range zr.File {
		if f.Name == "content.xml" {
			if content, err = f.Open(); err != nil {
				return nil, fmt.Errorf("read content.xml: %w", err)
			}
			break
		}
	}
	if content == nil {
		return nil, fmt.Errorf("%s has no content.xml — not an ODF document", filepath.Base(name))
	}
	defer content.Close()

	src := &Source{Format: "odt"}
	dec := xml.NewDecoder(content)

	// Streaming rather than tree-building: the interesting state is shallow
	// (what am I inside, what text have I gathered) and a 1.6 MB document need
	// not become a DOM to be read once.
	var (
		text      strings.Builder
		depth     int
		inHeading bool
		inText    bool
		rowCells  []string
		inRow     bool
		inCell    bool
	)
	flushText := func() string {
		s := strings.TrimSpace(wsRun.ReplaceAllString(text.String(), " "))
		text.Reset()
		return s
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse content.xml: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case odfImage:
				src.Images++
			case odfTable:
				depth++
			case odfTableRow:
				inRow, rowCells = true, nil
			case odfTableCell:
				inCell = true
				text.Reset()
			case odfHeading:
				inHeading, inText = true, true
				text.Reset()
			case odfParagraph, odfListItem:
				if !inCell {
					text.Reset()
				}
				inText = true
			case odfTab, odfLineBreak, odfSpace:
				text.WriteByte(' ')
			}

		case xml.CharData:
			if inText || inCell {
				text.Write(t)
			}

		case xml.EndElement:
			switch t.Name.Local {
			case odfTableCell:
				rowCells = append(rowCells, flushText())
				inCell = false
			case odfTableRow:
				// ONE LINE PER ROW. Cells joined by pipes, so layer 1 sees a
				// table it can recognise and map onto item fields.
				if inRow && anyNonEmpty(rowCells) {
					src.Lines = append(src.Lines, "| "+strings.Join(rowCells, " | ")+" |")
				}
				inRow, rowCells = false, nil
			case odfTable:
				depth--
				src.Lines = append(src.Lines, "")
			case odfHeading:
				if s := flushText(); s != "" {
					src.Lines = append(src.Lines, "## "+s, "")
				}
				inHeading, inText = false, false
			case odfParagraph, odfListItem:
				if inCell {
					// A cell's paragraphs are part of the cell, not lines.
					text.WriteByte(' ')
					continue
				}
				prefix := ""
				if t.Name.Local == odfListItem {
					prefix = "- "
				}
				if s := flushText(); s != "" {
					src.Lines = append(src.Lines, prefix+s)
					if prefix == "" {
						src.Lines = append(src.Lines, "")
					}
				}
				inText = false
			case odfList:
				src.Lines = append(src.Lines, "")
			}
			_ = inHeading
		}
	}
	return src, nil
}

// OOXML element names. Same ZIP-plus-XML shape as ODF with a different
// vocabulary, and the same trap: a row is one line, not one line per cell.
const (
	ooxParagraph = "p"
	ooxRun       = "t"
	ooxTable     = "tbl"
	ooxTableRow  = "tr"
	ooxTableCell = "tc"
	ooxBreak     = "br"
	ooxTab       = "tab"
	ooxStyle     = "pStyle"
	ooxDrawing   = "drawing"
	ooxPict      = "pict"
	ooxNumPr     = "numPr" // the paragraph is a list item
)

func readDOCX(name string, raw []byte) (*Source, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filepath.Base(name), err)
	}

	var content io.ReadCloser
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			if content, err = f.Open(); err != nil {
				return nil, fmt.Errorf("read word/document.xml: %w", err)
			}
			break
		}
	}
	if content == nil {
		return nil, fmt.Errorf("%s has no word/document.xml — not an OOXML document", filepath.Base(name))
	}
	defer content.Close()

	src := &Source{Format: "docx"}
	dec := xml.NewDecoder(content)

	var (
		text     strings.Builder
		style    string
		isList   bool
		rowCells []string
		inRow    bool
		inCell   bool
		inRunTxt bool
	)
	flush := func() string {
		s := strings.TrimSpace(wsRun.ReplaceAllString(text.String(), " "))
		text.Reset()
		return s
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse word/document.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case ooxDrawing, ooxPict:
				src.Images++
			case ooxTableRow:
				inRow, rowCells = true, nil
			case ooxTableCell:
				inCell = true
				text.Reset()
			case ooxParagraph:
				if !inCell {
					text.Reset()
				}
				style, isList = "", false
			case ooxNumPr:
				isList = true
			case ooxStyle:
				// Word carries heading level in a style name, not the element,
				// so "Heading2" is the only signal that this is a heading.
				for _, a := range t.Attr {
					if a.Name.Local == "val" {
						style = a.Value
					}
				}
			case ooxRun:
				inRunTxt = true
			case ooxBreak, ooxTab:
				text.WriteByte(' ')
			}

		case xml.CharData:
			if inRunTxt {
				text.Write(t)
			}

		case xml.EndElement:
			switch t.Name.Local {
			case ooxRun:
				inRunTxt = false
			case ooxTableCell:
				rowCells = append(rowCells, flush())
				inCell = false
			case ooxTableRow:
				if inRow && anyNonEmpty(rowCells) {
					src.Lines = append(src.Lines, "| "+strings.Join(rowCells, " | ")+" |")
				}
				inRow, rowCells = false, nil
			case ooxTable:
				src.Lines = append(src.Lines, "")
			case ooxParagraph:
				if inCell {
					text.WriteByte(' ')
					continue
				}
				s := flush()
				if s == "" {
					continue
				}
				switch {
				case strings.HasPrefix(strings.ToLower(style), "heading"):
					src.Lines = append(src.Lines, "## "+s, "")
				case isList:
					src.Lines = append(src.Lines, "- "+s)
				default:
					src.Lines = append(src.Lines, s, "")
				}
			}
		}
	}
	return src, nil
}

func anyNonEmpty(ss []string) bool {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}
