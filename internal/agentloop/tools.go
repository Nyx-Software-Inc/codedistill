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

package agentloop

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"codedistill/internal/ollama"
)

// The loop's own tools. Their shapes follow the ones coding models are
// trained on (Claude Code's, which other agents copy), so a model uses them
// without being taught. Every path is inside the checkout: the tools refuse
// anything else, and the sandbox the run is in refuses it again.

type localTools struct {
	dir     string
	canEdit bool
}

func newLocalTools(dir string, canEdit bool) *localTools {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return &localTools{dir: dir, canEdit: canEdit}
}

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }

func (t *localTools) specs() []ollama.ToolSpec {
	s := []ollama.ToolSpec{
		{Name: "read_file", Description: "Read a file. Lines are numbered from 1 (the numbers are not part of the file). Large files are shown a window at a time: use offset and limit for the rest.",
			Schema: obj(map[string]any{"path": str("the file, relative to the checkout"),
				"offset": num("the first line to show (default 1)"), "limit": num("how many lines (default 2000)")}, "path")},
		{Name: "list_files", Description: "List files matching a glob pattern such as \"**/*.go\" or \"internal/api/*.go\". Files git ignores are left out.",
			Schema: obj(map[string]any{"pattern": str("the glob; ** matches any number of directories")}, "pattern")},
		{Name: "search", Description: "Search file contents with a regular expression (RE2 syntax). Returns file:line: text for each match.",
			Schema: obj(map[string]any{"pattern": str("the regular expression"),
				"path": str("a file or directory to search in (default: the whole checkout)"),
				"glob": str("only files matching this glob, e.g. \"*.go\"")}, "pattern")},
		{Name: "run", Description: "Run a shell command in the checkout (sh -c) and return its exit code and output. Use it to build, test and run tools. Commands that run longer than the timeout are stopped.",
			Schema: obj(map[string]any{"command": str("the command"),
				"timeout": num("seconds before it is stopped (default 120, at most 1200)")}, "command")},
	}
	if t.canEdit {
		s = append(s,
			ollama.ToolSpec{Name: "edit_file", Description: "Replace text in a file. old_string must match the file exactly, including whitespace, and occur exactly once, unless replace_all is set. Read the file first.",
				Schema: obj(map[string]any{"path": str("the file"), "old_string": str("the exact text to replace"),
					"new_string":  str("the text to put in its place"),
					"replace_all": map[string]any{"type": "boolean", "description": "replace every occurrence"}}, "path", "old_string", "new_string")},
			ollama.ToolSpec{Name: "write_file", Description: "Create a file, or overwrite one, with the given content. Prefer edit_file for changing an existing file.",
				Schema: obj(map[string]any{"path": str("the file"), "content": str("the whole content")}, "path", "content")},
		)
	}
	return s
}

func (t *localTools) has(name string) bool {
	switch name {
	case "read_file", "list_files", "search", "run":
		return true
	case "edit_file", "write_file":
		return t.canEdit
	}
	return false
}

