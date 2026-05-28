// SPDX-License-Identifier: MIT
// Package daemon — server_p7_subsystem_probe.go
//
// `cmd/hades-ctld/main.go` invokes Server.SubsystemProbe every 5
// minutes per subsystem ("knowledge", "scheduler", "inbox", "tmux") and
// emits a structured slog line with the per-status counts. The output
// timeline is the substrate HADES design hash-chain extension hooks anchor
// against (per design choice D); the slog output lands in stderr /
// the launchd log file.
//
// J-7 ships the dispatcher contract + a no-op fallback that returns an
// empty slice. probers (concrete types living in
// internal/{knowledge,scheduler,inbox,tmuxlife}) wire via SetXxxProber
// setters in a follow-up; until then SubsystemProbe returns []ProbeRow{}
// and the snapshot logger emits "subsystem unwired" with status counts
// all zero — operationally inert but observable.
//
// invariant boundary: this file lives in internal/daemon (which already
// imports internal/store + every HADES design subsystem package indirectly via
// the adapters). The per-subsystem prober concrete types do not violate
// the boundary because they are dependency-injected (SetXxxProber); the
// Server treats them as opaque interfaces.
package daemon

import (
	"context"
	"sort"
)

type ProbeRow struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type SubsystemProber interface {
	// SubsystemProbe runs the subsystem's full probe surface and returns
	// a slice of probe rows. MUST honour ctx cancellation; SHOULD return
	// within 5 seconds. Concrete types live in subsystem packages; the
	// adapters compose their typed Prober's methods into the uniform
	// []ProbeRow shape expected by the daemon snapshot logger.
	SubsystemProbe(ctx context.Context) ([]ProbeRow, error)
}

func (s *Server) SetKnowledgeProber(p SubsystemProber) {
	s.setSubsystemProber("knowledge", p)
}

func (s *Server) SetSchedulerProber(p SubsystemProber) {
	s.setSubsystemProber("scheduler", p)
}

func (s *Server) SetInboxProber(p SubsystemProber) {
	s.setSubsystemProber("inbox", p)
}

func (s *Server) SetTmuxProber(p SubsystemProber) {
	s.setSubsystemProber("tmux", p)
}

func (s *Server) setSubsystemProber(name string, p SubsystemProber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subsystemProbers == nil {
		s.subsystemProbers = map[string]SubsystemProber{}
	}
	if p == nil {
		delete(s.subsystemProbers, name)
		return
	}
	s.subsystemProbers[name] = p
}

// SubsystemProbe dispatches by subsystem name to the registered prober.
// Returns the prober's probe rows, or empty slice if no prober is
// registered for the name. The snapshot logger interprets an empty slice
// as "subsystem unwired" + emits zero counts — operator sees the rendering
// without an error.
//
// Returns (empty, nil) if name is unknown OR prober is unwired. Test
// fixtures rely on the (empty, nil) shape rather than an error so the
// snapshot logger's loop logic stays simple ("if err == nil and len > 0
// then count statuses").
//
// J-7 contract: the dispatcher MUST NOT return an error for "prober not
// configured" — that condition is operationally normal during the
// pre- bring-up window. Errors are reserved for actual probe
// invocation failures (the prober returned a non-nil error).
func (s *Server) SubsystemProbe(ctx context.Context, name string) ([]ProbeRow, error) {
	s.mu.Lock()
	var p SubsystemProber
	if s.subsystemProbers != nil {
		p = s.subsystemProbers[name]
	}
	s.mu.Unlock()
	if p == nil {

		return []ProbeRow{}, nil
	}
	return p.SubsystemProbe(ctx)
}

func (s *Server) RegisteredSubsystemProbers() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.subsystemProbers))
	for k := range s.subsystemProbers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
