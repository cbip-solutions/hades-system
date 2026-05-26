package views

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestCostViewSkeleton(t *testing.T) {
	v := NewCostView(nil)
	if !strings.Contains(v.View(), "COST") {
		t.Errorf("expected COST header, got: %s", v.View())
	}
}

func TestCostViewSatisfiesTeaModel(t *testing.T) {
	var _ tea.Model = NewCostView(nil)
}

func TestCostViewRefetchNil(t *testing.T) {
	if NewCostView(nil).Refetch() != nil {
		t.Error("expected nil Refetch with nil client")
	}
}

func TestCostViewInit(t *testing.T) {
	if NewCostView(nil).Init() != nil {
		t.Error("expected nil Init for nil client")
	}
	if NewCostView(client.NewWithBaseURL("http://x")).Init() == nil {
		t.Error("expected non-nil Init for non-nil client")
	}
}

func TestCostUpdateDataMsg(t *testing.T) {
	v := NewCostView(nil)
	v.Update(costDataMsg{
		totalUSD:   12.34,
		byTier:     []client.BudgetTierSpend{{Project: "internal-platform-x", Profile: "impl", Tier: "anthropic", SpendUSD: 5.5}},
		augHitRate: 0.74,
		augQueries: 100,
		augBytes:   1024 * 1024,
	})
	out := v.View()
	for _, want := range []string{"$12.34", "internal-platform-x/impl", "anthropic", "74%", "100", "1.0 MB"} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestCostUpdateErrSurfaces(t *testing.T) {
	v := NewCostView(nil)
	v.Update(costDataMsg{err: errors.New("budget down")})
	if !strings.Contains(v.View(), "budget down") {
		t.Errorf("expected err, got: %s", v.View())
	}
}

func TestCostNoSpendShowsHint(t *testing.T) {
	v := NewCostView(nil)
	if !strings.Contains(v.View(), "no spend") {
		t.Errorf("expected (no spend recorded) hint, got: %s", v.View())
	}
}

func TestCostRefetchHTTP(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.BudgetSummaryResp{
			Range: "24h", TotalUSD: 9.99,
			ByTier: []client.BudgetTierSpend{
				{Project: "p", Profile: "prof", Tier: "t", SpendUSD: 1.0},
			},
		})
	})
	mux.HandleFunc("/v1/augment/summary", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.AugmentSummaryResponse{
			CacheHitRate: 0.5, KGQueriesFired: 50, TokensConsumed: 256,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	v := NewCostView(c)
	msg := v.Refetch()()
	m := msg.(costDataMsg)
	if m.err != nil {
		t.Fatalf("err = %v", m.err)
	}
	if m.totalUSD != 9.99 {
		t.Errorf("totalUSD = %v", m.totalUSD)
	}
	if m.augHitRate != 0.5 {
		t.Errorf("augHitRate = %v", m.augHitRate)
	}
}

func TestCostRefetchBudgetError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	v := NewCostView(c)
	msg := v.Refetch()()
	if (msg.(costDataMsg)).err == nil {
		t.Fatal("expected error")
	}
}

func TestCostRefetchAugmentToleratedError(t *testing.T) {

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.BudgetSummaryResp{TotalUSD: 1.0})
	})
	mux.HandleFunc("/v1/augment/summary", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	msg := NewCostView(c).Refetch()()
	m := msg.(costDataMsg)
	if m.err != nil {
		t.Errorf("expected no err when augment 503s, got: %v", m.err)
	}
	if m.augHitRate != 0 {
		t.Errorf("expected augHitRate=0, got: %v", m.augHitRate)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{2048, "2.0 KB"},
		{2 * 1024 * 1024, "2.0 MB"},
		{3 * 1024 * 1024 * 1024, "3.0 GB"},
	}
	for _, tc := range cases {
		if got := humanBytes(tc.n); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestCostUpdateIgnoresOtherMessages(t *testing.T) {
	v := NewCostView(nil)
	updated, _ := v.Update(tea.KeyMsg{})
	if _, ok := updated.(*CostView); !ok {
		t.Error("expected *CostView")
	}
}

func TestCostHADESPrefix(t *testing.T) {
	v := NewCostView(nil)
	if !strings.Contains(v.View(), "HADES") {
		t.Errorf("CostView missing HADES prefix:\n%s", v.View())
	}
}
