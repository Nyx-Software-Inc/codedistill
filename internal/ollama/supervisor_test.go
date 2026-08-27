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

package ollama

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// discardLogger silences log output inside tests.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestProbe_DeadEndpoint(t *testing.T) {
	if err := probe("http://127.0.0.1:1"); err == nil {
		t.Error("probe against :1 should fail")
	}
}

func TestProbe_LiveEndpoint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Write([]byte(`{"models":[]}`))
	}))
	defer ts.Close()
	if err := probe(ts.URL); err != nil {
		t.Errorf("probe: %v", err)
	}
}

func TestProbe_Rejects5xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	if err := probe(ts.URL); err == nil {
		t.Error("probe should reject 5xx")
	}
}

func TestEnsureRunning_NoopWhenAlreadyUp(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"models":[]}`))
	}))
	defer ts.Close()

	sup := &Supervisor{Endpoint: ts.URL, Log: discardLogger()}
	stop, err := sup.EnsureRunning(context.Background())
	if err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	// Stop must be a no-op that doesn't panic and doesn't block.
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("stop did not return promptly for already-running case")
	}
}

func TestEnsureRunning_ErrorsWhenNothingToSpawn(t *testing.T) {
	// Use an endpoint guaranteed to be dead, and point BinPath at a file
	// that does not exist. Should fail before trying to exec.
	sup := &Supervisor{
		Endpoint: "http://127.0.0.1:1",
		BinPath:  "/definitely/not/a/real/path/ollama",
		Log:      discardLogger(),
	}
	_, err := sup.EnsureRunning(context.Background())
	if err == nil {
		t.Fatal("expected error when binary doesn't exist")
	}
	if !strings.Contains(err.Error(), "ollama binary") {
		t.Errorf("expected error to mention binary path, got: %v", err)
	}
}

func TestEnsureRunning_SpawnAndStop(t *testing.T) {
	// Simulate Ollama with a tiny helper binary — a shell script that starts
	// an HTTP server answering /api/tags on $1 and waits for SIGTERM/SIGINT.
	// This exercises the full Start → wait-for-port → stop path without
	// needing a real Ollama install.
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}

	// Pick a port by listening once and closing.
	ln, err := listenLocal()
	if err != nil {
		t.Fatalf("pick port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	script := `#!/bin/sh
ADDR="$1"
# Start a trivial HTTP responder in the background. Use python3 if present;
# otherwise fall back to a netcat one-shot loop.
trap 'kill $PID 2>/dev/null; exit 0' INT TERM
if command -v python3 >/dev/null 2>&1; then
  python3 -c "
import http.server, socketserver, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(s):
        s.send_response(200); s.end_headers(); s.wfile.write(b'{\"models\":[]}')
    def log_message(s,*a,**k): pass
host, port = sys.argv[1].rsplit(':',1)
socketserver.TCPServer((host, int(port)), H).serve_forever()
" "$ADDR" &
  PID=$!
  wait $PID
else
  exit 2
fi
`
	dir := t.TempDir()
	binPath := dir + "/fake-ollama"
	// Write a wrapper that calls the inline script with the endpoint.
	wrapper := "#!/bin/sh\n# Ignore the 'serve' arg; we only care about the endpoint.\n" +
		"exec /bin/sh " + dir + "/serve.sh " + addr + "\n"
	if err := os.WriteFile(binPath, []byte(wrapper), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	if err := os.WriteFile(dir+"/serve.sh", []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	sup := &Supervisor{
		Endpoint:     "http://" + addr,
		BinPath:      binPath,
		Log:          discardLogger(),
		ReadyTimeout: 5 * time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop, err := sup.EnsureRunning(ctx)
	if err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	// After spawn, the endpoint must actually answer.
	if err := probe(sup.Endpoint); err != nil {
		stop()
		t.Fatalf("spawned child isn't answering: %v", err)
	}
	// Stop should kill the child and return.
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("stop did not return within grace period")
	}
}
