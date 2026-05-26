package workforceadapter

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/workforce/worker"
)

func minimalResolved(name string) doctrine.Resolved {
	return doctrine.Resolved{
		Schema: doctrine.Schema{
			SchemaVersion: 1,
			Name:          name,
		},
		Provenance: map[string]string{"name": "builtin:" + name},
	}
}

func TestNewDoctrineConfigAdapterRejectsEmpty(t *testing.T) {
	_, err := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: doctrine.Resolved{},
	})
	if err == nil {
		t.Fatal("expected error for empty Resolved")
	}
}

func TestNewDoctrineConfigAdapterAccepts(t *testing.T) {
	adapter, err := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: minimalResolved("max-scope"),
		RepoRoot: "/tmp/repo",
	})
	if err != nil {
		t.Fatalf("NewDoctrineConfigAdapter: %v", err)
	}
	if adapter == nil {
		t.Fatal("nil adapter")
	}
}

func TestDoctrineConfigAdapterImplementsInterface(t *testing.T) {
	adapter, _ := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: minimalResolved("max-scope"),
	})
	var _ worker.DoctrineConfig = adapter
}

func TestReinforcementTemplateReturnsMarkerWhenPointerEmpty(t *testing.T) {
	adapter, _ := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: minimalResolved("max-scope"),
		RepoRoot: "/tmp/repo",
	})
	got := adapter.ReinforcementTemplate("max-scope")
	if !strings.Contains(got, "[doctrine: max-scope]") {
		t.Errorf("got %q, want substring '[doctrine: max-scope]'", got)
	}
}

func TestReinforcementTemplateReturnsMarkerWhenRepoRootEmpty(t *testing.T) {
	resolved := minimalResolved("max-scope")
	resolved.Schema.Workforce.DoctrineReinforcementTemplatePointer = "templates/max-scope.md"
	adapter, _ := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: resolved,
		RepoRoot: "",
	})
	got := adapter.ReinforcementTemplate("max-scope")
	if !strings.Contains(got, "[doctrine: max-scope]") {
		t.Errorf("got %q, want fallback marker", got)
	}
}

func TestReinforcementTemplateReadsRelativePointer(t *testing.T) {
	resolved := minimalResolved("max-scope")
	resolved.Schema.Workforce.DoctrineReinforcementTemplatePointer = "templates/max-scope.md"
	calls := []string{}
	adapter, _ := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: resolved,
		RepoRoot: "/tmp/repo",
		ReadFile: func(path string) ([]byte, error) {
			calls = append(calls, path)
			return []byte("MAX SCOPE FULL TEMPLATE BODY"), nil
		},
	})
	got := adapter.ReinforcementTemplate("max-scope")
	if got != "MAX SCOPE FULL TEMPLATE BODY" {
		t.Errorf("got %q, want template body", got)
	}
	if len(calls) != 1 || calls[0] != "/tmp/repo/templates/max-scope.md" {
		t.Errorf("readFile calls = %v, want one call to /tmp/repo/templates/max-scope.md", calls)
	}
}

func TestReinforcementTemplateReadsAbsolutePointer(t *testing.T) {
	resolved := minimalResolved("max-scope")
	resolved.Schema.Workforce.DoctrineReinforcementTemplatePointer = "/absolute/path.md"
	calls := []string{}
	adapter, _ := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: resolved,
		RepoRoot: "/tmp/repo",
		ReadFile: func(path string) ([]byte, error) {
			calls = append(calls, path)
			return []byte("ABS BODY"), nil
		},
	})
	got := adapter.ReinforcementTemplate("max-scope")
	if got != "ABS BODY" {
		t.Errorf("got %q, want abs body", got)
	}
	if len(calls) != 1 || calls[0] != "/absolute/path.md" {
		t.Errorf("readFile calls = %v; absolute path must be used as-is", calls)
	}
}

func TestReinforcementTemplateReadFileErrorFallsBackToMarker(t *testing.T) {
	resolved := minimalResolved("max-scope")
	resolved.Schema.Workforce.DoctrineReinforcementTemplatePointer = "templates/x.md"
	adapter, _ := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: resolved,
		RepoRoot: "/tmp/repo",
		ReadFile: func(path string) ([]byte, error) {
			return nil, errors.New("file not found")
		},
	})
	got := adapter.ReinforcementTemplate("max-scope")
	if !strings.Contains(got, "[doctrine: max-scope]") {
		t.Errorf("got %q, want fallback marker", got)
	}
}

func TestReinforcementTemplateEmptyFileFallsBackToMarker(t *testing.T) {
	resolved := minimalResolved("max-scope")
	resolved.Schema.Workforce.DoctrineReinforcementTemplatePointer = "templates/empty.md"
	adapter, _ := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: resolved,
		RepoRoot: "/tmp/repo",
		ReadFile: func(path string) ([]byte, error) {
			return []byte{}, nil
		},
	})
	got := adapter.ReinforcementTemplate("max-scope")
	if !strings.Contains(got, "[doctrine: max-scope]") {
		t.Errorf("got %q, want fallback marker on empty file", got)
	}
}

func TestCheckpointDeadlineFallbackOnZeroTTL(t *testing.T) {
	adapter, _ := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: minimalResolved("max-scope"),
	})
	got := adapter.CheckpointDeadline("max-scope")
	if got != worker.DefaultCheckpointDeadline {
		t.Errorf("got %v, want DefaultCheckpointDeadline (%v)", got, worker.DefaultCheckpointDeadline)
	}
}

func TestCheckpointDeadlineCapsAtTacticalMax(t *testing.T) {
	resolved := minimalResolved("max-scope")

	resolved.Schema.Subprocess.PersistentTTLSliding = doctrine.Duration(8 * time.Hour)
	adapter, _ := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: resolved,
	})
	got := adapter.CheckpointDeadline("max-scope")
	want := 5 * time.Minute
	if got != want {
		t.Errorf("got %v, want capped %v", got, want)
	}
}

func TestCheckpointDeadlineUsesTTLDirectlyWhenWithinBound(t *testing.T) {
	resolved := minimalResolved("max-scope")
	resolved.Schema.Subprocess.PersistentTTLSliding = doctrine.Duration(2 * time.Minute)
	adapter, _ := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: resolved,
	})
	got := adapter.CheckpointDeadline("max-scope")
	want := 2 * time.Minute
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCheckpointDeadlineFallbackOnNegativeTTL(t *testing.T) {
	resolved := minimalResolved("max-scope")
	resolved.Schema.Subprocess.PersistentTTLSliding = doctrine.Duration(-10 * time.Second)
	adapter, _ := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: resolved,
	})
	got := adapter.CheckpointDeadline("max-scope")
	if got != worker.DefaultCheckpointDeadline {
		t.Errorf("got %v, want DefaultCheckpointDeadline (%v)", got, worker.DefaultCheckpointDeadline)
	}
}

func TestNewDoctrineConfigAdapterDefaultsReadFile(t *testing.T) {

	adapter, err := NewDoctrineConfigAdapter(DoctrineConfigAdapterOptions{
		Resolved: minimalResolved("max-scope"),
		RepoRoot: "/tmp/repo",
		ReadFile: nil,
	})
	if err != nil {
		t.Fatalf("NewDoctrineConfigAdapter: %v", err)
	}
	if adapter.readFile == nil {
		t.Error("readFile must default to non-nil (os.ReadFile)")
	}
}
