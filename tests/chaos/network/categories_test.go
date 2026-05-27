// go:build chaos

// Toxiproxy-driven chaos package.
//
// categories_test.go pins the Category taxonomy contract: every
// canonical toxic maps to exactly one Category, the 3-category set is
// the partition of AllToxicTypes(), and the per-category bucketing
// + filter behave deterministically. These tests do NOT need a
// running Toxiproxy daemon — they exercise pure taxonomy semantics.

package network

import (
	"slices"
	"testing"
)

// TestCategoriesPartitionAllToxicTypes pins the load-bearing contract:
// AllToxicTypes() = union of all category buckets, with no overlap and
// no CategoryUnknown leakage. Drift on either side (a new toxic added
// without a category, or a toxic mapped to two categories) MUST fail
// here BEFORE the 80-scenario matrix walker dispatches into a default
// branch.
func TestCategoriesPartitionAllToxicTypes(t *testing.T) {
	if err := validateCategoryPartition(); err != nil {
		t.Fatalf("category partition: %v", err)
	}
}

func TestCategoryOfTable(t *testing.T) {
	cases := []struct {
		tox  ToxicType
		want Category
	}{

		{ToxicDown, CategoryClose},
		{ToxicTimeout, CategoryClose},
		{ToxicResetPeer, CategoryClose},

		{ToxicLatency, CategoryDegrade},
		{ToxicBandwidth, CategoryDegrade},
		{ToxicSlowClose, CategoryDegrade},
		{ToxicSlicer, CategoryDegrade},

		{ToxicLimitData, CategoryCorrupt},
		{ToxicModifyBuffer, CategoryCorrupt},
		{ToxicModifyRate, CategoryCorrupt},
	}
	if got, want := len(cases), len(AllToxicTypes()); got != want {
		t.Fatalf("test-case count = %d, want %d (one per canonical toxic)", got, want)
	}
	for _, c := range cases {
		t.Run(string(c.tox), func(t *testing.T) {
			if got := CategoryOf(c.tox); got != c.want {
				t.Errorf("CategoryOf(%s) = %s, want %s", c.tox, got, c.want)
			}
		})
	}
}

func TestCategoryOfUnknownReturnsCategoryUnknown(t *testing.T) {
	if got := CategoryOf(ToxicType("synthetic_future_toxic")); got != CategoryUnknown {
		t.Errorf("CategoryOf(unknown) = %s, want CategoryUnknown", got)
	}
}

func TestAllCategoriesHasExactlyThree(t *testing.T) {
	if got := len(AllCategories()); got != 3 {
		t.Fatalf("AllCategories() = %d, want 3", got)
	}
}

func TestCategoryStringStable(t *testing.T) {
	cases := []struct {
		cat  Category
		want string
	}{
		{CategoryUnknown, "unknown"},
		{CategoryClose, "close"},
		{CategoryDegrade, "degrade"},
		{CategoryCorrupt, "corrupt"},
	}
	for _, c := range cases {
		if got := c.cat.String(); got != c.want {
			t.Errorf("Category(%d).String() = %q, want %q", c.cat, got, c.want)
		}
	}
}

func TestToxicsByCategoryDeterministic(t *testing.T) {
	buckets := ToxicsByCategory()
	if got, want := len(buckets), 3; got != want {
		t.Fatalf("buckets = %d, want %d (one per category)", got, want)
	}
	for cat, toxics := range buckets {
		t.Run(cat.String(), func(t *testing.T) {
			sorted := slices.Clone(toxics)
			slices.Sort(sorted)
			if !slices.Equal(toxics, sorted) {
				t.Errorf("category %s slice not sorted: got=%v want=%v", cat, toxics, sorted)
			}
		})
	}
}

func TestToxicsByCategoryCoverage(t *testing.T) {
	buckets := ToxicsByCategory()
	var flat []ToxicType
	for _, slice := range buckets {
		flat = append(flat, slice...)
	}
	if got, want := len(flat), len(AllToxicTypes()); got != want {
		t.Fatalf("flat bucket count = %d, want %d", got, want)
	}
	seen := map[ToxicType]bool{}
	for _, tox := range flat {
		if seen[tox] {
			t.Errorf("toxic %s appears in multiple buckets", tox)
		}
		seen[tox] = true
	}
	for _, tox := range AllToxicTypes() {
		if !seen[tox] {
			t.Errorf("toxic %s missing from bucket coverage", tox)
		}
	}
}

func TestFilterByCategoryDegradeSubMatrix(t *testing.T) {
	reg := &Registry{
		Edges: map[string]EdgeConfig{
			"a": {Listen: "127.0.0.1:39001"},
			"b": {Listen: "127.0.0.1:39002"},
		},
	}
	all := GenerateScenarios(reg)
	if got, want := len(all), 2*10; got != want {
		t.Fatalf("full matrix = %d, want %d", got, want)
	}
	wantPerCat := map[Category]int{
		CategoryClose:   2 * 3,
		CategoryDegrade: 2 * 4,
		CategoryCorrupt: 2 * 3,
	}
	var sum int
	for _, cat := range AllCategories() {
		filtered := FilterByCategory(all, cat)
		if got, want := len(filtered), wantPerCat[cat]; got != want {
			t.Errorf("filter(%s) = %d scenarios, want %d", cat, got, want)
		}

		for _, s := range filtered {
			if got := CategoryOf(s.Toxic); got != cat {
				t.Errorf("scenario %s in %s bucket has CategoryOf=%s", s, cat, got)
			}
		}
		sum += len(filtered)
	}
	if got, want := sum, len(all); got != want {
		t.Errorf("filtered sum = %d, want %d (partition)", got, want)
	}
}

func TestFilterByCategoryEmptyReturnsNonNilSlice(t *testing.T) {
	got := FilterByCategory(nil, CategoryClose)
	if got == nil {
		t.Fatal("FilterByCategory(nil, ...) returned nil; want empty slice")
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0", len(got))
	}
}
