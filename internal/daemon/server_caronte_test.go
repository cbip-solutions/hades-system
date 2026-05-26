package daemon

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	research "github.com/cbip-solutions/hades-system/internal/mcp/research"
)

type fakeCaronteForDaemon struct{ closed bool }

func (f *fakeCaronteForDaemon) CodeGraph(_ context.Context, query, projectID string) (research.CodeGraphResult, error) {
	return research.CodeGraphResult{Hits: []research.CodeGraphHit{{Node: query}}, ProjectID: projectID}, nil
}

func (f *fakeCaronteForDaemon) IndexProject(_ context.Context, projectID string) (handlers.CaronteReindexReport, error) {
	return handlers.CaronteReindexReport{
		ProjectID:      projectID,
		LanguageCounts: map[string]int{},
		Completed:      true,
	}, nil
}

func (f *fakeCaronteForDaemon) Close() error { f.closed = true; return nil }

func TestCaronteEngineNilByDefault(t *testing.T) {
	s := newTestServer(t)
	if s.CaronteEngine() != nil {
		t.Error("CaronteEngine() non-nil before SetCaronteEngine")
	}
}

func TestSetCaronteEngineRoundTrip(t *testing.T) {
	s := newTestServer(t)
	fe := &fakeCaronteForDaemon{}
	s.SetCaronteEngine(fe)
	got := s.CaronteEngine()
	if got == nil {
		t.Fatal("CaronteEngine() nil after SetCaronteEngine")
	}
	res, err := got.CodeGraph(context.Background(), "X", "p")
	if err != nil || len(res.Hits) != 1 || res.Hits[0].Node != "X" {
		t.Errorf("round-trip engine misbehaved: res=%+v err=%v", res, err)
	}
}

func TestSetCaronteEngineNilSafe(t *testing.T) {
	s := newTestServer(t)
	fe := &fakeCaronteForDaemon{}
	s.SetCaronteEngine(fe)
	if s.CaronteEngine() == nil {
		t.Fatal("CaronteEngine() nil after first Set")
	}

	s.SetCaronteEngine(nil)
	if s.CaronteEngine() != nil {
		t.Error("CaronteEngine() non-nil after SetCaronteEngine(nil)")
	}
}

func TestBGEAvailableDefaultFalse(t *testing.T) {
	s := newTestServer(t)
	if s.BGEAvailable() {
		t.Error("BGEAvailable() = true before SetBGEAvailable; want false (degraded-default)")
	}
}

func TestSetBGEAvailableRoundTrip(t *testing.T) {
	s := newTestServer(t)
	s.SetBGEAvailable(true)
	if !s.BGEAvailable() {
		t.Error("BGEAvailable() = false after SetBGEAvailable(true)")
	}
	s.SetBGEAvailable(false)
	if s.BGEAvailable() {
		t.Error("BGEAvailable() = true after SetBGEAvailable(false)")
	}
}
