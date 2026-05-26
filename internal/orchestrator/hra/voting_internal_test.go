package hra

import "testing"

func TestMajorityThreshold_Boundaries(t *testing.T) {
	t.Parallel()

	want := map[int]int{
		1:  1,
		2:  2,
		3:  2,
		4:  3,
		5:  3,
		6:  4,
		7:  4,
		8:  5,
		9:  5,
		10: 6,
	}
	for n, exp := range want {
		got := majorityThreshold(n)
		if got != exp {
			t.Errorf("majorityThreshold(%d)=%d, want %d", n, got, exp)
		}
	}
}
