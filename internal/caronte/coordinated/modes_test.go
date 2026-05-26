package coordinated

import (
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestBuildSurfaceMessageAutonomyWithLore(t *testing.T) {
	b := ContractBreakage{
		Change: store.BreakingChange{
			ChangeID:     "ch-42",
			EndpointRepo: "orders-api",
			Kind:         "removed_field",
		},
		AffectedConsumers: []ConsumerRef{
			{Repo: "checkout-web", File: "src/cart.ts", Line: 88},
			{Repo: "billing-svc", File: "billing/api.py", Line: 12},
		},
		LoreAttribution: &LoreAttribution{
			Author:    "alice@example.com",
			CommitSHA: "abc1234",
			ADRRefs:   []string{"ADR-0099"},
		},
	}
	got := buildSurfaceMessage(b, ModeAutonomy, []string{"orders-api", "checkout-web", "billing-svc"})

	mustContain(t, got, "ch-42")
	mustContain(t, got, "AUTONOMY")
	mustContain(t, got, "checkout-web")
	mustContain(t, got, "billing-svc")
	mustContain(t, got, "alice@example.com")
	mustContain(t, got, "abc1234")
	mustContain(t, got, "ADR-0099")
	mustContain(t, got, "removed_field")

	again := buildSurfaceMessage(b, ModeAutonomy, []string{"orders-api", "checkout-web", "billing-svc"})
	if got != again {
		t.Errorf("buildSurfaceMessage non-deterministic; first=%q second=%q", got, again)
	}
}

func TestBuildSurfaceMessageSurfaceNoLoreNoConsumers(t *testing.T) {
	b := ContractBreakage{
		Change: store.BreakingChange{
			ChangeID:     "ch-empty",
			EndpointRepo: "internal-svc",
			Kind:         "schema_drift",
		},
	}
	got := buildSurfaceMessage(b, ModeSurface, nil)
	if got == "" {
		t.Fatalf("buildSurfaceMessage(sparse): want non-empty string")
	}
	mustContain(t, got, "ch-empty")
	mustContain(t, got, "SURFACE")
	mustContain(t, got, "schema_drift")

	mustContain(t, got, "no consumers affected")
	mustContain(t, got, "no lore attribution")
}

func TestBuildSurfaceMessagePoolUnavailableNote(t *testing.T) {
	b := ContractBreakage{
		Change: store.BreakingChange{ChangeID: "ch-deg", EndpointRepo: "orders-api", Kind: "type_change"},
		AffectedConsumers: []ConsumerRef{
			{Repo: "checkout-web", File: "src/cart.ts", Line: 88},
		},
	}
	got := buildSurfaceMessage(b, ModeSurface, nil)
	mustContain(t, got, "ch-deg")
	mustContain(t, got, "SURFACE")

	mustContain(t, got, "consider")
	mustContain(t, got, "checkout-web")
}

func TestBuildSurfaceMessageOrderingDeterministic(t *testing.T) {
	bA := ContractBreakage{
		Change: store.BreakingChange{ChangeID: "ch-1", EndpointRepo: "api", Kind: "removed_field"},
		AffectedConsumers: []ConsumerRef{
			{Repo: "z-repo", File: "z.go", Line: 1},
			{Repo: "a-repo", File: "a.go", Line: 1},
		},
	}
	bB := ContractBreakage{
		Change: store.BreakingChange{ChangeID: "ch-1", EndpointRepo: "api", Kind: "removed_field"},
		AffectedConsumers: []ConsumerRef{
			{Repo: "a-repo", File: "a.go", Line: 1},
			{Repo: "z-repo", File: "z.go", Line: 1},
		},
	}
	if buildSurfaceMessage(bA, ModeSurface, nil) != buildSurfaceMessage(bB, ModeSurface, nil) {
		t.Errorf("surface message ordering NOT deterministic across consumer-slice permutations")
	}
}

func TestBuildSurfaceMessageUnknownModeFallsBackToSurface(t *testing.T) {
	b := ContractBreakage{
		Change: store.BreakingChange{ChangeID: "ch-x", EndpointRepo: "api", Kind: "added_required"},
	}
	got := buildSurfaceMessage(b, DispatchMode("bogus"), nil)
	mustContain(t, got, "ch-x")
	mustContain(t, got, "SURFACE")
}

func TestBuildSurfaceMessageSupersedesRendered(t *testing.T) {
	b := ContractBreakage{
		Change: store.BreakingChange{ChangeID: "ch-sup", EndpointRepo: "api", Kind: "removed_field"},
		LoreAttribution: &LoreAttribution{
			Author:     "bob@example.com",
			CommitSHA:  "def5678",
			Supersedes: []string{"ADR-0050", "ADR-0051"},
		},
	}
	got := buildSurfaceMessage(b, ModeSurface, nil)
	mustContain(t, got, "supersedes")
	mustContain(t, got, "ADR-0050")
	mustContain(t, got, "ADR-0051")
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("surface message missing %q\nfull message:\n%s", want, got)
	}
}
