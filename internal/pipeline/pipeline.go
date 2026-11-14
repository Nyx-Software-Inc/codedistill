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

// Package pipeline reads what a project's build pipeline runs, from the
// pipeline files in its repository (docs/design/fix-scanner-findings.md, the
// TestBuilder's level 2).
//
// It does not run a pipeline. It finds the test and lint commands in it that
// CodeDistill recognises, so they can be run on a fix the way the pipeline
// will, and so the TestBuilder can follow the pipeline's conventions (a race
// detector, build tags). Everything else a pipeline does (building images,
// packaging, deploying, anything with secrets or variables) is left alone.
package pipeline

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Command is one recognised command, with where it came from and the
// directory it runs in, relative to the repository.
type Command struct {
	Source string `json:"source"` // "Jenkinsfile", ".github/workflows/ci.yml"
	Dir    string `json:"dir,omitempty"`
	Run    string `json:"run"`
}

// Pipeline is what the repository says its pipeline runs.
type Pipeline struct {
	Files    []string  `json:"files"`    // the pipeline files found
	Commands []Command `json:"commands"` // the recognised test and lint commands
}

// recognised are the commands that test or lint and need nothing but the
// repository: a command starting with one of these is kept.
var recognised = []string{
	"go test", "go vet", "golangci-lint run", "staticcheck", "gosec",
	"npm test", "npm run test", "npm run lint", "npm run check", "npx vitest", "npx jest", "npx eslint", "npx svelte-check", "npx tsc",
	"yarn test", "pnpm test",
	"pytest", "python -m pytest", "python3 -m pytest", "ruff check", "flake8", "mypy",
	"cargo test", "cargo clippy",
	"mvn test", "mvn verify", "./mvnw test", "./mvnw verify", "./gradlew test", "./gradlew check", "gradle test",
	"bundle exec rspec", "bundle exec rubocop", "dotnet test",
}

