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

// Package verifyprofile derives sensible example verification commands for a
// project from its repo layout. The Verification settings (test/lint/types/
// sast/vuln commands the glass box runs) historically showed Go examples as
// placeholder hints regardless of the actual project — wrong and confusing the
// moment you point CodeDistill at a Java or Python repo. Detecting the language
// from root marker files (go.mod, pom.xml, package.json, …) lets the UI show
// the right examples and offer one-click defaults.
package verifyprofile

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Profile is the per-language set of example verification commands plus the
// detected language label. An empty Language means we couldn't tell (no known
// marker file, or no repo_root configured) — the UI falls back to neutral hints.
// An individual command may be empty when a language has no ubiquitous tool for
// that check (e.g. no standard Rust SAST); the UI just shows a neutral hint there.
type Profile struct {
	Language string `json:"language"` // "Go", "Java (Maven)", "C#/.NET", …; "" if unknown
	Test     string `json:"test"`
	Lint     string `json:"lint"`
	Types    string `json:"types"`
	Sast     string `json:"sast"`
	Vuln     string `json:"vuln"`
}

// Language profiles. Kept as named vars because a couple are reached from more
// than one detection path (Kotlin vs Java both build with Gradle).
var (
	profGo = Profile{
		Language: "Go",
		Test:     "go test ./...",
		Lint:     "golangci-lint run",
		Types:    "go vet ./...",
		Sast:     "gosec ./...",
		Vuln:     "govulncheck ./...",
	}
	profMaven = Profile{
		Language: "Java (Maven)",
		Test:     "mvn -q test",
		Lint:     "mvn -q checkstyle:check",
		Types:    "mvn -q compile",
		Sast:     "mvn -q spotbugs:check",
		Vuln:     "mvn -q org.owasp:dependency-check-maven:check",
	}
	profGradleJava = Profile{
		Language: "Java (Gradle)",
		Test:     "./gradlew test",
		Lint:     "./gradlew checkstyleMain",
		Types:    "./gradlew compileJava",
		Sast:     "./gradlew spotbugsMain",
		Vuln:     "./gradlew dependencyCheckAnalyze",
	}
	profKotlin = Profile{
		Language: "Kotlin (Gradle)",
		Test:     "./gradlew test",
		Lint:     "./gradlew ktlintCheck",
		Types:    "./gradlew compileKotlin",
		Sast:     "./gradlew detekt",
		Vuln:     "./gradlew dependencyCheckAnalyze",
	}
	profDotNet = Profile{
		Language: "C#/.NET",
		Test:     "dotnet test",
		Lint:     "dotnet format --verify-no-changes",
		Types:    "dotnet build",
		Sast:     "", // no ubiquitous .NET SAST CLI; leave blank rather than guess
		Vuln:     "dotnet list package --vulnerable",
	}
	profNode = Profile{
		Language: "Node",
		Test:     "npm test",
		Lint:     "npm run lint",
		Types:    "npx tsc --noEmit",
		Sast:     "npx semgrep --config auto",
		Vuln:     "npm audit",
	}
	profPython = Profile{
		Language: "Python",
		Test:     "pytest",
		Lint:     "ruff check .",
		Types:    "mypy .",
		Sast:     "bandit -r .",
		Vuln:     "pip-audit",
	}
	profRust = Profile{
		Language: "Rust",
		Test:     "cargo test",
		Lint:     "cargo clippy -- -D warnings",
		Types:    "cargo check",
		Sast:     "", // no ubiquitous Rust SAST; leave blank rather than guess
		Vuln:     "cargo audit",
	}
	profRuby = Profile{
		Language: "Ruby",
		Test:     "bundle exec rspec",
		Lint:     "bundle exec rubocop",
		Types:    "", // Ruby type-checking (Sorbet/Steep) isn't ubiquitous
		Sast:     "bundle exec brakeman",
		Vuln:     "bundle exec bundle audit",
	}
	profCpp = Profile{
		Language: "C/C++ (CMake)",
		Test:     "ctest --output-on-failure",
		Lint:     "clang-tidy",
		Types:    "cmake --build build",
		Sast:     "cppcheck --enable=warning,performance,portability .",
		Vuln:     "", // no ubiquitous C/C++ dependency-vuln CLI
	}
)

// Detect returns the example command profile for the repo at repoRoot. An empty
// repoRoot, an unreadable path, or no recognized marker yields a zero Profile
// (Language == ""), which callers treat as "unknown — show neutral hints".
//
// Detectors are tried in priority order. The order matters for polyglot repos:
// Go leads because CodeDistill's own go.mod sits at the root while package.json
// lives under web/, so root-nearest markers win.
func Detect(repoRoot string) Profile {
	if repoRoot == "" {
		return Profile{}
	}
	switch {
	case hasFile(repoRoot, "go.mod"):
		return profGo
	case hasFile(repoRoot, "go.work"):
		// A workspace root with no go.mod of its own (locator: go.work →
		// use ./backend). `./...` is wrong here, not just imprecise: go
		// rejects it outright ("directory prefix . does not contain modules
		// listed in go.work"), and gosec run from this root exits 0 with an
		// empty report — a clean bill of health for code it never looked at.
		return goWorkspaceProfile(GoModules(repoRoot))
	case hasFile(repoRoot, "pom.xml"):
		return profMaven
	case hasFile(repoRoot, "build.gradle", "build.gradle.kts"):
		// Kotlin and Java both build with Gradle — distinguish by sniffing the
		// build script for the Kotlin plugin (and a .kts DSL is a strong tell).
		if gradleIsKotlin(repoRoot) {
			return profKotlin
		}
		return profGradleJava
	case hasGlob(repoRoot, "*.sln", "*.csproj", "*.fsproj"):
		return profDotNet
	case hasFile(repoRoot, "package.json"):
		return profNode
	case hasFile(repoRoot, "pyproject.toml", "setup.py", "setup.cfg", "requirements.txt"):
		return profPython
	case hasFile(repoRoot, "Cargo.toml"):
		return profRust
	case hasFile(repoRoot, "Gemfile"):
		return profRuby
	case hasFile(repoRoot, "CMakeLists.txt"):
		return profCpp
	}
	return Profile{}
}

