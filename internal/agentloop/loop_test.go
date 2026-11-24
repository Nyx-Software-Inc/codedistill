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
package agentloop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codedistill/internal/ollama"
)

// script is a model that plays back fixed turns and keeps what it was sent.
type script struct {
	turns []ollama.ChatResponse
	seen  []ollama.ChatRequest
}

func (s *script) Chat(_ context.Context, r ollama.ChatRequest) (*ollama.ChatResponse, error) {
	s.seen = append(s.seen, r)
	if len(s.seen) > len(s.turns) {
		return &ollama.ChatResponse{Text: "out of script"}, nil
	}
	t := s.turns[len(s.seen)-1]
	return &t, nil
}

func call(id, name, args string) ollama.ToolCall {
	return ollama.ToolCall{ID: id, Name: name, Args: json.RawMessage(args)}
}

type fakeRemote struct{ calls []string }

func (f *fakeRemote) Tools() []ollama.ToolSpec {
	return []ollama.ToolSpec{{Name: "record_claims", Schema: obj(map[string]any{})}}
}
func (f *fakeRemote) Call(_ context.Context, name string, args json.RawMessage) (string, bool, error) {
	f.calls = append(f.calls, name+" "+string(args))
	return "recorded", false, nil
}

func checkout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hash.go"), []byte("package hash\n\nimport \"crypto/md5\"\n\nvar h = md5.New\n"), 0o644)
	return dir
}

// The whole loop: read, edit, run, record with CodeDistill's tool, then stop.
// Each result goes back to the model against the call that asked for it.
func TestRunFixesAndRecords(t *testing.T) {
	dir := checkout(t)
	m := &script{turns: []ollama.ChatResponse{
		{ToolCalls: []ollama.ToolCall{call("a", "read_file", `{"path":"hash.go"}`)}},
		{ToolCalls: []ollama.ToolCall{call("b", "edit_file", `{"path":"hash.go","old_string":"crypto/md5","new_string":"crypto/sha256"}`),
			call("c", "edit_file", `{"path":"hash.go","old_string":"md5.New","new_string":"sha256.New"}`)}},
		{ToolCalls: []ollama.ToolCall{call("d", "run", `{"command":"grep -c sha256 hash.go"}`)}},
		{ToolCalls: []ollama.ToolCall{call("e", "record_claims", `{"claims":["md5 replaced"]}`)}},
		{Text: "Replaced md5 with sha256."},
	}}
	rem := &fakeRemote{}
	var notes []string
	res, err := Run(context.Background(), Config{Dir: dir, Brief: "Fix the md5 finding.", CanEdit: true, Model: m, Remote: rem,
		OnTool: func(n string, _ json.RawMessage) { notes = append(notes, n) }})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stopped != "done" || res.Turns != 5 || res.Text != "Replaced md5 with sha256." {
		t.Fatalf("result = %+v", res)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "hash.go"))
	if !strings.Contains(string(b), `"crypto/sha256"`) || !strings.Contains(string(b), "sha256.New") {
		t.Fatalf("file = %s", b)
	}
	if len(rem.calls) != 1 || !strings.Contains(rem.calls[0], "md5 replaced") {
		t.Fatalf("remote calls = %v", rem.calls)
	}
	// The read's result, numbered, answered call "a".
	tr := m.seen[1].Messages[2].ToolResults[0]
	if tr.CallID != "a" || !strings.Contains(tr.Content, "     1\tpackage hash") {
		t.Fatalf("read result = %+v", tr)
	}
	if got := m.seen[3].Messages[6].ToolResults[0].Content; !strings.Contains(got, "2\n[exit code 0]") {
		t.Fatalf("run result = %q", got)
	}
	if strings.Join(notes, ",") != "read_file,edit_file,edit_file,run,record_claims" || res.ToolCalls["edit_file"] != 2 {
		t.Fatalf("notes = %v, counts = %v", notes, res.ToolCalls)
	}
}

