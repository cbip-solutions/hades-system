package augment_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

func TestSummarize_ThreeClusters(t *testing.T) {
	fused := []augment.RRFFusedResult{

		{NoteID: "n1", Title: "Engine.SelectWinner | internal/orchestrator/merge/engine.go", Score: 2.5, ProjectID: "internal-platform-x", LaneIDs: []int{1, 2}},
		{NoteID: "n2", Title: "Engine.diff | internal/orchestrator/merge/engine.go", Score: 2.0, ProjectID: "internal-platform-x", LaneIDs: []int{2}},
		{NoteID: "n3", Title: "applyMerge | internal/orchestrator/merge/decision.go", Score: 1.8, ProjectID: "internal-platform-x", LaneIDs: []int{3}},

		{NoteID: "n4", Title: "BudgetGate.Check | internal/budget/enforce.go", Score: 1.5, ProjectID: "internal-platform-x", LaneIDs: []int{2, 4}},
		{NoteID: "n5", Title: "axisTagCompleteness | internal/budget/axes.go", Score: 1.2, ProjectID: "internal-platform-x", LaneIDs: []int{2}},

		{NoteID: "n6", Title: "Compute | internal/audit/chain/compute.go", Score: 1.0, ProjectID: "internal-platform-x", LaneIDs: []int{4}},
	}
	out, err := augment.Summarize(context.Background(), fused, "internal-platform-x")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 clusters, got %d: %+v", len(out), out)
	}

	if !strings.Contains(out[0].ClusterID, "internal/orchestrator") {
		t.Errorf("cluster[0] should be internal/orchestrator, got ClusterID=%q", out[0].ClusterID)
	}
	if len(out[0].Files) != 2 {
		t.Errorf("cluster[0] expected 2 files, got %d: %v", len(out[0].Files), out[0].Files)
	}
	if !contains(strings.Join(out[0].Symbols, ","), "Engine.SelectWinner") {
		t.Errorf("cluster[0] expected Engine.SelectWinner symbol, got %v", out[0].Symbols)
	}
	if out[0].TokenCount <= 0 {
		t.Errorf("cluster[0] expected positive TokenCount, got %d", out[0].TokenCount)
	}
}

func TestSummarize_DeterministicOrdering(t *testing.T) {
	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "FuncA | a/file1.go", Score: 1.0, ProjectID: "p", LaneIDs: []int{1}},
		{NoteID: "n2", Title: "FuncB | a/file2.go", Score: 1.0, ProjectID: "p", LaneIDs: []int{2}},
		{NoteID: "n3", Title: "FuncC | b/file3.go", Score: 1.0, ProjectID: "p", LaneIDs: []int{3}},
		{NoteID: "n4", Title: "FuncD | b/file4.go", Score: 1.0, ProjectID: "p", LaneIDs: []int{4}},
	}
	out1, err1 := augment.Summarize(context.Background(), fused, "p")
	if err1 != nil {
		t.Fatalf("Summarize 1: %v", err1)
	}
	out2, err2 := augment.Summarize(context.Background(), fused, "p")
	if err2 != nil {
		t.Fatalf("Summarize 2: %v", err2)
	}
	if !reflect.DeepEqual(out1, out2) {
		t.Errorf("non-deterministic output:\nout1=%+v\nout2=%+v", out1, out2)
	}
}

func TestSummarize_MaxCommunitiesCap(t *testing.T) {
	fused := make([]augment.RRFFusedResult, 0, 10)
	for i := 0; i < 10; i++ {
		fused = append(fused, augment.RRFFusedResult{
			NoteID:    fmt.Sprintf("n%d", i),
			Title:     fmt.Sprintf("Sym%d | dir%d/sub/file.go", i, i),
			Score:     float64(10 - i),
			ProjectID: "p",
			LaneIDs:   []int{1},
		})
	}
	out, err := augment.Summarize(context.Background(), fused, "p")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(out) != augment.MaxCommunitiesDefault {
		t.Fatalf("expected %d clusters (cap), got %d", augment.MaxCommunitiesDefault, len(out))
	}
	if !contains(strings.Join(out[0].Symbols, ","), "Sym0") {
		t.Errorf("top cluster should contain Sym0, got %v", out[0].Symbols)
	}
}

func TestSummarize_EmptyInput(t *testing.T) {
	out, err := augment.Summarize(context.Background(), nil, "p")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty output, got %+v", out)
	}
}

func TestSummarize_SingleResult(t *testing.T) {
	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "OneFunc | path/file.go", Score: 1.0, ProjectID: "p", LaneIDs: []int{1}},
	}
	out, err := augment.Summarize(context.Background(), fused, "p")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(out))
	}
	if len(out[0].NoteIDs) != 1 || out[0].NoteIDs[0] != "n1" {
		t.Errorf("cluster NoteIDs: want [n1], got %v", out[0].NoteIDs)
	}
}

func TestSummarize_ResultsWithoutPathFallback(t *testing.T) {
	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "Foo", Score: 1.0, ProjectID: "p", LaneIDs: []int{1}},
		{NoteID: "n2", Title: "Bar", Score: 1.0, ProjectID: "p", LaneIDs: []int{1}},
	}
	out, err := augment.Summarize(context.Background(), fused, "p")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 fallback cluster, got %d", len(out))
	}
	if out[0].ClusterID != "uncategorized" {
		t.Errorf("ClusterID: want uncategorized, got %q", out[0].ClusterID)
	}
}

