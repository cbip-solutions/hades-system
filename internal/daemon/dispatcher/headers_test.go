package dispatcher_test

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcher"
)

func TestInjectHeadersFromContext(t *testing.T) {
	ctx := dispatcher.WithProject(context.Background(), "test-project")
	ctx = dispatcher.WithSession(ctx, "sess-123")
	ctx = dispatcher.WithProfile(ctx, "audit-reviewer")

	headers := dispatcher.HeadersFromContext(ctx)
	if got := headers["X-Zen-Project"]; got != "test-project" {
		t.Errorf("X-Zen-Project = %q, want test-project", got)
	}
	if got := headers["X-Zen-Session"]; got != "sess-123" {
		t.Errorf("X-Zen-Session = %q, want sess-123", got)
	}
	if got := headers["X-Zen-Profile"]; got != "audit-reviewer" {
		t.Errorf("X-Zen-Profile = %q, want audit-reviewer", got)
	}
}

func TestMergeHeadersPreservesContextValues(t *testing.T) {
	ctx := dispatcher.WithProfile(context.Background(), "swarm-coder")
	existing := map[string]string{
		"X-Zen-Project": "explicit-project",
		"User-Agent":    "zen-swarm/0.3",
	}
	merged := dispatcher.MergeHeaders(ctx, existing)
	if merged["X-Zen-Project"] != "explicit-project" {
		t.Error("explicit X-Zen-Project should win over context")
	}
	if merged["X-Zen-Profile"] != "swarm-coder" {
		t.Error("X-Zen-Profile from context should propagate")
	}
	if merged["User-Agent"] != "zen-swarm/0.3" {
		t.Error("non-Zen headers should be preserved")
	}
}

func TestHeadersFromContextEmptyValuesOmitted(t *testing.T) {
	ctx := dispatcher.WithProject(context.Background(), "")
	ctx = dispatcher.WithSession(ctx, "")
	ctx = dispatcher.WithProfile(ctx, "")

	headers := dispatcher.HeadersFromContext(ctx)
	if _, ok := headers["X-Zen-Project"]; ok {
		t.Error("empty X-Zen-Project should be omitted from output map")
	}
	if _, ok := headers["X-Zen-Session"]; ok {
		t.Error("empty X-Zen-Session should be omitted from output map")
	}
	if _, ok := headers["X-Zen-Profile"]; ok {
		t.Error("empty X-Zen-Profile should be omitted from output map")
	}
}

func TestHeadersFromContextNoValuesReturnsEmpty(t *testing.T) {
	headers := dispatcher.HeadersFromContext(context.Background())
	if len(headers) != 0 {
		t.Errorf("expected empty map, got %v", headers)
	}
}

func TestMergeHeadersNilExplicit(t *testing.T) {
	ctx := dispatcher.WithProject(context.Background(), "ctx-project")
	merged := dispatcher.MergeHeaders(ctx, nil)
	if merged["X-Zen-Project"] != "ctx-project" {
		t.Errorf("X-Zen-Project = %q, want ctx-project", merged["X-Zen-Project"])
	}
}

func TestMergeHeadersEmptyExplicitValueWins(t *testing.T) {
	ctx := dispatcher.WithProject(context.Background(), "ctx-project")
	explicit := map[string]string{
		"X-Zen-Project": "",
	}
	merged := dispatcher.MergeHeaders(ctx, explicit)

	v, ok := merged["X-Zen-Project"]
	if !ok {
		t.Error("X-Zen-Project key should be present when explicit map contains it (even empty)")
	}
	if v != "" {
		t.Errorf("X-Zen-Project = %q, want empty string (explicit wins)", v)
	}
}

func TestHeadersFromContextPerKeyIsolation(t *testing.T) {
	ctx := dispatcher.WithProject(context.Background(), "proj-A")
	ctx = dispatcher.WithProfile(ctx, "profile-B")

	headers := dispatcher.HeadersFromContext(ctx)
	if headers["X-Zen-Project"] != "proj-A" {
		t.Errorf("X-Zen-Project = %q, want proj-A", headers["X-Zen-Project"])
	}
	if headers["X-Zen-Profile"] != "profile-B" {
		t.Errorf("X-Zen-Profile = %q, want profile-B", headers["X-Zen-Profile"])
	}
	if _, ok := headers["X-Zen-Session"]; ok {
		t.Error("X-Zen-Session should be absent when WithSession was not called")
	}
}

func TestHeadersFromContextNonStringValueSafe(t *testing.T) {

	ctx := dispatcher.WithProject(context.Background(), "safe")
	ctx = dispatcher.WithSession(ctx, "safe-sess")
	ctx = dispatcher.WithProfile(ctx, "safe-profile")

	type foreignKey struct{}
	ctx = context.WithValue(ctx, foreignKey{}, 42)

	headers := dispatcher.HeadersFromContext(ctx)
	if headers["X-Zen-Project"] != "safe" {
		t.Errorf("X-Zen-Project = %q after foreign-key injection, want safe", headers["X-Zen-Project"])
	}
	if headers["X-Zen-Session"] != "safe-sess" {
		t.Errorf("X-Zen-Session = %q, want safe-sess", headers["X-Zen-Session"])
	}
	if headers["X-Zen-Profile"] != "safe-profile" {
		t.Errorf("X-Zen-Profile = %q, want safe-profile", headers["X-Zen-Profile"])
	}
}

func TestMergeHeadersDoesNotMutateExplicit(t *testing.T) {
	ctx := dispatcher.WithProfile(context.Background(), "ctx-profile")
	explicit := map[string]string{"X-Zen-Project": "explicit-proj"}
	snapshot := map[string]string{"X-Zen-Project": "explicit-proj"}

	_ = dispatcher.MergeHeaders(ctx, explicit)

	if len(explicit) != len(snapshot) {
		t.Errorf("explicit map size mutated: got %d, want %d", len(explicit), len(snapshot))
	}
	for k, want := range snapshot {
		if got := explicit[k]; got != want {
			t.Errorf("explicit[%q] mutated: got %q, want %q", k, got, want)
		}
	}
	if _, leaked := explicit["X-Zen-Profile"]; leaked {
		t.Error("ctx-derived X-Zen-Profile leaked into explicit map")
	}
}

func TestHeadersFromContextReturnsFreshMap(t *testing.T) {
	ctx := dispatcher.WithProject(context.Background(), "p")
	a := dispatcher.HeadersFromContext(ctx)
	a["X-Zen-Project"] = "mutated"
	a["X-Injected"] = "garbage"

	b := dispatcher.HeadersFromContext(ctx)
	if b["X-Zen-Project"] != "p" {
		t.Errorf("second call leaked first-call mutation: got %q, want p", b["X-Zen-Project"])
	}
	if _, ok := b["X-Injected"]; ok {
		t.Error("second call leaked first-call injected key")
	}
}

func TestWithFunctionsInnermostWins(t *testing.T) {
	ctx := dispatcher.WithProject(context.Background(), "outer")
	ctx = dispatcher.WithProject(ctx, "inner")
	ctx = dispatcher.WithSession(ctx, "s-outer")
	ctx = dispatcher.WithSession(ctx, "s-inner")

	headers := dispatcher.HeadersFromContext(ctx)
	if got := headers["X-Zen-Project"]; got != "inner" {
		t.Errorf("X-Zen-Project = %q, want inner (innermost wins)", got)
	}
	if got := headers["X-Zen-Session"]; got != "s-inner" {
		t.Errorf("X-Zen-Session = %q, want s-inner (innermost wins)", got)
	}
}