// Detect reads the pipeline files at the repository's root.
func Detect(repo string) Pipeline {
	var p Pipeline
	add := func(source string, lines []line) {
		p.Files = append(p.Files, source)
		for _, l := range lines {
			for _, c := range commandsIn(l) {
				c.Source = source
				p.Commands = append(p.Commands, c)
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(repo, "Jenkinsfile")); err == nil {
		add("Jenkinsfile", jenkinsLines(string(b)))
	}
	for _, f := range globs(repo, ".github/workflows/*.yml", ".github/workflows/*.yaml") {
		if b, err := os.ReadFile(filepath.Join(repo, f)); err == nil {
			add(f, yamlRunLines(string(b), "run"))
		}
	}
	for _, f := range []string{".gitlab-ci.yml", "bitbucket-pipelines.yml", ".circleci/config.yml", "azure-pipelines.yml"} {
		if b, err := os.ReadFile(filepath.Join(repo, f)); err == nil {
			add(f, yamlRunLines(string(b), "script", "run", "command"))
		}
	}
	if b, err := os.ReadFile(filepath.Join(repo, "Makefile")); err == nil {
		add("Makefile", makeLines(string(b)))
	}
	p.Commands = dedupe(p.Commands)
	return p
}

type line struct {
	text string
	dir  string // a working directory the line is known to run in
}

func globs(repo string, patterns ...string) []string {
	var out []string
	for _, pat := range patterns {
		m, _ := filepath.Glob(filepath.Join(repo, pat))
		for _, f := range m {
			if rel, err := filepath.Rel(repo, f); err == nil {
				out = append(out, filepath.ToSlash(rel))
			}
		}
	}
	sort.Strings(out)
	return out
}

var (
	jenkinsSingle = regexp.MustCompile(`\bsh\s*\(?\s*'([^'\n]*)'`)
	jenkinsDouble = regexp.MustCompile(`\bsh\s*\(?\s*"([^"\n]*)"`)
	jenkinsDir    = regexp.MustCompile(`\bdir\s*\(\s*['"]([^'"]+)['"]\s*\)`)
)

// jenkinsLines finds the shell a Jenkinsfile runs: sh '…', sh "…", and the
// lines of sh ”'…”' / sh """…""" blocks, inside dir('…') where it says so.
func jenkinsLines(src string) []line {
	var out []line
	var dirs []string // the dir('…') blocks we are inside, innermost last
	depth := []int{}
	level := 0
	inBlock, quote := false, ""
	for _, raw := range strings.Split(src, "\n") {
		t := strings.TrimSpace(raw)
		cur := ""
		if len(dirs) > 0 {
			cur = dirs[len(dirs)-1]
		}
		if inBlock {
			if strings.Contains(t, quote) {
				inBlock = false
				t = strings.TrimSpace(t[:strings.Index(t, quote)])
			}
			if t != "" {
				out = append(out, line{t, cur})
			}
			continue
		}
		for _, q := range []string{"'''", `"""`} {
			if i := strings.Index(t, "sh "+q); i >= 0 || strings.HasSuffix(t, "sh("+q) {
				rest := t[strings.Index(t, q)+3:]
				if j := strings.Index(rest, q); j >= 0 {
					out = append(out, line{strings.TrimSpace(rest[:j]), cur})
				} else {
					inBlock, quote = true, q
					if rest = strings.TrimSpace(rest); rest != "" {
						out = append(out, line{rest, cur})
					}
				}
			}
		}
		for _, m := range jenkinsSingle.FindAllStringSubmatch(t, -1) {
			out = append(out, line{m[1], cur})
		}
		for _, m := range jenkinsDouble.FindAllStringSubmatch(t, -1) {
			out = append(out, line{m[1], cur})
		}
		if m := jenkinsDir.FindStringSubmatch(t); m != nil {
			dirs = append(dirs, m[1])
			depth = append(depth, level)
		}
		level += strings.Count(t, "{") - strings.Count(t, "}")
		for len(depth) > 0 && level <= depth[len(depth)-1] {
			dirs, depth = dirs[:len(dirs)-1], depth[:len(depth)-1]
		}
	}
	return out
}

// yamlRunLines finds the shell in a YAML pipeline: `key: cmd`, `key: |`
// blocks, and `key:` lists of commands, for the given keys. It reads lines,
// not YAML: good enough for finding commands, and it never fails on a file
// with anchors or templating a parser would choke on.
func yamlRunLines(src string, keys ...string) []line {
	var out []line
	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	blockIndent, listIndent := -1, -1
	dir := ""
	for sc.Scan() {
		raw := sc.Text()
		t := strings.TrimSpace(raw)
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if blockIndent >= 0 {
			if t == "" || indent > blockIndent {
				if t != "" {
					out = append(out, line{t, dir})
				}
				continue
			}
			blockIndent = -1
		}
		if listIndent >= 0 {
			if strings.HasPrefix(t, "- ") && indent >= listIndent {
				out = append(out, line{unquote(strings.TrimPrefix(t, "- ")), dir})
				continue
			}
			listIndent = -1
		}
		key := strings.TrimPrefix(t, "- ")
		if strings.HasPrefix(key, "working-directory:") {
			dir = unquote(strings.TrimSpace(strings.TrimPrefix(key, "working-directory:")))
			continue
		}
		for _, k := range keys {
			if !strings.HasPrefix(key, k+":") {
				continue
			}
			v := strings.TrimSpace(strings.TrimPrefix(key, k+":"))
			switch {
			case v == "|" || v == ">" || v == "|-" || v == ">-":
				blockIndent = indent
			case v == "":
				listIndent = indent
			default:
				out = append(out, line{unquote(v), dir})
			}
		}
		if strings.HasPrefix(t, "- name:") || strings.HasPrefix(t, "- uses:") {
			dir = "" // a new step
		}
	}
	return out
}

var makeTarget = regexp.MustCompile(`^([A-Za-z0-9_.-]+)\s*:`)

// makeLines finds the recipes of a Makefile's test-like targets.
func makeLines(src string) []line {
	var out []line
	in := false
	for _, raw := range strings.Split(src, "\n") {
		if m := makeTarget.FindStringSubmatch(raw); m != nil && !strings.HasPrefix(raw, "\t") {
			switch m[1] {
			case "test", "tests", "check", "lint", "ci", "verify", "vet":
				in = true
			default:
				in = false
			}
			continue
		}
		if in && strings.HasPrefix(raw, "\t") {
			out = append(out, line{strings.TrimLeft(strings.TrimSpace(raw), "@-"), ""})
		}
	}
	return out
}

// commandsIn splits a shell line on && and keeps the recognised commands,
// following a `cd dir &&` in front of them.
func commandsIn(l line) []Command {
	var out []Command
	dir := l.dir
	for _, seg := range strings.Split(l.text, "&&") {
		seg = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(seg), "("), ")"))
		if strings.HasPrefix(seg, "cd ") {
			dir = filepath.ToSlash(filepath.Join(dir, strings.TrimSpace(strings.TrimPrefix(seg, "cd "))))
			continue
		}
		// A variable, a substitution, a pipe or a redirect means it depends on
		// something the pipeline sets up, or does more than test.
		if strings.ContainsAny(seg, "$`|<>;") {
			continue
		}
		for _, r := range recognised {
			if seg == r || strings.HasPrefix(seg, r+" ") {
				out = append(out, Command{Dir: strings.TrimPrefix(dir, "./"), Run: seg})
				break
			}
		}
	}
	return out
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

func dedupe(cs []Command) []Command {
	seen := map[string]bool{}
	var out []Command
	for _, c := range cs {
		k := c.Dir + "\x00" + c.Run
		if !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	return out
}