func (t *localTools) call(ctx context.Context, name string, raw json.RawMessage) (string, bool) {
	var a struct {
		Path       string `json:"path"`
		Offset     int    `json:"offset"`
		Limit      int    `json:"limit"`
		Pattern    string `json:"pattern"`
		Glob       string `json:"glob"`
		Command    string `json:"command"`
		Timeout    int    `json:"timeout"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return "the arguments are not valid JSON for this tool: " + err.Error(), true
	}
	var out string
	var err error
	switch name {
	case "read_file":
		out, err = t.readFile(a.Path, a.Offset, a.Limit)
	case "list_files":
		out, err = t.listFiles(a.Pattern)
	case "search":
		out, err = t.search(a.Pattern, a.Path, a.Glob)
	case "run":
		return t.run(ctx, a.Command, a.Timeout)
	case "edit_file":
		out, err = t.editFile(a.Path, a.OldString, a.NewString, a.ReplaceAll)
	case "write_file":
		out, err = t.writeFile(a.Path, a.Content)
	}
	if err != nil {
		return err.Error(), true
	}
	return out, false
}

// resolve turns a path the model gave into one inside the checkout, or
// refuses it. Symbolic links are followed as far as the path exists, so a
// link cannot lead out.
func (t *localTools) resolve(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("a path is required")
	}
	full := p
	if !filepath.IsAbs(full) {
		full = filepath.Join(t.dir, p)
	}
	full = filepath.Clean(full)
	real := full
	// Follow links on the longest part of the path that exists.
	for probe := full; ; probe = filepath.Dir(probe) {
		if r, err := filepath.EvalSymlinks(probe); err == nil {
			rest, _ := filepath.Rel(probe, full)
			real = filepath.Join(r, rest)
			break
		}
		if probe == filepath.Dir(probe) {
			break
		}
	}
	if rel, err := filepath.Rel(t.dir, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the checkout; only files inside it can be used", p)
	}
	return full, nil
}

func (t *localTools) rel(full string) string {
	if r, err := filepath.Rel(t.dir, full); err == nil {
		return filepath.ToSlash(r)
	}
	return full
}

const readDefaultLimit = 2000

func (t *localTools) readFile(p string, offset, limit int) (string, error) {
	full, err := t.resolve(p)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("%s does not exist", p)
	}
	if st.IsDir() {
		return "", fmt.Errorf("%s is a directory; use list_files to see what is in it", p)
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
		return "", fmt.Errorf("%s is a binary file", p)
	}
	if offset < 1 {
		offset = 1
	}
	if limit <= 0 {
		limit = readDefaultLimit
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return "(the file is empty)", nil
	}
	if offset > len(lines) {
		return "", fmt.Errorf("%s has %d lines; offset %d is past the end", p, len(lines), offset)
	}
	end := min(len(lines), offset-1+limit)
	var sb strings.Builder
	for i := offset - 1; i < end; i++ {
		l := lines[i]
		if len(l) > 2000 {
			l = l[:2000] + "… [line cut]"
		}
		fmt.Fprintf(&sb, "%6d\t%s\n", i+1, l)
	}
	if offset > 1 || end < len(lines) {
		fmt.Fprintf(&sb, "(lines %d–%d of %d)\n", offset, end, len(lines))
	}
	return sb.String(), nil
}

// files lists the checkout's files the way git sees them (tracked, and
// untracked ones not ignored); outside a repository, every file but .git.
func (t *localTools) files() []string {
	cmd := exec.Command("git", "-c", "core.fsmonitor=false", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = t.dir
	if out, err := cmd.Output(); err == nil {
		var found []string
		seen := map[string]bool{}
		for _, f := range strings.Split(string(out), "\x00") {
			if f != "" && !seen[f] {
				if _, err := os.Lstat(filepath.Join(t.dir, f)); err == nil {
					seen[f] = true
					found = append(found, f)
				}
			}
		}
		sort.Strings(found)
		return found
	}
	var found []string
	_ = filepath.WalkDir(t.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			found = append(found, t.rel(p))
		}
		return nil
	})
	return found
}

// globRE turns a glob into a regular expression over slash-separated paths:
// ** spans directories, * and ? stay within one. A pattern without a slash
// matches a file's name anywhere, as people expect "*.go" to.
func globRE(pattern string) (*regexp.Regexp, error) {
	pattern = filepath.ToSlash(strings.TrimPrefix(pattern, "./"))
	if !strings.Contains(pattern, "/") {
		pattern = "**/" + pattern
	}
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '*' && i+1 < len(pattern) && pattern[i+1] == '*':
			i++
			if i+1 < len(pattern) && pattern[i+1] == '/' {
				i++
				sb.WriteString("(?:.*/)?")
			} else {
				sb.WriteString(".*")
			}
		case c == '*':
			sb.WriteString("[^/]*")
		case c == '?':
			sb.WriteString("[^/]")
		case c == '{':
			j := strings.IndexByte(pattern[i:], '}')
			if j < 0 {
				sb.WriteString(regexp.QuoteMeta(string(c)))
				continue
			}
			alts := strings.Split(pattern[i+1:i+j], ",")
			for k := range alts {
				alts[k] = regexp.QuoteMeta(alts[k])
			}
			sb.WriteString("(?:" + strings.Join(alts, "|") + ")")
			i += j
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}

const listCap = 500

func (t *localTools) listFiles(pattern string) (string, error) {
	if pattern == "" {
		pattern = "**"
	}
	re, err := globRE(pattern)
	if err != nil {
		return "", fmt.Errorf("the pattern is not a glob this tool understands: %v", err)
	}
	var hits []string
	for _, f := range t.files() {
		if re.MatchString(f) {
			hits = append(hits, f)
		}
	}
	if len(hits) == 0 {
		return "no files match " + pattern, nil
	}
	more := ""
	if len(hits) > listCap {
		more = fmt.Sprintf("\n… and %d more; narrow the pattern", len(hits)-listCap)
		hits = hits[:listCap]
	}
	return strings.Join(hits, "\n") + more, nil
}

const searchCap = 200

func (t *localTools) search(pattern, under, glob string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("the pattern is not a valid regular expression (RE2): %v", err)
	}
	prefix := ""
	if under != "" && under != "." {
		full, err := t.resolve(under)
		if err != nil {
			return "", err
		}
		prefix = t.rel(full)
	}
	var gre *regexp.Regexp
	if glob != "" {
		if gre, err = globRE(glob); err != nil {
			return "", fmt.Errorf("the glob is not one this tool understands: %v", err)
		}
	}
	var hits []string
	total := 0
	for _, f := range t.files() {
		if prefix != "" && f != prefix && !strings.HasPrefix(f, prefix+"/") {
			continue
		}
		if gre != nil && !gre.MatchString(f) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(t.dir, f))
		if err != nil || len(b) > 4<<20 || bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
			continue
		}
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for n := 1; sc.Scan(); n++ {
			if re.Match(sc.Bytes()) {
				total++
				if len(hits) < searchCap {
					hits = append(hits, fmt.Sprintf("%s:%d: %s", f, n, clip(sc.Text(), 300)))
				}
			}
		}
	}
	if total == 0 {
		return "no matches", nil
	}
	out := strings.Join(hits, "\n")
	if total > len(hits) {
		out += fmt.Sprintf("\n… %d matches in all; narrow the pattern or the path", total)
	}
	return out, nil
}

// gitDenied are the git verbs an agent may not use, as Claude Code's are
// denied by name. The Conductor's own checks (on the branch, descends from
// the start, nothing pushed) do not depend on this holding.
var gitDenied = regexp.MustCompile(`(?:^|[;&|(\s])git(?:\s+-[cC]\s+\S+)*\s+(push|checkout|switch|reset|rebase|branch|remote|worktree|tag|config)\b`)

func (t *localTools) run(ctx context.Context, command string, timeout int) (string, bool) {
	if strings.TrimSpace(command) == "" {
		return "a command is required", true
	}
	if m := gitDenied.FindStringSubmatch(command); m != nil {
		return fmt.Sprintf("git %s is not allowed here: the job's branch is managed by CodeDistill. Commit your work with git add and git commit.", m[1]), true
	}
	if timeout <= 0 {
		timeout = 120
	}
	timeout = min(timeout, 1200)
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-c", command)
	cmd.Dir = t.dir
	cmd.Stdin = nil
	setProcessGroup(cmd)
	cmd.WaitDelay = 5 * time.Second
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	out := strings.TrimRight(buf.String(), "\n")
	switch {
	case cctx.Err() == context.DeadlineExceeded:
		return fmt.Sprintf("%s\n[stopped after %d seconds; give it a longer timeout, or run something smaller]", out, timeout), true
	case err != nil:
		code := -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return fmt.Sprintf("%s\n[exit code %d]", out, code), true
	}
	if out == "" {
		out = "(no output)"
	}
	return out + "\n[exit code 0]", false
}

func (t *localTools) editFile(p, old, repl string, all bool) (string, error) {
	full, err := t.resolve(p)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("%s does not exist; use write_file to create it", p)
	}
	if old == "" {
		return "", fmt.Errorf("old_string is empty; give the exact text to replace (use write_file to create a file)")
	}
	if old == repl {
		return "", fmt.Errorf("old_string and new_string are the same; nothing would change")
	}
	s := string(b)
	n := strings.Count(s, old)
	if n == 0 && !all {
		// Weaker models copy text with the wrong whitespace at line ends.
		// Matched loosely only when that finds exactly one place, so a loose
		// match can never pick between two.
		if start, end, ok := looseMatch(s, old); ok {
			s = s[:start] + repl + s[end:]
			st, _ := os.Stat(full)
			if err := os.WriteFile(full, []byte(s), st.Mode().Perm()); err != nil {
				return "", err
			}
			return "edited " + p + " (old_string matched once when whitespace at line ends was ignored)", nil
		}
	}
	switch {
	case n == 0:
		return "", fmt.Errorf("old_string was not found in %s. It must match exactly, including indentation; read the file again and copy the text", p)
	case n > 1 && !all:
		return "", fmt.Errorf("old_string occurs %d times in %s; include more of the surrounding text so it matches once, or set replace_all", n, p)
	}
	if all {
		s = strings.ReplaceAll(s, old, repl)
	} else {
		s = strings.Replace(s, old, repl, 1)
	}
	st, _ := os.Stat(full)
	if err := os.WriteFile(full, []byte(s), st.Mode().Perm()); err != nil {
		return "", err
	}
	if all {
		return fmt.Sprintf("replaced %d occurrences in %s", n, p), nil
	}
	return "edited " + p, nil
}

// looseMatch finds old in s comparing whole lines with whitespace at their
// ends ignored, and the blank lines old starts or ends with dropped. It
// returns the span of the matched lines in s (without the final newline)
// when there is exactly one such place.
func looseMatch(s, old string) (start, end int, ok bool) {
	want := strings.Split(strings.Trim(old, "\r\n"), "\n")
	for i := range want {
		want[i] = strings.TrimRight(want[i], " \t\r")
	}
	if len(want) == 0 || strings.TrimSpace(strings.Join(want, "")) == "" {
		return 0, 0, false
	}
	lines := strings.SplitAfter(s, "\n")
	offs := make([]int, len(lines)+1)
	for i, l := range lines {
		offs[i+1] = offs[i] + len(l)
	}
	found := 0
	for i := 0; i+len(want) <= len(lines); i++ {
		match := true
		for j, w := range want {
			if strings.TrimRight(lines[i+j], " \t\r\n") != w {
				match = false
				break
			}
		}
		if match {
			found++
			start, end = offs[i], offs[i+len(want)]
			end -= len(lines[i+len(want)-1]) - len(strings.TrimRight(lines[i+len(want)-1], "\r\n"))
		}
	}
	return start, end, found == 1
}

func (t *localTools) writeFile(p, content string) (string, error) {
	full, err := t.resolve(p)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(full); err == nil && st.IsDir() {
		return "", fmt.Errorf("%s is a directory", p)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(full); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(full, []byte(content), mode); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %s (%d bytes)", p, len(content)), nil
}
