package compliance

import (
	"math"
	"math/rand"
	"sort"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/budget"
)

func goldenWindow() []float64 {
	r := rand.New(rand.NewSource(12345))
	out := make([]float64, 200)
	for i := range out {
		out[i] = 1.0 + r.NormFloat64()*0.1
	}
	return out
}

func TestInvZen078_GoldenDatasetReproducibility(t *testing.T) {
	w := goldenWindow()
	cases := []struct {
		name   string
		sample float64
	}{
		{"in-distribution", 1.0},
		{"edge", 1.4},
		{"far-out", 5.0},
		{"negative-far-out", -5.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			z1, err := budget.ComputeZScore(w, c.sample)
			if err != nil {
				t.Fatalf("ComputeZScore: %v", err)
			}
			z2, _ := budget.ComputeZScore(w, c.sample)
			if z1 != z2 {
				t.Errorf("z1 = %.20f, z2 = %.20f (must be bit-equal)", z1, z2)
			}
		})
	}
}

func TestInvZen078_PermutationInvariance(t *testing.T) {
	w := goldenWindow()
	z1, _ := budget.ComputeZScore(w, 5.0)

	rev := append([]float64{}, w...)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	z2, _ := budget.ComputeZScore(rev, 5.0)

	sortedAsc := append([]float64{}, w...)
	sort.Float64s(sortedAsc)
	z3, _ := budget.ComputeZScore(sortedAsc, 5.0)

	sortedDesc := append([]float64{}, sortedAsc...)
	for i, j := 0, len(sortedDesc)-1; i < j; i, j = i+1, j-1 {
		sortedDesc[i], sortedDesc[j] = sortedDesc[j], sortedDesc[i]
	}
	z4, _ := budget.ComputeZScore(sortedDesc, 5.0)

	const eps = 1e-9
	if math.Abs(z1-z2) > eps || math.Abs(z1-z3) > eps || math.Abs(z1-z4) > eps {
		t.Errorf("permutations diverge: %.15f / %.15f / %.15f / %.15f", z1, z2, z3, z4)
	}
}

func TestInvZen078_ZScoreSignsCorrect(t *testing.T) {
	w := goldenWindow()
	above, _ := budget.ComputeZScore(w, 5.0)
	below, _ := budget.ComputeZScore(w, -5.0)
	if above <= 0 {
		t.Errorf("above-mean sample z = %f, want > 0", above)
	}
	if below >= 0 {
		t.Errorf("below-mean sample z = %f, want < 0", below)
	}
}

func TestInvZen078_LargeWindowStableAcrossPermutations(t *testing.T) {

	r := rand.New(rand.NewSource(99))
	w := make([]float64, 5000)
	for i := range w {
		w[i] = 10.0 + r.NormFloat64()*1.0
	}
	z1, _ := budget.ComputeZScore(w, 50.0)

	for shuffle := 0; shuffle < 5; shuffle++ {
		shuffled := append([]float64{}, w...)
		r.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		zs, _ := budget.ComputeZScore(shuffled, 50.0)
		if math.Abs(zs-z1) > 1e-9 {
			t.Errorf("shuffle %d diverges: %.15f vs %.15f", shuffle, zs, z1)
		}
	}
}
