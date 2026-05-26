package mcpgateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte"
)

func TestHandleGetBreakingChangesMissingWorkspaceArg(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_breaking_changes"),
		Args:      map[string]any{},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err == nil || !strings.Contains(err.Error(), `missing "workspace"`) {
		t.Errorf("err = %v; want missing-workspace validation error", err)
	}
}

func TestHandleGetBreakingChangesEngineErrorEscalates(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{failAll: true}, NopAuditEmitter())
	for _, mode := range []Mode{ModeAutonomy, ModeAFK, ModeInteractive} {
		_, err := p.CallByTool(context.Background(), CallRequest{
			Tool:      MustToolName("caronte", "get_breaking_changes"),
			Args:      map[string]any{"workspace": "ws-1"},
			ProjectID: "proj-1",
			Mode:      mode,
		})
		if !errors.Is(err, ErrCaronteUnreachable) {
			t.Errorf("mode=%s: err = %v; want ErrCaronteUnreachable", mode.String(), err)
		}
	}
}

func TestHandleGetBreakingChangesHappyPathDefaultSince(t *testing.T) {
	calledWithSince := int64(-1)
	wrap := &spyBreakingChangesEngine{fakeCaronteEngine: &fakeCaronteEngine{}, sinceCapture: &calledWithSince}
	p := NewCaronteProxy(wrap, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_breaking_changes"),
		Args:      map[string]any{"workspace": "ws-1"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool: %v", err)
	}
	if calledWithSince != 0 {
		t.Errorf("engine.GetBreakingChanges since = %d; want 0 (default for absent)", calledWithSince)
	}
	if !strings.Contains(resp.Content[0].Text, `"change_id":"chg-1"`) {
		t.Errorf("body %q missing change_id from fake", resp.Content[0].Text)
	}
}

func TestHandleGetBreakingChangesSinceParsed(t *testing.T) {
	calledWithSince := int64(-1)
	wrap := &spyBreakingChangesEngine{fakeCaronteEngine: &fakeCaronteEngine{}, sinceCapture: &calledWithSince}
	p := NewCaronteProxy(wrap, NopAuditEmitter())
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_breaking_changes"),
		Args:      map[string]any{"workspace": "ws-1", "since": float64(1700000000)},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool: %v", err)
	}
	if calledWithSince != 1700000000 {
		t.Errorf("engine.GetBreakingChanges since = %d; want 1700000000", calledWithSince)
	}
}

type spyBreakingChangesEngine struct {
	*fakeCaronteEngine
	sinceCapture *int64
}

func (s *spyBreakingChangesEngine) GetBreakingChanges(ctx context.Context, wsID string, since int64) ([]caronte.BreakingChangePayload, error) {
	if s.sinceCapture != nil {
		*s.sinceCapture = since
	}
	return s.fakeCaronteEngine.GetBreakingChanges(ctx, wsID, since)
}
