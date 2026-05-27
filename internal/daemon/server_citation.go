// Copyright 2026 hades-system contributors. SPDX-License-Identifier: MIT

package daemon

import "github.com/cbip-solutions/hades-system/internal/citation"

func (s *Server) SetCitationRegistry(reg *citation.Registry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.citationRegistry = reg
}

// Citations returns the daemon-owned *citation.Registry. nil when
// SetCitationRegistry has not been called (tests, partial bootstrap).
//
// Consumers MUST nil-check the return before calling Dispatch /
// Lookup — the citation package's own Dispatch already handles a
// nil Registry gracefully via ErrNoRendererMatch, but release
// platform renderer code at the daemon layer should branch on nil
// to surface a clear "citation substrate unavailable" error rather
// than a panic.
//
// Goroutine-safe via Server.mu.
func (s *Server) Citations() *citation.Registry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.citationRegistry
}
