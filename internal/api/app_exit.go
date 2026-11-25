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

package api

import (
	"net/http"
	"time"

	"codedistill/internal/domain"
)

// File → Exit, in the desktop app. Every window the app has open — the main
// one and every popped-out pane — belongs to one browser process, and the
// server stops when that process ends. Closing the main window alone used to
// leave popped-out panes, the browser and so the server running unseen. Exit
// asks the server to close the browser, which closes every window, and stop.
//
// Only a server that opened the app window can be exited this way: a server
// people reach from their own browsers is not one person's to stop.

// SetExit makes the server a desktop app's: exit stops it, browser and all.
func (s *Server) SetExit(exit func()) {
	s.exitMu.Lock()
	defer s.exitMu.Unlock()
	s.exit = exit
}

func (s *Server) exitFunc() func() {
	s.exitMu.Lock()
	defer s.exitMu.Unlock()
	return s.exit
}

// appInfo is GET /api/v1/app: whether this is the desktop app (so the UI
// offers File → Exit), and how many jobs Exit would stop.
func (s *Server) appInfo(w http.ResponseWriter, r *http.Request) {
	running := 0
	if jobs, err := s.store.ListRunningJobs(r.Context()); err == nil {
		for _, j := range jobs {
			if j.Status == domain.JobRunning {
				running++
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"desktop_app": s.exitFunc() != nil, "running_jobs": running})
}

// appExit is POST /api/v1/app/exit. It answers first, then stops, so the
// window hears that the exit was accepted before the browser closes it.
func (s *Server) appExit(w http.ResponseWriter, r *http.Request) {
	exit := s.exitFunc()
	if exit == nil {
		writeMsg(w, http.StatusNotFound, "this server is not a desktop app: close the browser window, and stop the server where it runs")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "exiting"})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		exit()
	}()
}
