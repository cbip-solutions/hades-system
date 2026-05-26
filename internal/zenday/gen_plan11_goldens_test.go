package zenday

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratePlan11Goldens(t *testing.T) {
	if os.Getenv("GEN_GOLDENS") == "" {
		t.Skip("set GEN_GOLDENS=1 to regenerate plan11 golden files")
	}

	augGolden := renderAugmentation(&AugmentationSection{
		TotalCostUSD:   1.23,
		TokensConsumed: 8234,
		TokensCeiling:  10000,
		KGQueriesFired: 47,
		CacheHitRate:   0.72,
	})
	writeGolden(t, "plan11_augmentation.golden", augGolden)

	knwGolden := renderKnowledge(&KnowledgeSection{
		FTS5Docs:                    8234,
		FTS5DocsDeltaSinceYesterday: 12,
		AggregatorDBSizeMB:          42,
		PromoteToday:                3,
		CrossProjectQueries:         7,
		LitestreamReplicaLagSec:     5,
	})
	writeGolden(t, "plan11_knowledge.golden", knwGolden)

	notifGolden := renderNotifications(&NotificationsSection{
		RoutesActive:         []string{"email", "slack"},
		PendingAcks:          2,
		CostCap50Alerts:      1,
		CostCap80Alerts:      0,
		CostCap100Alerts:     0,
		CaronteHealthDigests: 3,
		HermesDispatchErrors: 0,
	})
	writeGolden(t, "plan11_notifications.golden", notifGolden)
}

func writeGolden(t *testing.T, name, content string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write golden %s: %v", path, err)
	}
	t.Logf("wrote %s (%d bytes)", path, len(content))
}
