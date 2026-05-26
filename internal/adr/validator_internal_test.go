package adr

import (
	"errors"
	"testing"
)

// TestCheckIDUniquenessSkipsEmptyID verifies that ADRs with an empty ID field
// (legacy format) are skipped by checkIDUniqueness and do not trigger a
// collision even when two such ADRs appear in the same corpus.
// This branch is architecturally unreachable from ValidateAll (schema check
// fires first and rejects empty IDs), but the guard is load-bearing for any
// future caller that invokes checkIDUniqueness directly.
func TestCheckIDUniquenessSkipsEmptyID(t *testing.T) {
	adrs := []*ADR{
		{Frontmatter: Frontmatter{ID: ""}, Path: "docs/decisions/legacy-a.md"},
		{Frontmatter: Frontmatter{ID: ""}, Path: "docs/decisions/legacy-b.md"},
	}
	if err := checkIDUniqueness(adrs); err != nil {
		t.Errorf("checkIDUniqueness() = %v; want nil for two empty-ID ADRs", err)
	}
}

func TestCheckIDUniquenessDetectsCollisionDirect(t *testing.T) {
	adrs := []*ADR{
		{Frontmatter: Frontmatter{ID: "ADR-0001"}, Path: "docs/decisions/a.md"},
		{Frontmatter: Frontmatter{ID: "ADR-0001"}, Path: "docs/decisions/b.md"},
	}
	err := checkIDUniqueness(adrs)
	if err == nil {
		t.Fatal("checkIDUniqueness() = nil; want ErrIDCollision")
	}
	if !errors.Is(err, ErrIDCollision) {
		t.Errorf("error = %v; want errors.Is(..., ErrIDCollision)", err)
	}
}

func TestDetectSupersedeCycleSelfLoop(t *testing.T) {
	adrs := []*ADR{
		{
			Frontmatter: Frontmatter{
				ID:           "ADR-0001",
				SupersededBy: "ADR-0001",
			},
			Path: "docs/decisions/self-loop.md",
		},
	}
	err := detectSupersedeCycle(adrs)
	if err == nil {
		t.Fatal("detectSupersedeCycle() = nil; want ErrSupersedeCycle for self-loop")
	}
	if !errors.Is(err, ErrSupersedeCycle) {
		t.Errorf("error = %v; want errors.Is(..., ErrSupersedeCycle)", err)
	}
}

func TestDetectSupersedeCycleSkipsEmptyIDInOuterLoop(t *testing.T) {
	adrs := []*ADR{
		{Frontmatter: Frontmatter{ID: ""}, Path: "docs/decisions/legacy.md"},
		{
			Frontmatter: Frontmatter{ID: "ADR-0010", SupersededBy: ""},
			Path:        "docs/decisions/0010.md",
		},
	}
	if err := detectSupersedeCycle(adrs); err != nil {
		t.Errorf("detectSupersedeCycle() = %v; want nil (empty-ID ADR skipped, no cycle)", err)
	}
}

func TestDetectSupersedeCycleMultipleCycles(t *testing.T) {

	adrs := []*ADR{
		{Frontmatter: Frontmatter{ID: "ADR-0020", SupersededBy: "ADR-0021"}, Path: "docs/decisions/0020.md"},
		{Frontmatter: Frontmatter{ID: "ADR-0021", SupersededBy: "ADR-0020"}, Path: "docs/decisions/0021.md"},
		{Frontmatter: Frontmatter{ID: "ADR-0030", SupersededBy: "ADR-0031"}, Path: "docs/decisions/0030.md"},
		{Frontmatter: Frontmatter{ID: "ADR-0031", SupersededBy: "ADR-0030"}, Path: "docs/decisions/0031.md"},
	}
	err := detectSupersedeCycle(adrs)
	if err == nil {
		t.Fatal("detectSupersedeCycle() = nil; want ErrSupersedeCycle for two independent cycles")
	}
	if !errors.Is(err, ErrSupersedeCycle) {
		t.Errorf("error = %v; want errors.Is(..., ErrSupersedeCycle)", err)
	}
}
