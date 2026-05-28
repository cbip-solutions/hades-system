// SPDX-License-Identifier: MIT
package tmuxlife

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

type DriftType int

const (
	DriftPaneAdded DriftType = iota

	DriftPaneRemoved

	DriftWindowKilled

	DriftWindowRenamed
)

func (d DriftType) String() string {
	switch d {
	case DriftPaneAdded:
		return "PaneAdded"
	case DriftPaneRemoved:
		return "PaneRemoved"
	case DriftWindowKilled:
		return "WindowKilled"
	case DriftWindowRenamed:
		return "WindowRenamed"
	default:
		return fmt.Sprintf("DriftType(%d)", int(d))
	}
}

type LayoutDrift struct {
	SessionName string
	Window      WindowName
	Type        DriftType
	PaneTitle   string
	ObservedAt  time.Time
}

func (d LayoutDrift) IsValid() bool {
	if d.SessionName == "" || d.Window == "" {
		return false
	}
	return d.Type >= DriftPaneAdded && d.Type <= DriftWindowRenamed
}

func (d LayoutDrift) String() string {
	return fmt.Sprintf("%s:%s %s", d.SessionName, d.Window, d.Type)
}

type paneRecord struct {
	ID    string
	Title string
}

type PaneLister interface {
	ListPanes(ctx context.Context, sessionName string, window WindowName) ([]paneRecord, error)
}

// DriftEmitter abstracts the event-emission seam. The real implementation
// wraps the orchestrator eventlog. Tests use a fake that records
// emissions in a slice. Forensic-only contract: emitter never auto-reverts;
// receivers MAY surface drift in operator-visible UIs (hades day brief)
// but MUST NOT mutate tmux state.
type DriftEmitter interface {
	Emit(d LayoutDrift)
}

type DriftPoller struct {
	store    SessionStore
	lister   PaneLister
	emitter  DriftEmitter
	interval time.Duration

	logger *log.Logger

	now func() time.Time
}

func NewDriftPoller(store SessionStore, lister PaneLister, emitter DriftEmitter, interval time.Duration) *DriftPoller {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &DriftPoller{
		store:    store,
		lister:   lister,
		emitter:  emitter,
		interval: interval,
		logger:   log.Default(),
		now:      time.Now,
	}
}

func (p *DriftPoller) Run(ctx context.Context) {
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.tick(ctx); err != nil {
				p.logger.Printf("tmuxlife.DriftPoller: tick error: %v", err)
			}
		}
	}
}

// tick performs one drift sweep across all active sessions. Errors on
// individual session/window lookups do NOT abort the sweep — one bad
// session must not block the rest. The only error returned from tick()
// is a ListSessions failure (the whole walk cannot proceed), which Run()
// then logs.
//
// invariant guarantees scratch is never inspected: the poller iterates
// over DaemonOwnedWindows (which excludes WindowScratch). A defensive
// guard inside the loop logs and skips if WindowScratch ever appears in
// DaemonOwnedWindows (compile-time test TestDaemonOwnedExcludesScratch
// is the first layer; this is the runtime second layer).
func (p *DriftPoller) tick(ctx context.Context) error {
	sessions, err := p.store.ListSessions()
	if err != nil {
		return fmt.Errorf("tmuxlife.DriftPoller.tick: ListSessions: %w", err)
	}
	for _, s := range sessions {
		if s.Status != StatusActive {
			continue
		}
		expectedByWindow, err := p.store.ExpectedPanesFor(s.Name)
		if err != nil {
			p.logger.Printf("tmuxlife.DriftPoller: ExpectedPanesFor(%q) error: %v", s.Name, err)
			continue
		}
		for _, win := range DaemonOwnedWindows {

			if win == WindowScratch {
				p.logger.Printf("tmuxlife.DriftPoller: scratch in DaemonOwnedWindows; invariant violated")
				continue
			}
			actual, err := p.lister.ListPanes(ctx, s.Name, win)
			if err != nil {

				p.logger.Printf("tmuxlife.DriftPoller: ListPanes(%s:%s) error: %v", s.Name, win, err)
				continue
			}
			expected := expectedByWindow[win]
			p.compareAndEmit(s.Name, win, expected, actual)
		}
	}
	return nil
}

func (p *DriftPoller) compareAndEmit(sessionName string, win WindowName, expected []string, actual []paneRecord) {
	expectedSet := make(map[string]struct{}, len(expected))
	for _, id := range expected {
		expectedSet[id] = struct{}{}
	}
	actualByID := make(map[string]paneRecord, len(actual))
	for _, a := range actual {
		actualByID[a.ID] = a
	}

	if len(expected) > 0 && len(actual) == 0 {
		p.emitter.Emit(LayoutDrift{
			SessionName: sessionName,
			Window:      win,
			Type:        DriftWindowKilled,
			ObservedAt:  p.now(),
		})
		return
	}

	addedIDs := make([]string, 0)
	for id := range actualByID {
		if _, ok := expectedSet[id]; !ok {
			addedIDs = append(addedIDs, id)
		}
	}
	sort.Strings(addedIDs)
	for _, id := range addedIDs {
		p.emitter.Emit(LayoutDrift{
			SessionName: sessionName,
			Window:      win,
			Type:        DriftPaneAdded,
			PaneTitle:   actualByID[id].Title,
			ObservedAt:  p.now(),
		})
	}

	removedIDs := make([]string, 0)
	for id := range expectedSet {
		if _, ok := actualByID[id]; !ok {
			removedIDs = append(removedIDs, id)
		}
	}
	sort.Strings(removedIDs)
	for _, id := range removedIDs {
		p.emitter.Emit(LayoutDrift{
			SessionName: sessionName,
			Window:      win,
			Type:        DriftPaneRemoved,
			PaneTitle:   id,
			ObservedAt:  p.now(),
		})
	}
}

type RealPaneLister struct{}

func (RealPaneLister) ListPanes(ctx context.Context, sessionName string, window WindowName) ([]paneRecord, error) {
	target := sessionName + ":" + string(window)
	out, err := ExecTmux(ctx, "-S", SocketPath, "list-panes", "-t", target, "-F", "#{pane_id}|#{pane_title}")
	if err != nil {

		return nil, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	records := make([]paneRecord, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		rec := paneRecord{ID: parts[0]}
		if len(parts) == 2 {
			rec.Title = parts[1]
		}
		records = append(records, rec)
	}
	return records, nil
}