func TestSummarize_TokenCountPositive(t *testing.T) {
	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "Func | a/b/c.go", Score: 1.0, ProjectID: "p", LaneIDs: []int{1}},
	}
	out, err := augment.Summarize(context.Background(), fused, "p")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(out) != 1 || out[0].TokenCount <= 0 {
		t.Errorf("expected positive TokenCount, got %+v", out)
	}
}

func TestSummarize_TopicAllUpperConst(t *testing.T) {
	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "MAX_BUDGET | const/budget.go", Score: 1.0, ProjectID: "p", LaneIDs: []int{1}},
	}
	out, err := augment.Summarize(context.Background(), fused, "p")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if out[0].Topic != "const" {
		t.Errorf("expected topic=const, got %q", out[0].Topic)
	}
}

func TestSummarize_TopicLowerFunction(t *testing.T) {
	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "fooBar | x/y.go", Score: 1.0, ProjectID: "p", LaneIDs: []int{1}},
	}
	out, err := augment.Summarize(context.Background(), fused, "p")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if out[0].Topic != "function" {
		t.Errorf("expected topic=function, got %q", out[0].Topic)
	}
}

func TestSummarize_TopicNonAlphaCode(t *testing.T) {

	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "1Func | x/y.go", Score: 1.0, ProjectID: "p", LaneIDs: []int{1}},
	}
	out, err := augment.Summarize(context.Background(), fused, "p")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if out[0].Topic != "code" {
		t.Errorf("expected topic=code, got %q", out[0].Topic)
	}
}

func TestSummarize_TitleWithLineSuffix(t *testing.T) {

	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "Foo | a/b/file.go:123", Score: 1.0, ProjectID: "p", LaneIDs: []int{1}},
	}
	out, err := augment.Summarize(context.Background(), fused, "p")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(out[0].Files) != 1 || out[0].Files[0] != "a/b/file.go" {
		t.Errorf("expected stripped path, got %v", out[0].Files)
	}
}

func TestSummarize_TitleWithNonNumericColon(t *testing.T) {

	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "Foo | mod:variant", Score: 1.0, ProjectID: "p", LaneIDs: []int{1}},
	}
	out, err := augment.Summarize(context.Background(), fused, "p")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	if len(out[0].Files) != 1 || !strings.Contains(out[0].Files[0], ":") {
		t.Errorf("non-numeric colon should be preserved, got %v", out[0].Files)
	}
}

func TestSummarize_AbsolutePath(t *testing.T) {

	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "f | /a/b/x.go", Score: 1.0, ProjectID: "p", LaneIDs: []int{1}},
	}
	out, err := augment.Summarize(context.Background(), fused, "p")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if !contains(out[0].ClusterID, "a") {
		t.Errorf("expected absolute path cluster to include 'a', got %q", out[0].ClusterID)
	}
}

func TestSummarize_SingleComponentPath(t *testing.T) {

	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "f | mainfile.go", Score: 1.0, ProjectID: "p", LaneIDs: []int{1}},
	}
	out, err := augment.Summarize(context.Background(), fused, "p")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(out) != 1 {
		t.Errorf("expected 1 cluster, got %d", len(out))
	}
}

func TestSummarize_DotPrefixSymbolHandled(t *testing.T) {

	fused := []augment.RRFFusedResult{
		{NoteID: "n1", Title: "Type. | a/b/x.go", Score: 1.0, ProjectID: "p", LaneIDs: []int{1}},
	}
	out, err := augment.Summarize(context.Background(), fused, "p")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if out[0].Topic != "code" {
		t.Errorf("empty core after dot should be code, got %q", out[0].Topic)
	}
}

func TestSummarize_TokenReductionVsRaw(t *testing.T) {
	const corpusSize = 1000

	roots := []string{
		"internal/orchestrator",
		"internal/augment",
		"internal/audit",
		"internal/budget",
		"internal/store",
	}
	fused := make([]augment.RRFFusedResult, 0, corpusSize)
	for i := 0; i < corpusSize; i++ {
		root := roots[i%len(roots)]
		fileIdx := (i / 50) % 20
		fused = append(fused, augment.RRFFusedResult{
			NoteID:  fmt.Sprintf("note-%05d", i),
			Title:   fmt.Sprintf("Func%d | %s/sub_%d/file_%d.go", i, root, fileIdx/5, fileIdx),
			Snippet: fmt.Sprintf("Snippet %d: lorem ipsum dolor sit amet consectetur adipiscing elit", i),
			Score:   float64(corpusSize-i) / float64(corpusSize),
		})
	}

	out, err := augment.Summarize(context.Background(), fused, "internal-platform-x")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("Summarize returned 0 clusters on 1000-triple corpus")
	}

	rawChars := 0
	for _, f := range fused {
		rawChars += len(f.Title) + len(f.Snippet) + len(f.NoteID) + 6
	}
	rawTokens := rawChars / 4

	summarizedTokens := 0
	for _, c := range out {
		summarizedTokens += c.TokenCount
	}

	if summarizedTokens == 0 {
		t.Fatal("summarized token count is 0; expected > 0")
	}
	if rawTokens == 0 {
		t.Fatal("raw token count is 0; corpus malformed")
	}

	ratio := float64(summarizedTokens) / float64(rawTokens)
	t.Logf("Token reduction: raw=%d tokens; summarized=%d tokens; ratio=%.4f (target <= 0.10)",
		rawTokens, summarizedTokens, ratio)
	if ratio > 0.10 {
		t.Errorf("token reduction failed: ratio=%.4f > 0.10 (spec target <= 0.10 — lenient bound vs the doc-cited 0.03 GraphRAG benchmark)", ratio)
	}
	if ratio <= 0 {
		t.Errorf("ratio=%.4f; want > 0 (summarized output must be non-empty)", ratio)
	}
}