// A tool's error goes back to the model as the result, so it can correct
// itself; the run goes on.
func TestRunSendsErrorsBack(t *testing.T) {
	dir := checkout(t)
	m := &script{turns: []ollama.ChatResponse{
		{ToolCalls: []ollama.ToolCall{call("a", "edit_file", `{"path":"hash.go","old_string":"sha1","new_string":"x"}`),
			call("b", "nonsense", `{}`)}},
		{Text: "gave up"},
	}}
	if _, err := Run(context.Background(), Config{Dir: dir, Brief: "b", CanEdit: true, Model: m}); err != nil {
		t.Fatal(err)
	}
	rs := m.seen[1].Messages[2].ToolResults
	if !rs[0].IsError || !strings.Contains(rs[0].Content, "not found") || !rs[1].IsError || !strings.Contains(rs[1].Content, "no tool called") {
		t.Fatalf("results = %+v", rs)
	}
}

func TestRunStopsAtTheStepLimit(t *testing.T) {
	dir := checkout(t)
	loop := ollama.ChatResponse{ToolCalls: []ollama.ToolCall{call("a", "list_files", `{"pattern":"*.go"}`)}}
	m := &script{turns: []ollama.ChatResponse{loop, loop, loop, loop}}
	res, err := Run(context.Background(), Config{Dir: dir, Brief: "b", Model: m, StepLimit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stopped != "step limit" || res.Turns != 3 || len(m.seen) != 3 {
		t.Fatalf("result = %+v after %d calls", res, len(m.seen))
	}
}

// A reviewer, or a fixer still planning, is not offered the edit tools, and
// cannot use them by naming them.
func TestRunWithoutEditTools(t *testing.T) {
	dir := checkout(t)
	m := &script{turns: []ollama.ChatResponse{
		{ToolCalls: []ollama.ToolCall{call("a", "write_file", `{"path":"x.go","content":"package x"}`)}},
		{Text: "ok"},
	}}
	if _, err := Run(context.Background(), Config{Dir: dir, Brief: "b", Model: m}); err != nil {
		t.Fatal(err)
	}
	for _, s := range m.seen[0].Tools {
		if s.Name == "edit_file" || s.Name == "write_file" {
			t.Fatalf("offered %s", s.Name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "x.go")); err == nil {
		t.Fatal("a reviewer wrote a file")
	}
}

func TestToolsStayInsideTheCheckout(t *testing.T) {
	dir := checkout(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o644)
	os.Symlink(outside, filepath.Join(dir, "link"))
	lt := newLocalTools(dir, true)
	for _, c := range []struct{ name, args string }{
		{"read_file", `{"path":"../secret"}`},
		{"read_file", `{"path":"` + filepath.Join(outside, "secret") + `"}`},
		{"read_file", `{"path":"link/secret"}`},
		{"write_file", `{"path":"link/new","content":"x"}`},
		{"write_file", `{"path":"../../escape","content":"x"}`},
		{"search", `{"pattern":"s","path":".."}`},
	} {
		out, isErr := lt.call(context.Background(), c.name, json.RawMessage(c.args))
		if !isErr || !strings.Contains(out, "outside the checkout") {
			t.Errorf("%s %s = %q, %v", c.name, c.args, out, isErr)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "new")); err == nil {
		t.Fatal("wrote through a link")
	}
	// An absolute path inside the checkout is fine.
	if out, isErr := lt.call(context.Background(), "read_file", json.RawMessage(`{"path":"`+filepath.Join(dir, "hash.go")+`"}`)); isErr {
		t.Fatalf("absolute inside: %s", out)
	}
}

func TestEditFileMustMatchOnce(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\na\nb\n"), 0o644)
	lt := newLocalTools(dir, true)
	out, isErr := lt.call(context.Background(), "edit_file", json.RawMessage(`{"path":"f.txt","old_string":"a","new_string":"c"}`))
	if !isErr || !strings.Contains(out, "occurs 2 times") {
		t.Fatalf("ambiguous edit = %q", out)
	}
	if out, isErr := lt.call(context.Background(), "edit_file", json.RawMessage(`{"path":"f.txt","old_string":"a","new_string":"c","replace_all":true}`)); isErr {
		t.Fatal(out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(b) != "c\nc\nb\n" {
		t.Fatalf("file = %q", b)
	}
}

func TestRunRefusesGitVerbs(t *testing.T) {
	lt := newLocalTools(t.TempDir(), true)
	for _, c := range []string{"git push origin main", "cd x && git checkout main", "git -c a=b reset --hard", "true; git config user.name x"} {
		out, isErr := lt.run(context.Background(), c, 5)
		if !isErr || !strings.Contains(out, "not allowed") {
			t.Errorf("%q = %q", c, out)
		}
	}
	if out, isErr := lt.run(context.Background(), "git status --short >/dev/null 2>&1; echo fine", 5); isErr || !strings.Contains(out, "fine") {
		t.Errorf("status refused: %q", out)
	}
}

func TestRunReportsExitAndTimeout(t *testing.T) {
	lt := newLocalTools(t.TempDir(), true)
	if out, isErr := lt.run(context.Background(), "echo no; exit 3", 5); !isErr || !strings.Contains(out, "[exit code 3]") {
		t.Fatalf("exit = %q", out)
	}
	if out, isErr := lt.run(context.Background(), "sleep 30", 1); !isErr || !strings.Contains(out, "stopped after 1 seconds") {
		t.Fatalf("timeout = %q", out)
	}
}

func TestListAndSearchRespectGitignore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	exec.Command("git", "-C", dir, "init", "-q").Run()
	os.MkdirAll(filepath.Join(dir, "internal", "api"), 0o755)
	os.MkdirAll(filepath.Join(dir, "node_modules", "x"), 0o755)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main // TODO one\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "internal", "api", "a.go"), []byte("package api\n// TODO two\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "node_modules", "x", "i.go"), []byte("// TODO hidden\n"), 0o644)
	lt := newLocalTools(dir, false)

	out, _ := lt.listFiles("**/*.go")
	if out != "internal/api/a.go\nmain.go" {
		t.Fatalf("list **/*.go = %q", out)
	}
	if out, _ := lt.listFiles("*.go"); out != "internal/api/a.go\nmain.go" {
		t.Fatalf("list *.go = %q", out)
	}
	if out, _ := lt.listFiles("internal/*/*.{go,ts}"); out != "internal/api/a.go" {
		t.Fatalf("list braces = %q", out)
	}
	out, _ = lt.search("TODO", "", "")
	if out != "internal/api/a.go:2: // TODO two\nmain.go:1: package main // TODO one" {
		t.Fatalf("search = %q", out)
	}
	if out, _ := lt.search("TODO", "internal", ""); out != "internal/api/a.go:2: // TODO two" {
		t.Fatalf("search under = %q", out)
	}
}

func TestCapOutputKeepsTheTail(t *testing.T) {
	s := strings.Repeat("x", 40<<10) + "FAIL: TestHash"
	out := capOutput(s)
	if len(out) > outputCap+200 || !strings.HasSuffix(out, "FAIL: TestHash") || !strings.Contains(out, "bytes cut") {
		t.Fatalf("capped to %d, tail %q", len(out), out[len(out)-20:])
	}
}

// Old results give way first when the conversation outgrows the context; the
// newest one, which the model has not read yet, is kept.
func TestTrimContextDropsOldestResults(t *testing.T) {
	big := strings.Repeat("y", 10_000)
	msgs := []ollama.ChatMessage{
		{Role: "user", Text: "brief"},
		{Role: "assistant", ToolCalls: []ollama.ToolCall{call("a", "read_file", `{}`)}},
		{Role: "tool", ToolResults: []ollama.ToolResult{{CallID: "a", Name: "read_file", Content: big}}},
		{Role: "assistant", ToolCalls: []ollama.ToolCall{call("b", "read_file", `{}`)}},
		{Role: "tool", ToolResults: []ollama.ToolResult{{CallID: "b", Name: "read_file", Content: big}}},
	}
	trimContext(msgs, 5_000) // 15,000 characters
	if !strings.Contains(msgs[2].ToolResults[0].Content, "dropped to save room") {
		t.Fatal("oldest result kept")
	}
	if msgs[4].ToolResults[0].Content != big || msgs[0].Text != "brief" {
		t.Fatal("trimmed the newest result or the brief")
	}
}

// Wrong whitespace at line ends is forgiven when the text then matches in
// exactly one place, and never when it matches in two.
func TestEditFileLooseMatch(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "h.go")
	os.WriteFile(f, []byte("package smoke\n\nimport \"crypto/md5\"\n\nfunc A() {}\t\n"), 0o644)
	lt := newLocalTools(dir, true)
	out, isErr := lt.call(context.Background(), "edit_file", json.RawMessage(`{"path":"h.go","old_string":"import \"crypto/md5\" ","new_string":"import \"crypto/sha256\""}`))
	if isErr || !strings.Contains(out, "whitespace") {
		t.Fatalf("loose edit = %q", out)
	}
	out, isErr = lt.call(context.Background(), "edit_file", json.RawMessage(`{"path":"h.go","old_string":"func A() {}\n","new_string":"func B() {}"}`))
	if isErr {
		t.Fatalf("trailing tab edit = %q", out)
	}
	if b, _ := os.ReadFile(f); string(b) != "package smoke\n\nimport \"crypto/sha256\"\n\nfunc B() {}\n" {
		t.Fatalf("file = %q", b)
	}
	os.WriteFile(f, []byte("x := 1 \ny\nx := 1\t\n"), 0o644)
	if out, isErr := lt.call(context.Background(), "edit_file", json.RawMessage(`{"path":"h.go","old_string":"x := 1","new_string":"z"}`)); !isErr || !strings.Contains(out, "occurs 2 times") {
		t.Fatalf("ambiguous = %q", out)
	}
	if out, isErr := lt.call(context.Background(), "edit_file", json.RawMessage(`{"path":"h.go","old_string":"x := 1  \n","new_string":"z"}`)); !isErr || !strings.Contains(out, "not found") {
		t.Fatalf("ambiguous loose = %q", out)
	}
}

// A model that stops right after a failed call is asked once whether it is
// done; one that stops after a success is done.
func TestRunNudgesAfterAFailedCall(t *testing.T) {
	dir := checkout(t)
	m := &script{turns: []ollama.ChatResponse{
		{ToolCalls: []ollama.ToolCall{call("a", "run", `{"command":"exit 1"}`)}},
		{Text: "Let me fix that."},
		{ToolCalls: []ollama.ToolCall{call("b", "run", `{"command":"true"}`)}},
		{Text: "Done."},
	}}
	res, err := Run(context.Background(), Config{Dir: dir, Brief: "b", Model: m})
	if err != nil {
		t.Fatal(err)
	}
	if res.Turns != 4 || res.Text != "Done." || m.seen[2].Messages[len(m.seen[2].Messages)-1].Text != nudge {
		t.Fatalf("result = %+v", res)
	}
}

// Each turn's running totals are reported as the run goes, not only at the end.
func TestRunReportsUsageEachTurn(t *testing.T) {
	m := &script{turns: []ollama.ChatResponse{
		{ToolCalls: []ollama.ToolCall{call("a", "list_files", `{"pattern":"*.go"}`)}, Usage: ollama.ChatUsage{In: 100, Out: 10, CacheRead: 5}},
		{Text: "done", Usage: ollama.ChatUsage{In: 120, Out: 7, CacheWrite: 2}},
	}}
	var seen []string
	res, err := Run(context.Background(), Config{Dir: checkout(t), Brief: "look", Model: m,
		OnTurn: func(turns int, u ollama.ChatUsage) {
			seen = append(seen, fmt.Sprintf("%d:%d/%d/%d/%d", turns, u.In, u.Out, u.CacheRead, u.CacheWrite))
		}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, " ") != "1:100/10/5/0 2:220/17/5/2" {
		t.Fatalf("reported %v", seen)
	}
	if res.Usage != (ollama.ChatUsage{In: 220, Out: 17, CacheRead: 5, CacheWrite: 2}) {
		t.Fatalf("final usage = %+v", res.Usage)
	}
}
