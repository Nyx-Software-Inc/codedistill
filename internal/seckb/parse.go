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

package seckb

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Entry is one item of knowledge: a CWE weakness (and, later, an attack
// pattern, a verification requirement, a cheat-sheet section). Its text is
// the source's own, never a model's.
type Entry struct {
	Key     string // "CWE-916"
	Source  string // "cwe"
	Title   string
	Body    string // description, extended description, mitigations, as plain text
	URL     string
	Version string // the source's version it came from
}

// KEV is one vulnerability CISA lists as exploited in the wild.
type KEV struct {
	CVE            string   `json:"cveID"`
	Vendor         string   `json:"vendorProject"`
	Product        string   `json:"product"`
	Name           string   `json:"vulnerabilityName"`
	DateAdded      string   `json:"dateAdded"`
	Description    string   `json:"shortDescription"`
	RequiredAction string   `json:"requiredAction"`
	DueDate        string   `json:"dueDate"`
	Ransomware     string   `json:"knownRansomwareCampaignUse"`
	CWEs           []string `json:"cwes"`
}

// ParseKEV reads CISA's catalogue. The version is its catalogVersion.
func ParseKEV(b []byte) ([]KEV, string, error) {
	var doc struct {
		Version string `json:"catalogVersion"`
		Vulns   []KEV  `json:"vulnerabilities"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, "", fmt.Errorf("parse the KEV catalogue: %w", err)
	}
	if doc.Version == "" || len(doc.Vulns) == 0 {
		return nil, "", fmt.Errorf("the KEV catalogue has no version or no entries")
	}
	return doc.Vulns, doc.Version, nil
}

// ParseCWE reads MITRE's CWE catalogue from its zip: every weakness, with
// its description, extended description and mitigations as plain text. The
// version is the catalogue's Version and Date ("5.0, 2026-10-28").
func ParseCWE(zipped []byte) ([]Entry, string, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipped), int64(len(zipped)))
	if err != nil {
		return nil, "", fmt.Errorf("open the CWE archive: %w", err)
	}
	var f *zip.File
	for _, x := range zr.File {
		if strings.HasSuffix(x.Name, ".xml") {
			f = x
		}
	}
	if f == nil {
		return nil, "", fmt.Errorf("the CWE archive holds no XML")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, "", err
	}
	defer rc.Close()
	return parseCWEXML(io.LimitReader(rc, 512<<20))
}

type cweText struct {
	Inner string `xml:",innerxml"`
}

type cweWeakness struct {
	ID          string  `xml:"ID,attr"`
	Name        string  `xml:"Name,attr"`
	Status      string  `xml:"Status,attr"`
	Description string  `xml:"Description"`
	Extended    cweText `xml:"Extended_Description"`
	Mitigations []struct {
		Phase       []string `xml:"Phase"`
		Description cweText  `xml:"Description"`
	} `xml:"Potential_Mitigations>Mitigation"`
}

func parseCWEXML(r io.Reader) ([]Entry, string, error) {
	dec := xml.NewDecoder(r)
	version := ""
	var out []Entry
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("parse the CWE catalogue: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "Weakness_Catalog":
			var v, d string
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "Version":
					v = a.Value
				case "Date":
					d = a.Value
				}
			}
			version = strings.Trim(v+", "+d, ", ")
		case "Weakness":
			var w cweWeakness
			if err := dec.DecodeElement(&w, &se); err != nil {
				return nil, "", fmt.Errorf("parse CWE weakness: %w", err)
			}
			if w.Status == "Deprecated" || w.ID == "" {
				continue
			}
			out = append(out, w.entry(version))
		}
	}
	if len(out) == 0 {
		return nil, "", fmt.Errorf("the CWE catalogue has no weaknesses")
	}
	return out, version, nil
}

func (w cweWeakness) entry(version string) Entry {
	var b strings.Builder
	b.WriteString(clean(w.Description))
	if ext := plainText(w.Extended.Inner); ext != "" {
		b.WriteString("\n\n" + ext)
	}
	if len(w.Mitigations) > 0 {
		b.WriteString("\n\nMitigations:")
		for _, m := range w.Mitigations {
			d := plainText(m.Description.Inner)
			if d == "" {
				continue
			}
			phase := ""
			if len(m.Phase) > 0 {
				phase = " (" + strings.Join(m.Phase, ", ") + ")"
			}
			b.WriteString("\n- " + d + phase)
		}
	}
	return Entry{Key: "CWE-" + w.ID, Source: "cwe", Title: w.Name, Body: b.String(),
		URL: "https://cwe.mitre.org/data/definitions/" + w.ID + ".html", Version: version}
}

var (
	tagRE   = regexp.MustCompile(`<[^>]+>`)
	blockRE = regexp.MustCompile(`</?(?:xhtml:)?(?:p|li|br|div|ul|ol)[^>]*>`)
	spaceRE = regexp.MustCompile(`[ \t]+`)
	linesRE = regexp.MustCompile(`\n\s*\n+`)
)

// plainText turns the catalogue's XHTML into text, keeping paragraphs and
// list items apart.
func plainText(s string) string {
	s = blockRE.ReplaceAllString(s, "\n")
	s = tagRE.ReplaceAllString(s, "")
	s = htmlUnescape(s)
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(spaceRE.ReplaceAllString(l, " ")); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.TrimSpace(linesRE.ReplaceAllString(strings.Join(lines, "\n"), "\n"))
}

func clean(s string) string { return strings.TrimSpace(spaceRE.ReplaceAllString(s, " ")) }

func htmlUnescape(s string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&amp;", "&").Replace(s)
}
