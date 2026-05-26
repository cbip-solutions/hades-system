package mcpgateway_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
)

func nopHandler(_ context.Context, _ mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
	return mcpgateway.CallResponse{}, nil
}

func TestToolRegistryRegisterAndLookup(t *testing.T) {
	r := mcpgateway.NewToolRegistry()
	tn := mcpgateway.MustToolName("budget", "cap_status")
	meta := mcpgateway.ToolMeta{
		Description: "Return current cost cap status",
		InputSchema: map[string]any{"type": "object"},
	}
	if err := r.Register(tn, nopHandler, meta); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Lookup(tn)
	if !ok {
		t.Fatalf("Lookup: not found")
	}
	if got.Meta.Description != meta.Description {
		t.Errorf("desc = %q want %q", got.Meta.Description, meta.Description)
	}
}

func TestToolRegistryDedupRejectsCollision(t *testing.T) {

	r := mcpgateway.NewToolRegistry()
	tn := mcpgateway.MustToolName("budget", "cap_status")
	if err := r.Register(tn, nopHandler, mcpgateway.ToolMeta{}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register(tn, nopHandler, mcpgateway.ToolMeta{})
	if err == nil {
		t.Fatal("second Register: nil error; expected ErrToolNameCollision")
	}
	if !errors.Is(err, mcpgateway.ErrToolNameCollision) {
		t.Errorf("err = %v; expected wrap of ErrToolNameCollision", err)
	}
}

func TestToolRegistryListSorted(t *testing.T) {
	r := mcpgateway.NewToolRegistry()
	tools := []mcpgateway.ToolName{
		mcpgateway.MustToolName("research", "agentic"),
		mcpgateway.MustToolName("budget", "cap_status"),
		mcpgateway.MustToolName("audit", "emit"),
	}
	for _, tn := range tools {
		if err := r.Register(tn, nopHandler, mcpgateway.ToolMeta{}); err != nil {
			t.Fatalf("Register %v: %v", tn, err)
		}
	}
	got := r.List()
	if len(got) != len(tools) {
		t.Fatalf("List len = %d, want %d", len(got), len(tools))
	}

	names := make([]string, len(got))
	for i, e := range got {
		names[i] = e.Name.String()
	}
	sorted := make([]string, len(names))
	copy(sorted, names)
	sort.Strings(sorted)
	for i := range names {
		if names[i] != sorted[i] {
			t.Errorf("List[%d] = %q; expected sorted %q", i, names[i], sorted[i])
		}
	}
}

func TestToolRegistryHas(t *testing.T) {
	r := mcpgateway.NewToolRegistry()
	tn := mcpgateway.MustToolName("audit", "emit")
	if r.Has(tn) {
		t.Fatal("Has on empty registry returned true")
	}
	if err := r.Register(tn, nopHandler, mcpgateway.ToolMeta{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !r.Has(tn) {
		t.Error("Has after Register returned false")
	}
}

func TestToolRegistryLookupMiss(t *testing.T) {
	r := mcpgateway.NewToolRegistry()
	tn := mcpgateway.MustToolName("audit", "emit")
	if _, ok := r.Lookup(tn); ok {
		t.Error("Lookup miss returned ok=true")
	}
}

func TestToolRegistryRejectsNilHandler(t *testing.T) {
	r := mcpgateway.NewToolRegistry()
	tn := mcpgateway.MustToolName("audit", "emit")
	err := r.Register(tn, nil, mcpgateway.ToolMeta{})
	if err == nil {
		t.Fatal("Register nil handler returned nil err; expected error")
	}
}

func TestToolRegistryRejectsZeroToolName(t *testing.T) {
	r := mcpgateway.NewToolRegistry()
	var zero mcpgateway.ToolName
	err := r.Register(zero, nopHandler, mcpgateway.ToolMeta{})
	if err == nil {
		t.Fatal("Register zero ToolName returned nil err")
	}
	if !errors.Is(err, mcpgateway.ErrToolNameInvalid) {
		t.Errorf("err = %v; expected ErrToolNameInvalid", err)
	}
}

func TestToolRegistryConcurrentRegisterDedups(t *testing.T) {

	r := mcpgateway.NewToolRegistry()
	tn := mcpgateway.MustToolName("research", "agentic")
	var wg sync.WaitGroup
	var successes atomic.Int32
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			if err := r.Register(tn, nopHandler, mcpgateway.ToolMeta{}); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := successes.Load(); got != 1 {
		t.Errorf("successes = %d, want exactly 1", got)
	}
}

func TestToolRegistrySnapshot(t *testing.T) {

	r := mcpgateway.NewToolRegistry()
	tn1 := mcpgateway.MustToolName("audit", "emit")
	if err := r.Register(tn1, nopHandler, mcpgateway.ToolMeta{}); err != nil {
		t.Fatalf("Register tn1: %v", err)
	}
	snap := r.List()
	if len(snap) != 1 {
		t.Fatalf("snap len = %d", len(snap))
	}
	tn2 := mcpgateway.MustToolName("audit", "emit2")
	if err := r.Register(tn2, nopHandler, mcpgateway.ToolMeta{}); err != nil {
		t.Fatalf("Register tn2: %v", err)
	}
	if len(snap) != 1 {
		t.Errorf("snapshot mutated by subsequent Register: len = %d", len(snap))
	}
}

func TestToolRegistryLen(t *testing.T) {
	r := mcpgateway.NewToolRegistry()
	if r.Len() != 0 {
		t.Errorf("empty Len = %d, want 0", r.Len())
	}
	tn := mcpgateway.MustToolName("audit", "emit")
	if err := r.Register(tn, nopHandler, mcpgateway.ToolMeta{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if r.Len() != 1 {
		t.Errorf("Len = %d, want 1", r.Len())
	}
}

func TestToolRegistryListDeepCopiesInputSchema(t *testing.T) {
	r := mcpgateway.NewToolRegistry()
	tn := mcpgateway.MustToolName("research", "web_search")
	originalSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"q": map[string]any{"type": "string"},
		},
		"required": []any{"q"},
	}
	if err := r.Register(tn, nopHandler, mcpgateway.ToolMeta{
		Description: "search",
		InputSchema: originalSchema,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Mutate via the caller's slice; the registry MUST be unaffected.
	out1 := r.List()
	if len(out1) != 1 {
		t.Fatalf("List len = %d, want 1", len(out1))
	}
	out1[0].Meta.InputSchema["injected"] = "tampered"

	out2 := r.List()
	if _, leaked := out2[0].Meta.InputSchema["injected"]; leaked {
		t.Errorf("mutation leaked into registry cache; subsequent List sees 'injected' key")
	}

	if _, leaked := originalSchema["injected"]; leaked {
		t.Errorf("mutation leaked back into caller's source map; deep-copy must be bidirectional")
	}
}

func TestToolRegistryListDeepCopiesNestedSlice(t *testing.T) {
	r := mcpgateway.NewToolRegistry()
	tn := mcpgateway.MustToolName("caronte", "query")
	if err := r.Register(tn, nopHandler, mcpgateway.ToolMeta{
		InputSchema: map[string]any{
			"required": []any{"q"},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	out := r.List()

	required, ok := out[0].Meta.InputSchema["required"].([]any)
	if !ok {
		t.Fatalf("required type = %T; want []any", out[0].Meta.InputSchema["required"])
	}
	required = append(required, "tampered")
	out[0].Meta.InputSchema["required"] = required

	out2 := r.List()
	required2 := out2[0].Meta.InputSchema["required"].([]any)
	if len(required2) != 1 {
		t.Errorf("nested slice leaked mutation: List required = %v, want [\"q\"]", required2)
	}
}

func TestToolRegistryListDeepCopiesNilSlice(t *testing.T) {
	r := mcpgateway.NewToolRegistry()
	tn := mcpgateway.MustToolName("audit", "emit")
	if err := r.Register(tn, nopHandler, mcpgateway.ToolMeta{
		InputSchema: map[string]any{
			"type":     "object",
			"required": ([]any)(nil),
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	out := r.List()
	if len(out) != 1 {
		t.Fatalf("List len = %d, want 1", len(out))
	}
	got, ok := out[0].Meta.InputSchema["required"]
	if !ok {
		t.Fatalf("required key missing")
	}

	if got != nil {

		if s, isSlice := got.([]any); isSlice && s != nil {
			t.Errorf("nil slice corrupted: got %v", s)
		}
	}
}

func TestToolRegistryLookupDeepCopiesInputSchema(t *testing.T) {
	r := mcpgateway.NewToolRegistry()
	tn := mcpgateway.MustToolName("audit", "emit")
	if err := r.Register(tn, nopHandler, mcpgateway.ToolMeta{
		InputSchema: map[string]any{"type": "object"},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	e1, ok := r.Lookup(tn)
	if !ok {
		t.Fatal("Lookup ok=false")
	}
	e1.Meta.InputSchema["injected"] = "tampered"

	e2, _ := r.Lookup(tn)
	if _, leaked := e2.Meta.InputSchema["injected"]; leaked {
		t.Errorf("Lookup mutation leaked into registry cache")
	}
}
