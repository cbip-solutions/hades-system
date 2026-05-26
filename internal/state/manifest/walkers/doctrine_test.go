package walkers

import (
	"context"
	"strings"
	"testing"
)

func TestDoctrineWalker_StaticInjection(t *testing.T) {
	w := NewDoctrineWalker(func() []string {
		return []string{"max-scope", "default", "capa-firewall"}
	})
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := strings.Join(r.Declared, ",")
	if got != "capa-firewall,default,max-scope" {
		t.Errorf("Declared (sorted): got %q", got)
	}
}

func TestDoctrineWalker_NilProvider_ReportsMissing(t *testing.T) {
	w := NewDoctrineWalker(nil)
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !contains(r.MissingSources, "doctrine-registry") {
		t.Errorf("MissingSources: got %v", r.MissingSources)
	}
}

func TestDoctrineWalker_EmptySlice_ReportsMissing(t *testing.T) {
	w := NewDoctrineWalker(func() []string { return nil })
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !contains(r.MissingSources, "doctrine-registry") {
		t.Errorf("MissingSources on nil return: got %v", r.MissingSources)
	}
}

func TestDoctrineWalker_ReturnsEmpty_NotMissing(t *testing.T) {
	w := NewDoctrineWalker(func() []string { return []string{"max-scope"} })
	r, err := w.Walk(context.Background())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(r.MissingSources) != 0 {
		t.Errorf("MissingSources: got %v, want empty", r.MissingSources)
	}
	if len(r.Declared) != 1 || r.Declared[0] != "max-scope" {
		t.Errorf("Declared: got %v", r.Declared)
	}
}
