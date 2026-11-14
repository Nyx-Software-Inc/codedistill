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

package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runs(p Pipeline) string {
	var s []string
	for _, c := range p.Commands {
		s = append(s, c.Dir+"|"+c.Run)
	}
	return strings.Join(s, "\n")
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Jenkinsfile", `pipeline {
  stages {
    stage('Test') {
      steps {
        sh 'go test -race ./...'
        sh "docker build -t app:${VERSION} ."
        dir('web') {
          sh 'npm ci'
          sh 'npm test'
        }
        sh '''
          go vet ./...
          ./build.sh
        '''
      }
    }
  }
}`)
	write(t, dir, ".github/workflows/ci.yml", `jobs:
  test:
    steps:
      - uses: actions/checkout@v4
      - name: lint
        run: golangci-lint run --timeout 5m
      - name: py
        working-directory: tools
        run: |
          pip install -r requirements.txt
          pytest -q
      - run: echo $SECRET | do-something
`)
	write(t, dir, ".gitlab-ci.yml", `test:
  script:
    - cargo test --all
    - cargo build --release
`)
	write(t, dir, "Makefile", "build:\n\tgo build ./...\ntest:\n\t@go test -tags integration ./...\n")
	p := Detect(dir)
	got := runs(p)
	for _, want := range []string{
		"|go test -race ./...", "web|npm test", "|go vet ./...",
		"|golangci-lint run --timeout 5m", "tools|pytest -q", "|cargo test --all",
		"|go test -tags integration ./...",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, not := range []string{"docker", "npm ci", "build.sh", "pip install", "cargo build", "SECRET", "go build"} {
		if strings.Contains(got, not) {
			t.Errorf("kept %q, which is not a test or lint command:\n%s", not, got)
		}
	}
	if len(p.Files) != 4 {
		t.Errorf("files = %v", p.Files)
	}
}

// CodeDistill's own Jenkinsfile builds and packages; its tests run elsewhere.
func TestDetect_ThisRepository(t *testing.T) {
	p := Detect("../..")
	t.Logf("files %v\n%s", p.Files, runs(p))
}
