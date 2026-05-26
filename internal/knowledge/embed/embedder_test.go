package embed

import (
	"math"
	"testing"
)

func TestNormalizeL2(t *testing.T) {
	v := []float32{3, 4, 0, 0}
	out := NormalizeL2(v)

	if math.Abs(float64(out[0])-0.6) > 1e-6 {
		t.Errorf("out[0] = %f; want 0.6", out[0])
	}
	if math.Abs(float64(out[1])-0.8) > 1e-6 {
		t.Errorf("out[1] = %f; want 0.8", out[1])
	}

	mag := math.Sqrt(float64(out[0]*out[0] + out[1]*out[1] + out[2]*out[2] + out[3]*out[3]))
	if math.Abs(mag-1.0) > 1e-6 {
		t.Errorf("magnitude = %f; want 1.0", mag)
	}
}

func TestNormalizeL2ZeroVector(t *testing.T) {
	v := []float32{0, 0, 0, 0}
	out := NormalizeL2(v)

	for _, x := range out {
		if math.IsNaN(float64(x)) {
			t.Errorf("NaN in zero-vec normalization")
		}
	}
}

func TestCosineSimilarity(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	if cs := CosineSimilarity(a, b); math.Abs(float64(cs)) > 1e-6 {
		t.Errorf("orthogonal vec cosine = %f; want 0", cs)
	}
	if cs := CosineSimilarity(a, a); math.Abs(float64(cs)-1.0) > 1e-6 {
		t.Errorf("identical vec cosine = %f; want 1", cs)
	}
}

func TestCosineSimilarityDifferentLengthsReturnsZero(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{1, 0, 0}
	if cs := CosineSimilarity(a, b); cs != 0 {
		t.Errorf("different-length vec cosine = %f; want 0", cs)
	}
}

func TestCosineSimilarityEmptyVecsReturnsZero(t *testing.T) {
	if cs := CosineSimilarity([]float32{}, []float32{}); cs != 0 {
		t.Errorf("empty vec cosine = %f; want 0", cs)
	}
}

func TestCosineSimilarityZeroVecsReturnsZero(t *testing.T) {
	a := []float32{0, 0}
	b := []float32{0, 0}
	cs := CosineSimilarity(a, b)
	if math.IsNaN(float64(cs)) || cs != 0 {
		t.Errorf("zero vec cosine = %f; want 0", cs)
	}
}

func TestNormalizeL2EmptySlice(t *testing.T) {
	out := NormalizeL2([]float32{})
	if len(out) != 0 {
		t.Errorf("empty NormalizeL2 returned len=%d; want 0", len(out))
	}
}
