package chain

import (
	"testing"
	"time"
)

func TestPartitionIDFormat(t *testing.T) {

	got := PartitionID(1700051696)
	want := "2023_11"
	if got != want {
		t.Errorf("PartitionID(2023-11-15) = %q, want %q", got, want)
	}
}

func TestPartitionIDZeroPaddedMonth(t *testing.T) {

	got := PartitionID(1767225600)
	want := "2026_01"
	if got != want {
		t.Errorf("January = %q, want %q (zero-padded)", got, want)
	}
}

func TestPartitionIDUTCDeterministic(t *testing.T) {
	// Boundary case: 2026-04-30T23:00:00 UTC = 1777590000.
	// (Plan-file pre-computed 1777935600 was wrong — that maps to
	// 2026-05-04 23:00 UTC; corrected here via independent date(1)
	// verification — same intent: pin UTC-vs-local-time normalization.)
	// In CET (+01:00) this is 2026-05-01 00:00:00 — but UTC is the
	// canonical reference, so the partition MUST be 2026_04.
	got := PartitionID(1777590000)
	want := "2026_04"
	if got != want {
		t.Errorf("UTC boundary case = %q, want %q", got, want)
	}
}

func TestPartitionIDDifferentMonthsDifferentPartitions(t *testing.T) {
	a := PartitionID(1700051696)
	b := PartitionID(1702729600)
	if a == b {
		t.Errorf("different months produced same partition: %q", a)
	}
}

func TestPartitionIDSameMonthSamePartition(t *testing.T) {
	a := PartitionID(1700051696)
	b := PartitionID(1700310896)
	if a != b {
		t.Errorf("same month different days produced different partitions: %q vs %q", a, b)
	}
}

func TestPartitionIDLexicographicOrder(t *testing.T) {
	// Lexicographic sort of partition strings MUST equal chronological
	// order. This is load-bearing for ListPartitions ordering.
	cases := []int64{
		1672531200,
		1675209600,
		1677628800,
		1704067200,
	}
	var prev string
	for _, ts := range cases {
		p := PartitionID(ts)
		if prev != "" && prev >= p {
			t.Errorf("lexicographic order broken: %q >= %q", prev, p)
		}
		prev = p
	}
}

func TestPartitionIDFromTime(t *testing.T) {

	got := PartitionIDFromTime(time.Date(2023, 11, 15, 12, 34, 56, 0, time.UTC))
	if got != "2023_11" {
		t.Errorf("PartitionIDFromTime = %q, want 2023_11", got)
	}
}

func TestPartitionIDFromTimeNonUTCInputNormalized(t *testing.T) {

	cet := time.FixedZone("CET", 3600)

	t1 := time.Date(2026, 5, 1, 0, 0, 0, 0, cet)
	got := PartitionIDFromTime(t1)
	if got != "2026_04" {
		t.Errorf("non-UTC normalization: got %q, want 2026_04", got)
	}
}
