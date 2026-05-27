// go:build cgo
//go:build cgo
// +build cgo

package structure

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

var updateGolden = flag.Bool("update", false, "regenerate the structure golden fixture")

type goldenSnapshot struct {
	Coreness  map[string]int    `json:"coreness"`
	SCCID     map[string]int    `json:"scc_id"`
	PackageID map[string]string `json:"package_id"`
	SCCSize   map[string]int    `json:"scc_size"`
}

func snapshot(d Decomposition) goldenSnapshot {
	size := make(map[string]int, len(d.SCCSize))
	for k, v := range d.SCCSize {
		size[strconv.Itoa(k)] = v
	}
	return goldenSnapshot{Coreness: d.Coreness, SCCID: d.SCCID, PackageID: d.PackageID, SCCSize: size}
}

func marshalStable(t *testing.T, s goldenSnapshot) []byte {
	t.Helper()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return append(b, '\n')
}

func TestDeterminismGolden(t *testing.T) {
	goldenPath := filepath.Join("golden", "structure_basic.json")

	run := func() Decomposition {
		s := newStore(t)
		seedTrianglePlusCycle(t, s)
		dec, _, err := Recompute(context.Background(), s, "")
		if err != nil {
			t.Fatalf("Recompute: %v", err)
		}
		return dec
	}

	first := run()
	firstBytes := marshalStable(t, snapshot(first))
	for i := 0; i < 5; i++ {
		d := run()
		if !reflect.DeepEqual(snapshot(d), snapshot(first)) {
			t.Fatalf("run %d decomposition differs from run 0 (non-deterministic!)", i)
		}
		if string(marshalStable(t, snapshot(d))) != string(firstBytes) {
			t.Fatalf("run %d serialization differs byte-wise (non-deterministic!)", i)
		}
	}

	if run().HashKey != first.HashKey {
		t.Fatal("HashKey not stable across runs")
	}

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden: %v", err)
		}
		if err := os.WriteFile(goldenPath, firstBytes, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("golden updated: %s", goldenPath)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(want) != string(firstBytes) {
		t.Errorf("decomposition does not match golden %s\n--- got ---\n%s\n--- want ---\n%s\n(run: go test -tags=sqlite_fts5 ./internal/caronte/structure/ -run TestDeterminismGolden -update)", goldenPath, firstBytes, want)
	}
}

func TestDeterminismAcrossInputOrder(t *testing.T) {
	nodes := []store.Node{
		{NodeID: "pkg/x.A", FilePath: "pkg/x/a.go", Language: "go", ContentHash: "h"},
		{NodeID: "pkg/x.B", FilePath: "pkg/x/b.go", Language: "go", ContentHash: "h"},
		{NodeID: "pkg/x.C", FilePath: "pkg/x/c.go", Language: "go", ContentHash: "h"},
	}
	edges := []store.Edge{
		{SourceID: "pkg/x.A", TargetID: "pkg/x.B", Kind: string(store.EdgeCalls), Confidence: store.ConfExactStatic, SiteLine: 1},
		{SourceID: "pkg/x.B", TargetID: "pkg/x.C", Kind: string(store.EdgeCalls), Confidence: store.ConfExactStatic, SiteLine: 2},
		{SourceID: "pkg/x.C", TargetID: "pkg/x.A", Kind: string(store.EdgeCalls), Confidence: store.ConfExactStatic, SiteLine: 3},
	}
	a := snapshot(Compute(nodes, edges))

	rn := []store.Node{nodes[2], nodes[1], nodes[0]}
	re := []store.Edge{edges[2], edges[1], edges[0]}
	b := snapshot(Compute(rn, re))
	if !reflect.DeepEqual(a, b) {
		t.Errorf("decomposition depends on input order:\n got %+v\nwant %+v", b, a)
	}

	keys := make([]string, 0, len(a.Coreness))
	for k := range a.Coreness {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) != 3 {
		t.Errorf("want 3 nodes, got %d", len(keys))
	}
}
