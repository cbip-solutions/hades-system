package knowledge

import (
	"math"
	"testing"
	"time"
)

func TestComputeScoreBaseBM25Pass(t *testing.T) {
	now := time.Now()
	got := ComputeScore(RankParams{
		BaseBM25:     1.0,
		LastModified: now,
		Now:          now,
	})

	if got <= 1.0 {
		t.Errorf("score = %f, expected > 1.0 (recency boost active)", got)
	}
}

func TestComputeScoreRecencyDecaysExponentially(t *testing.T) {
	now := time.Now()
	young := ComputeScore(RankParams{
		BaseBM25: 1.0, LastModified: now.Add(-1 * time.Hour), Now: now,
	})
	week := ComputeScore(RankParams{
		BaseBM25: 1.0, LastModified: now.Add(-168 * time.Hour), Now: now,
	})
	month := ComputeScore(RankParams{
		BaseBM25: 1.0, LastModified: now.Add(-720 * time.Hour), Now: now,
	})
	if !(young > week && week > month) {
		t.Errorf("recency monotonic decay broken: young=%f week=%f month=%f", young, week, month)
	}
}

func TestComputeScoreProjectMatchBoost(t *testing.T) {
	now := time.Now()
	without := ComputeScore(RankParams{BaseBM25: 1.0, LastModified: now, Now: now})
	with := ComputeScore(RankParams{BaseBM25: 1.0, LastModified: now, Now: now, ProjectMatchBonus: 1.0})
	if !(with > without) {
		t.Errorf("project-match boost not applied: without=%f with=%f", without, with)
	}
	if math.Abs((with-without)-projectMatchBoostWeight) > 1e-9 {
		t.Errorf("project-match delta = %f, want %f", with-without, projectMatchBoostWeight)
	}
}

func TestComputeScoreOrderingInvariance(t *testing.T) {
	now := time.Now()
	a := ComputeScore(RankParams{BaseBM25: 5.0, LastModified: now, Now: now})
	b := ComputeScore(RankParams{BaseBM25: 1.0, LastModified: now, Now: now})
	if a <= b {
		t.Errorf("higher BM25 did not yield higher score: a=%f b=%f", a, b)
	}
}

func TestComputeScoreFutureModTimeClampedToZeroAge(t *testing.T) {
	now := time.Now()
	got := ComputeScore(RankParams{
		BaseBM25:     1.0,
		LastModified: now.Add(48 * time.Hour),
		Now:          now,
	})
	exact := ComputeScore(RankParams{BaseBM25: 1.0, LastModified: now, Now: now})
	if math.Abs(got-exact) > 1e-9 {
		t.Errorf("future mtime not clamped: got=%f exact=%f", got, exact)
	}
}

func TestLowercaseComputeScoreDelegatesToUppercase(t *testing.T) {
	now := time.Now()
	p := RankParams{
		BaseBM25:          2.5,
		LastModified:      now.Add(-3 * time.Hour),
		Now:               now,
		ProjectMatchBonus: 1.0,
	}
	if computeScore(p) != ComputeScore(p) {
		t.Errorf("lowercase wrapper drift: lc=%f UC=%f", computeScore(p), ComputeScore(p))
	}
}

func TestComputeScoreRecencyBoostShape(t *testing.T) {
	now := time.Now()
	tau := time.Duration(recencyDecayTauHours) * time.Hour
	cases := []struct {
		name      string
		age       time.Duration
		wantBoost float64
	}{
		{"zero age", 0, 1.0},
		{"one tau", tau, math.Exp(-1)},
		{"two tau", 2 * tau, math.Exp(-2)},
		{"five tau", 5 * tau, math.Exp(-5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {

			got := ComputeScore(RankParams{
				BaseBM25:     0,
				LastModified: now.Add(-tc.age),
				Now:          now,
			})
			if math.Abs(got-tc.wantBoost) > 1e-9 {
				t.Errorf("age=%v: recency=%f, want %f (Δ=%g)", tc.age, got, tc.wantBoost, got-tc.wantBoost)
			}
		})
	}
}

func TestComputeScoreDeterministic(t *testing.T) {
	now := time.Now()
	p := RankParams{
		BaseBM25:          3.7,
		LastModified:      now.Add(-42 * time.Hour),
		Now:               now,
		ProjectMatchBonus: 1.0,
	}
	first := ComputeScore(p)
	for i := 0; i < 100; i++ {
		if got := ComputeScore(p); got != first {
			t.Fatalf("non-deterministic: iter=%d got=%f first=%f", i, got, first)
		}
	}
}

func TestComputeScoreNegativeBM25Tolerated(t *testing.T) {
	now := time.Now()
	a := ComputeScore(RankParams{BaseBM25: -5.0, LastModified: now, Now: now})
	b := ComputeScore(RankParams{BaseBM25: -10.0, LastModified: now, Now: now})
	if math.IsNaN(a) || math.IsInf(a, 0) || math.IsNaN(b) || math.IsInf(b, 0) {
		t.Errorf("non-finite scores: a=%f b=%f", a, b)
	}
	if !(a > b) {
		t.Errorf("negative BM25 ordering broken: a=%f b=%f (a should be > b)", a, b)
	}
}
