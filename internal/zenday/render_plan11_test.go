package zenday

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadPlan11Golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read plan11 golden %s: %v", name, err)
	}
	return string(b)
}

func TestRenderAugmentation_GoldenNoTimestamp(t *testing.T) {
	s := &AugmentationSection{
		TotalCostUSD:   1.23,
		TokensConsumed: 8234,
		TokensCeiling:  10000,
		KGQueriesFired: 47,
		CacheHitRate:   0.72,
	}
	got := renderAugmentation(s)
	want := loadPlan11Golden(t, "plan11_augmentation.golden")
	if got != want {
		t.Errorf("renderAugmentation mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderAugmentation_Nil(t *testing.T) {
	if got := renderAugmentation(nil); got != "" {
		t.Fatalf("renderAugmentation(nil) = %q, want empty string", got)
	}
}

func TestRenderAugmentation_ZeroCeiling(t *testing.T) {
	s := &AugmentationSection{TotalCostUSD: 0, TokensConsumed: 100, TokensCeiling: 0}
	got := renderAugmentation(s)
	if strings.Contains(got, "% of doctrine ceiling") {
		t.Errorf("percentage shown for zero ceiling; got: %s", got)
	}
}

func TestRenderAugmentation_WithTimestamp(t *testing.T) {
	ts := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	s := &AugmentationSection{LastIndexedRFC3339: ts}
	got := renderAugmentation(s)
	if !strings.Contains(got, "last_indexed:") {
		t.Errorf("missing last_indexed line; got: %s", got)
	}
	if !strings.Contains(got, "ago") {
		t.Errorf("missing 'ago' suffix in last_indexed line; got: %s", got)
	}
}

func TestRenderKnowledge_GoldenFull(t *testing.T) {
	s := &KnowledgeSection{
		FTS5Docs:                    8234,
		FTS5DocsDeltaSinceYesterday: 12,
		AggregatorDBSizeMB:          42,
		PromoteToday:                3,
		CrossProjectQueries:         7,
		LitestreamReplicaLagSec:     5,
	}
	got := renderKnowledge(s)
	want := loadPlan11Golden(t, "plan11_knowledge.golden")
	if got != want {
		t.Errorf("renderKnowledge mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderKnowledge_Nil(t *testing.T) {
	if got := renderKnowledge(nil); got != "" {
		t.Fatalf("renderKnowledge(nil) = %q, want empty string", got)
	}
}

func TestRenderKnowledge_NoDelta(t *testing.T) {
	s := &KnowledgeSection{FTS5Docs: 100}
	got := renderKnowledge(s)
	if strings.Contains(got, "since yesterday") {
		t.Errorf("delta shown when FTS5DocsDeltaSinceYesterday == 0; got: %s", got)
	}
}

func TestRenderKnowledge_LagWarn(t *testing.T) {
	s := &KnowledgeSection{LitestreamReplicaLagSec: 65}
	got := renderKnowledge(s)
	if !strings.Contains(got, "WARN >= 60s") {
		t.Errorf("missing WARN >= 60s; got: %s", got)
	}
}

func TestRenderNotifications_GoldenRoutes(t *testing.T) {
	s := &NotificationsSection{
		RoutesActive:         []string{"email", "slack"},
		PendingAcks:          2,
		CostCap50Alerts:      1,
		CostCap80Alerts:      0,
		CostCap100Alerts:     0,
		CaronteHealthDigests: 3,
		HermesDispatchErrors: 0,
	}
	got := renderNotifications(s)
	want := loadPlan11Golden(t, "plan11_notifications.golden")
	if got != want {
		t.Errorf("renderNotifications mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderNotifications_Nil(t *testing.T) {
	if got := renderNotifications(nil); got != "" {
		t.Fatalf("renderNotifications(nil) = %q, want empty string", got)
	}
}

func TestRenderNotifications_NoRoutes(t *testing.T) {
	s := &NotificationsSection{RoutesActive: nil}
	got := renderNotifications(s)
	if !strings.Contains(got, "none configured") {
		t.Errorf("missing 'none configured' for empty routes; got: %s", got)
	}
}

func TestRenderNotifications_PendingAcksHint(t *testing.T) {
	s := &NotificationsSection{PendingAcks: 5}
	got := renderNotifications(s)
	if !strings.Contains(got, "zen inbox") {
		t.Errorf("missing zen inbox hint for PendingAcks>0; got: %s", got)
	}
}

func TestRenderNotifications_HermesDispatchErrorsHint(t *testing.T) {
	s := &NotificationsSection{HermesDispatchErrors: 3}
	got := renderNotifications(s)
	if !strings.Contains(got, "investigate") {
		t.Errorf("missing 'investigate' hint for HermesDispatchErrors>0; got: %s", got)
	}
}

func TestFormatThousands(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{8234, "8,234"},
		{10000, "10,000"},
		{100000, "100,000"},
		{1000000, "1,000,000"},
	}
	for _, tc := range cases {
		got := formatThousands(tc.n)
		if got != tc.want {
			t.Errorf("formatThousands(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestHumanizeDuration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{59 * time.Second, "59s"},
		{time.Minute, "1m"},
		{90 * time.Minute, "1h"},
		{2 * time.Hour, "2h"},
		{23 * time.Hour, "23h"},
		{24 * time.Hour, "1d"},
		{48 * time.Hour, "2d"},

		{-30 * time.Second, "30s"},
	}
	for _, tc := range cases {
		got := humanizeDuration(tc.d)
		if got != tc.want {
			t.Errorf("humanizeDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
