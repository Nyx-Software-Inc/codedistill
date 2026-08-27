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
