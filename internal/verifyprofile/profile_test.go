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

package verifyprofile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		name     string
		files    []string
		wantLang string
		wantTest string
	}{
		{"go", []string{"go.mod"}, "Go", "go test ./..."},
		{"maven", []string{"pom.xml"}, "Java (Maven)", "mvn -q test"},
		{"gradle_groovy", []string{"build.gradle"}, "Java (Gradle)", "./gradlew test"},
		{"gradle_kts_is_kotlin", []string{"build.gradle.kts"}, "Kotlin (Gradle)", "./gradlew test"},
		{"csharp_csproj", []string{"App.csproj"}, "C#/.NET", "dotnet test"},
		{"csharp_sln", []string{"Solution.sln"}, "C#/.NET", "dotnet test"},
		{"node", []string{"package.json"}, "Node", "npm test"},
		{"python", []string{"requirements.txt"}, "Python", "pytest"},
		{"rust", []string{"Cargo.toml"}, "Rust", "cargo test"},
		{"ruby", []string{"Gemfile"}, "Ruby", "bundle exec rspec"},
		{"cpp_cmake", []string{"CMakeLists.txt"}, "C/C++ (CMake)", "ctest --output-on-failure"},
		{"unknown", []string{"README.md"}, "", ""},
		// Polyglot: go.mod at root wins over a nested-marker (here both at root,
		// Go has priority) — matches CodeDistill's own go.mod-at-root layout.
		{"polyglot_go_wins", []string{"go.mod", "package.json"}, "Go", "go test ./..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got := Detect(dir)
			if got.Language != tc.wantLang {
				t.Errorf("Language = %q, want %q", got.Language, tc.wantLang)
			}
			if got.Test != tc.wantTest {
				t.Errorf("Test = %q, want %q", got.Test, tc.wantTest)
			}
		})
	}
}

// A Groovy build.gradle that applies the Kotlin plugin should detect as Kotlin
// even though the DSL file isn't .kts.
func TestDetectGradleKotlinPlugin(t *testing.T) {
	dir := t.TempDir()
	body := "plugins {\n  id 'org.jetbrains.kotlin.jvm' version '1.9.0'\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "build.gradle"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Detect(dir); got.Language != "Kotlin (Gradle)" {
		t.Errorf("Language = %q, want %q", got.Language, "Kotlin (Gradle)")
	}
}

func TestDetectEmptyRoot(t *testing.T) {
	if got := Detect(""); got.Language != "" {
		t.Errorf("empty root: Language = %q, want \"\"", got.Language)
	}
}

// writeTree creates files (with contents) under dir, making parent dirs.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A go.work root with no go.mod (locator's layout) is Go, and its commands
// target the workspace modules — `./...` from that root is rejected by go.
func TestDetectGoWorkspaceSingleModule(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"go.work":          "go 1.26.0\n\nuse ./backend\n",
		"backend/go.mod":   "module x\n",
		"web/package.json": "{}",
	})
	got := Detect(dir)
	want := Profile{
		Language: "Go (workspace)",
		Test:     "go test ./backend/...",
		Lint:     "cd backend && golangci-lint run",
		Types:    "go vet ./backend/...",
		Sast:     "cd backend && gosec ./...",
		Vuln:     "cd backend && govulncheck ./...",
	}
	if got != want {
		t.Errorf("Detect =\n %+v\nwant\n %+v", got, want)
	}
}

func TestDetectGoWorkspaceBlockUse(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"go.work":        "go 1.22\n\nuse (\n\t./svc/api // the API\n\t\"./shared\"\n\t./gone\n)\n",
		"svc/api/go.mod": "module a\n",
		"shared/go.mod":  "module b\n",
		// ./gone has no go.mod — dropped rather than emitted into a command.
	})
	if mods := GoModules(dir); len(mods) != 2 || mods[0] != "svc/api" || mods[1] != "shared" {
		t.Fatalf("GoModules = %q, want [svc/api shared]", mods)
	}
	got := Detect(dir)
	if got.Test != "go test ./svc/api/... ./shared/..." {
		t.Errorf("Test = %q", got.Test)
	}
	if got.Sast != "(cd svc/api && gosec ./...) && (cd shared && gosec ./...)" {
		t.Errorf("Sast = %q", got.Sast)
	}
}

// go.mod at the root wins over a go.work beside it: it is a module, so the
// plain ./... examples are right and GoModules is just the root.
func TestDetectGoModBeatsGoWork(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"go.mod": "module x\n", "go.work": "use .\n"})
	if got := Detect(dir); got != profGo {
		t.Errorf("Detect = %+v, want profGo", got)
	}
	if mods := GoModules(dir); len(mods) != 1 || mods[0] != "." {
		t.Errorf("GoModules = %q, want [.]", mods)
	}
}

func TestDetectGoWorkspaceNoUsableModules(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"go.work": "go 1.22\n"})
	if got := Detect(dir); got.Language != "Go" {
		t.Errorf("Language = %q, want Go", got.Language)
	}
}