// hasFile reports whether any of the named files exists directly in dir.
func hasFile(dir string, names ...string) bool {
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			return true
		}
	}
	return false
}

// hasGlob reports whether any of the glob patterns matches a file in dir. Used
// for languages whose marker filenames vary (C#/.NET: Foo.csproj, Foo.sln).
func hasGlob(dir string, patterns ...string) bool {
	for _, p := range patterns {
		if m, err := filepath.Glob(filepath.Join(dir, p)); err == nil && len(m) > 0 {
			return true
		}
	}
	return false
}

// gradleIsKotlin sniffs the Gradle build script for evidence of a Kotlin
// project. A build.gradle.kts (Kotlin DSL) or a kotlin plugin reference both
// signal Kotlin; otherwise we treat the Gradle project as Java.
func gradleIsKotlin(dir string) bool {
	if hasFile(dir, "build.gradle.kts") {
		return true
	}
	b, err := os.ReadFile(filepath.Join(dir, "build.gradle"))
	if err != nil {
		return false
	}
	return strings.Contains(string(b), "kotlin")
}

// GoModules returns the Go module directories under repoRoot, slash-separated
// and relative to it: ["."] for a plain go.mod repo, the go.work `use` entries
// for a workspace, nil when neither applies. Callers that run Go tools need
// this because in a workspace each tool has to be pointed at (or run inside)
// the modules themselves — the workspace root is not a module.
func GoModules(repoRoot string) []string {
	if repoRoot == "" {
		return nil
	}
	if hasFile(repoRoot, "go.mod") {
		return []string{"."}
	}
	b, err := os.ReadFile(filepath.Join(repoRoot, "go.work"))
	if err != nil {
		return nil
	}
	var mods []string
	for _, dir := range parseGoWorkUses(string(b)) {
		// A `use` naming a directory with no go.mod is a go.work the go
		// command itself would reject; skip it rather than generate a
		// command that fails for a reason unrelated to the user's code.
		if !hasFile(filepath.Join(repoRoot, filepath.FromSlash(dir)), "go.mod") {
			continue
		}
		mods = append(mods, dir)
	}
	return mods
}

// parseGoWorkUses pulls the `use` directories out of a go.work file — both the
// single-line form (`use ./backend`) and the block form (`use ( … )`). It is a
// deliberately small parser rather than golang.org/x/mod/modfile: `use` is the
// only directive we need, and the grammar for it is a line of one path.
func parseGoWorkUses(src string) []string {
	var dirs []string
	inBlock := false
	add := func(s string) {
		s = strings.Trim(strings.TrimSpace(s), "\"`")
		if s == "" {
			return
		}
		dirs = append(dirs, path.Clean(filepath.ToSlash(s)))
	}
	for _, line := range strings.Split(src, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		switch {
		case inBlock && line == ")":
			inBlock = false
		case inBlock:
			add(line)
		case line == "use (" || line == "use(":
			inBlock = true
		case strings.HasPrefix(line, "use ") || strings.HasPrefix(line, "use\t"):
			add(line[len("use"):])
		}
	}
	return dirs
}

// goWorkspaceProfile builds the Go examples for a workspace from its modules.
// go test / go vet take package patterns, and workspace mode lets one
// invocation span modules (./backend/... ./shared/...). The third-party tools
// are run inside each module instead: gosec is the one verified to report
// nothing from the workspace root, and per-module is the form golangci-lint
// and govulncheck are known to handle.
func goWorkspaceProfile(mods []string) Profile {
	if len(mods) == 0 {
		// go.work with no usable `use` — still Go, so show the familiar
		// examples rather than pretend the language is unknown.
		return profGo
	}
	pats := make([]string, len(mods))
	for i, m := range mods {
		pats[i] = "./" + m + "/..."
	}
	all := strings.Join(pats, " ")
	return Profile{
		Language: "Go (workspace)",
		Test:     "go test " + all,
		Lint:     inEachModule(mods, "golangci-lint run"),
		Types:    "go vet " + all,
		Sast:     inEachModule(mods, "gosec ./..."),
		Vuln:     inEachModule(mods, "govulncheck ./..."),
	}
}

// inEachModule runs cmd inside every module, failing if any run fails. The
// commands go through `sh -c` (internal/verify/runner.go), so a subshell per
// module keeps each cd from leaking into the next.
func inEachModule(mods []string, cmd string) string {
	if len(mods) == 1 {
		return "cd " + mods[0] + " && " + cmd
	}
	parts := make([]string, len(mods))
	for i, m := range mods {
		parts[i] = "(cd " + m + " && " + cmd + ")"
	}
	return strings.Join(parts, " && ")
}
