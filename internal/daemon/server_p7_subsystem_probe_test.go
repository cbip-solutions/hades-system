package daemon

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeSubsystemProber struct {
	rows []ProbeRow
	err  error
}

func (f *fakeSubsystemProber) SubsystemProbe(_ context.Context) ([]ProbeRow, error) {
	return f.rows, f.err
}

func newServerForSubsystemProbeTest(t *testing.T) *Server {
	t.Helper()
	st := newTestStore(t)
	return New(st, Config{DisableAuditInfra: true})
}

func TestSubsystemProbeUnwiredReturnsEmpty(t *testing.T) {
	s := newServerForSubsystemProbeTest(t)
	rows, err := s.SubsystemProbe(context.Background(), "knowledge")
	if err != nil {
		t.Fatalf("SubsystemProbe: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("unwired SubsystemProbe returned %d rows, want 0", len(rows))
	}
}

func TestSetKnowledgeProberThenSubsystemProbe(t *testing.T) {
	s := newServerForSubsystemProbeTest(t)
	want := []ProbeRow{
		{Name: "knowledge.index.integrity", Status: "ok", Message: "PRAGMA integrity_check = ok"},
	}
	s.SetKnowledgeProber(&fakeSubsystemProber{rows: want})
	got, err := s.SubsystemProbe(context.Background(), "knowledge")
	if err != nil {
		t.Fatalf("SubsystemProbe: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SubsystemProbe = %+v, want %+v", got, want)
	}
}

func TestSetSchedulerProberThenSubsystemProbe(t *testing.T) {
	s := newServerForSubsystemProbeTest(t)
	want := []ProbeRow{{Name: "scheduler.queue.depth", Status: "ok", Message: "depth=0"}}
	s.SetSchedulerProber(&fakeSubsystemProber{rows: want})
	got, err := s.SubsystemProbe(context.Background(), "scheduler")
	if err != nil {
		t.Fatalf("SubsystemProbe: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SubsystemProbe = %+v, want %+v", got, want)
	}
}

func TestSetInboxProberThenSubsystemProbe(t *testing.T) {
	s := newServerForSubsystemProbeTest(t)
	want := []ProbeRow{{Name: "inbox.aggregator.cache.consistent", Status: "ok", Message: "rows=12"}}
	s.SetInboxProber(&fakeSubsystemProber{rows: want})
	got, err := s.SubsystemProbe(context.Background(), "inbox")
	if err != nil {
		t.Fatalf("SubsystemProbe: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SubsystemProbe = %+v, want %+v", got, want)
	}
}

func TestSetTmuxProberThenSubsystemProbe(t *testing.T) {
	s := newServerForSubsystemProbeTest(t)
	want := []ProbeRow{{Name: "tmux.server.reachable", Status: "ok", Message: "/tmp/zen-swarm.sock"}}
	s.SetTmuxProber(&fakeSubsystemProber{rows: want})
	got, err := s.SubsystemProbe(context.Background(), "tmux")
	if err != nil {
		t.Fatalf("SubsystemProbe: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SubsystemProbe = %+v, want %+v", got, want)
	}
}

func TestSetNilProberUnwires(t *testing.T) {
	s := newServerForSubsystemProbeTest(t)
	s.SetKnowledgeProber(&fakeSubsystemProber{rows: []ProbeRow{{Name: "x"}}})

	rows, _ := s.SubsystemProbe(context.Background(), "knowledge")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row before unwire, got %d", len(rows))
	}

	s.SetKnowledgeProber(nil)
	rows, _ = s.SubsystemProbe(context.Background(), "knowledge")
	if len(rows) != 0 {
		t.Errorf("expected 0 rows after unwire, got %d", len(rows))
	}
}

func TestSubsystemProbeUnknownNameReturnsEmpty(t *testing.T) {
	s := newServerForSubsystemProbeTest(t)
	rows, err := s.SubsystemProbe(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("SubsystemProbe: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("unknown name returned %d rows, want 0", len(rows))
	}
}

func TestSubsystemProbePropagatesProberError(t *testing.T) {
	s := newServerForSubsystemProbeTest(t)
	wantErr := errors.New("synthetic probe error")
	s.SetKnowledgeProber(&fakeSubsystemProber{err: wantErr})
	_, err := s.SubsystemProbe(context.Background(), "knowledge")
	if !errors.Is(err, wantErr) {
		t.Errorf("SubsystemProbe err = %v, want %v", err, wantErr)
	}
}

func TestRegisteredSubsystemProbersReturnsSorted(t *testing.T) {
	s := newServerForSubsystemProbeTest(t)
	s.SetTmuxProber(&fakeSubsystemProber{})
	s.SetKnowledgeProber(&fakeSubsystemProber{})
	s.SetInboxProber(&fakeSubsystemProber{})
	s.SetSchedulerProber(&fakeSubsystemProber{})
	got := s.RegisteredSubsystemProbers()
	want := []string{"inbox", "knowledge", "scheduler", "tmux"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RegisteredSubsystemProbers = %v, want %v", got, want)
	}
}

func TestRegisteredSubsystemProbersEmptyByDefault(t *testing.T) {
	s := newServerForSubsystemProbeTest(t)
	got := s.RegisteredSubsystemProbers()
	if len(got) != 0 {
		t.Errorf("RegisteredSubsystemProbers default = %v, want []", got)
	}
}
