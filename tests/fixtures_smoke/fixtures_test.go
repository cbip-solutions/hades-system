package fixtures_smoke_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatalf("repo root not found from %s", wd)
		}
		wd = parent
	}
}

func TestTesseraFixturesShape(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		dir  string
		size int
	}{
		{"small", 10},
		{"mid", 1000},
		{"large", 10000},
	}
	for _, c := range cases {
		t.Run(c.dir, func(t *testing.T) {
			leavesPath := filepath.Join(root, "tests/testdata/tessera", c.dir, "leaves.json")
			sthPath := filepath.Join(root, "tests/testdata/tessera", c.dir, "sth.json")

			leavesBytes, err := os.ReadFile(leavesPath)
			if err != nil {
				t.Fatalf("read %s: %v", leavesPath, err)
			}
			var leaves []map[string]any
			if err := json.Unmarshal(leavesBytes, &leaves); err != nil {
				t.Fatalf("unmarshal leaves: %v", err)
			}
			if len(leaves) != c.size {
				t.Errorf("leaves count = %d; want %d", len(leaves), c.size)
			}

			sthBytes, err := os.ReadFile(sthPath)
			if err != nil {
				t.Fatalf("read %s: %v", sthPath, err)
			}
			var sth map[string]any
			if err := json.Unmarshal(sthBytes, &sth); err != nil {
				t.Fatalf("unmarshal sth: %v", err)
			}
			sizeFloat, ok := sth["size"].(float64)
			if !ok {
				t.Fatalf("sth.size missing or not number: %+v", sth)
			}
			if int(sizeFloat) != c.size {
				t.Errorf("sth.size = %d; want %d", int(sizeFloat), c.size)
			}
			rootHash, ok := sth["root_hash_hex"].(string)
			if !ok || len(rootHash) != 64 {
				t.Errorf("sth.root_hash_hex = %q (len %d); want 64-hex string", rootHash, len(rootHash))
			}
		})
	}
}

func TestAuditGoldenChainReproduces(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "tests/testdata/audit_events_raw/golden_chain.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var fixture struct {
		Count  int `json:"count"`
		Events []struct {
			Seq        int    `json:"seq"`
			EventType  string `json:"event_type"`
			Payload    string `json:"payload"`
			EmittedAt  int64  `json:"emitted_at_unix_seconds"`
			PrevHash   string `json:"prev_hash"`
			RecordHash string `json:"record_hash"`
		} `json:"events"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(fixture.Events) != fixture.Count {
		t.Fatalf("count mismatch: header=%d events=%d", fixture.Count, len(fixture.Events))
	}
	for _, e := range fixture.Events {
		got, err := chain.Compute(e.PrevHash, e.EventType, []byte(e.Payload), e.EmittedAt)
		if err != nil {
			t.Errorf("seq=%d Compute: %v", e.Seq, err)
			continue
		}
		if got != e.RecordHash {
			t.Errorf("seq=%d hash drift: got=%s want=%s", e.Seq, got, e.RecordHash)
		}
	}
}

func TestResearchFindingsCorpusShape(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "tests/testdata/research_cache/findings_corpus.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var corpus struct {
		Count    int `json:"count"`
		Findings []struct {
			ID          string `json:"id"`
			DispatchID  string `json:"dispatch_id"`
			URL         string `json:"url"`
			Snippet     string `json:"snippet"`
			ContentHash string `json:"content_hash"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if corpus.Count != 50 {
		t.Errorf("count = %d; want 50", corpus.Count)
	}
	if len(corpus.Findings) != corpus.Count {
		t.Errorf("findings len = %d; want %d", len(corpus.Findings), corpus.Count)
	}
	for i, f := range corpus.Findings {
		if f.ID == "" || f.DispatchID == "" || f.URL == "" || f.ContentHash == "" {
			t.Errorf("finding[%d] missing required field: %+v", i, f)
		}
		if len(f.ContentHash) != 64 {
			t.Errorf("finding[%d] content_hash len = %d; want 64", i, len(f.ContentHash))
		}
	}
}

func TestADRCorpusPresent(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "tests/testdata/adr_corpus")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
			count++
		}
	}
	if count == 0 {
		t.Errorf("no ADR .md files under %s; regenerator may have been skipped", dir)
	}
}

func TestADRCorpusFirstEightArePublicSafe(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "tests/testdata/adr_corpus")
	matches, err := filepath.Glob(filepath.Join(dir, "000[1-8]-*.md"))
	if err != nil {
		t.Fatalf("glob first eight ADR fixtures: %v", err)
	}
	if len(matches) != 8 {
		t.Fatalf("first-eight ADR fixture count = %d; want 8", len(matches))
	}
	forbidden := []string{
		"openclaude",
		"opencode",
		"gitnexus",
		"claude code",
		"gitlawb",
		"argus",
		"project-atlas",
		"/users/",
		"cbip-solutions",
	}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		haystack := strings.ToLower(filepath.Base(path) + "\n" + string(data))
		for _, bad := range forbidden {
			if strings.Contains(haystack, bad) {
				t.Errorf("%s contains public-unsafe fixture token %q", filepath.Base(path), bad)
			}
		}
	}
}

func TestADRMalformedCasesPresent(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "tests/testdata/adr_corpus/malformed")
	required := []string{
		"missing_id.md",
		"duplicate_id_a.md",
		"duplicate_id_b.md",
		"invalid_yaml.md",
		"cycle_a.md",
		"cycle_b.md",
	}
	for _, name := range required {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing malformed fixture %s: %v", name, err)
		}
	}

	for _, name := range required {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if !startsWithFrontmatterDelimiter(data) {
			t.Errorf("%s missing leading '---' frontmatter delimiter", name)
		}
	}
}

func startsWithFrontmatterDelimiter(data []byte) bool {
	const delim = "---"
	if len(data) < len(delim) {
		return false
	}
	return string(data[:len(delim)]) == delim
}
