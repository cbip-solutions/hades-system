package inbox

import (
	"strings"
	"testing"
	"time"
)

func TestBucketSecondsIs300(t *testing.T) {
	if BucketSeconds != 300 {
		t.Errorf("BucketSeconds = %d, want 300 (5min per Q11)", BucketSeconds)
	}
}

func TestDedupBucketAtUnixZero(t *testing.T) {
	got := DedupBucket(time.Unix(0, 0).UTC())
	if got != 0 {
		t.Errorf("DedupBucket(0) = %d, want 0", got)
	}
}

func TestDedupBucketIncrementsEvery5Min(t *testing.T) {
	b0 := DedupBucket(time.Unix(0, 0).UTC())
	b1 := DedupBucket(time.Unix(299, 0).UTC())
	b2 := DedupBucket(time.Unix(300, 0).UTC())
	b3 := DedupBucket(time.Unix(599, 0).UTC())
	b4 := DedupBucket(time.Unix(600, 0).UTC())

	if b0 != b1 {
		t.Errorf("0..299 should be same bucket: b0=%d b1=%d", b0, b1)
	}
	if b1 == b2 {
		t.Errorf("299 and 300 should differ: b1=%d b2=%d", b1, b2)
	}
	if b2 != b3 {
		t.Errorf("300..599 should be same bucket: b2=%d b3=%d", b2, b3)
	}
	if b3 == b4 {
		t.Errorf("599 and 600 should differ: b3=%d b4=%d", b3, b4)
	}
}

func TestComputeDedupKeyShape(t *testing.T) {
	t1 := time.Unix(1714560000, 0).UTC()
	got := ComputeDedupKey("hra.l4_alert", strings.Repeat("a", 64), t1)
	parts := strings.Split(got, "|")
	if len(parts) != 3 {
		t.Fatalf("ComputeDedupKey shape = %q, want a|b|c", got)
	}
	if parts[0] != "hra.l4_alert" {
		t.Errorf("EventType segment = %q, want hra.l4_alert", parts[0])
	}
	if parts[1] != strings.Repeat("a", 64) {
		t.Errorf("ContentHash segment len = %d, want 64", len(parts[1]))
	}
}

func TestComputeDedupKeyStableWithinBucket(t *testing.T) {
	hash := strings.Repeat("a", 64)
	t1 := time.Unix(1714560000, 0).UTC()
	t2 := time.Unix(1714560100, 0).UTC()
	t3 := time.Unix(1714560299, 0).UTC()

	k1 := ComputeDedupKey("x.y", hash, t1)
	k2 := ComputeDedupKey("x.y", hash, t2)
	k3 := ComputeDedupKey("x.y", hash, t3)

	if k1 != k2 || k2 != k3 {
		t.Errorf("keys not equal in bucket: %q / %q / %q", k1, k2, k3)
	}
}

func TestComputeDedupKeyDistinctAcrossBuckets(t *testing.T) {
	hash := strings.Repeat("a", 64)
	t1 := time.Unix(1714560000, 0).UTC()
	t2 := time.Unix(1714560300, 0).UTC()

	k1 := ComputeDedupKey("x.y", hash, t1)
	k2 := ComputeDedupKey("x.y", hash, t2)

	if k1 == k2 {
		t.Errorf("keys must differ across buckets: %q == %q", k1, k2)
	}
}

func TestBucketBoundaryReturnsFloor(t *testing.T) {

	bucket := DedupBucket(time.Unix(1714560000, 0).UTC())
	got := BucketBoundary(bucket)
	if got.Unix() != bucket*int64(BucketSeconds) {
		t.Errorf("BucketBoundary unix = %d, want %d", got.Unix(), bucket*int64(BucketSeconds))
	}
}

func TestDedupBucketIgnoresSubsecondPrecision(t *testing.T) {
	t0 := time.Unix(1714560000, 0).UTC()
	t1 := time.Unix(1714560000, 999_999_999).UTC()
	if DedupBucket(t0) != DedupBucket(t1) {
		t.Errorf("nanosecond precision must not change bucket: %d vs %d", DedupBucket(t0), DedupBucket(t1))
	}
}
