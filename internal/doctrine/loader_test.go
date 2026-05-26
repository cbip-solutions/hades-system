package doctrine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleDoctrineTOML = `
schema_version = 1
name = "custom-fast"

[research]
depth = "shallow"
sources = ["web_search", "arxiv"]
cache_ttl = "1h"
agentic_max_iter = 1

[subprocess]
ephemeral_default_timeout = "5m"
persistent_ttl_sliding = "1h"
pre_warm_pool_size = 0

[reviewer]
family_disjoint_pool = ["anthropic", "google"]
criteria_default = "default"

[budget]
pause_mode = "quiet"
anomaly_z_threshold = 3.5
anomaly_window_size = 30

[budget.caps]
project = "10.00 USD"
doctrine = "30.00 USD"

[budget.caps.stage]
design = "1.00 USD"

[budget.caps.task]
trivial = "0.05 USD"

[budget.caps.operation]
audit_review = "0.05 USD"

[workforce]
writable_paths_policy = "non-overlapping"
doctrine_reinforcement_template_pointer = "templates/custom.txt"

[apply]
merge_strategy = "three-way"
conflict_handling = "manual"

[watcher]
cadence = "5m"
cpu_budget = 0.01

[future.plan_99]
shape_alpha = "experimental"
`

func writeTempTOML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestLoadFileHappyPath(t *testing.T) {
	path := writeTempTOML(t, sampleDoctrineTOML)
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Schema.Name != "custom-fast" {
		t.Errorf("Name = %q", loaded.Schema.Name)
	}
	if loaded.Schema.Research.Depth != "shallow" {
		t.Errorf("Research.Depth = %q", loaded.Schema.Research.Depth)
	}
	if loaded.Schema.Research.AgenticMaxIter != 1 {
		t.Errorf("AgenticMaxIter = %d", loaded.Schema.Research.AgenticMaxIter)
	}
	if time.Duration(loaded.Schema.Research.CacheTTL) != 1*time.Hour {
		t.Errorf("CacheTTL = %v", time.Duration(loaded.Schema.Research.CacheTTL))
	}
	if loaded.Schema.Budget.Caps.Project != "10.00 USD" {
		t.Errorf("Caps.Project = %q", loaded.Schema.Budget.Caps.Project)
	}
	if loaded.Schema.Budget.Caps.Stage["design"] != "1.00 USD" {
		t.Errorf("Caps.Stage[design] = %q", loaded.Schema.Budget.Caps.Stage["design"])
	}
	if got := loaded.Schema.Future["plan_99"]["shape_alpha"]; got != "experimental" {
		t.Errorf("Future[plan_99] = %v", got)
	}
}

func TestLoadFileProvenance(t *testing.T) {
	path := writeTempTOML(t, sampleDoctrineTOML)
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	want := map[string]string{
		"name":                                              path,
		"schema_version":                                    path,
		"research.depth":                                    path,
		"research.cache_ttl":                                path,
		"research.agentic_max_iter":                         path,
		"subprocess.ephemeral_default_timeout":              path,
		"subprocess.persistent_ttl_sliding":                 path,
		"subprocess.pre_warm_pool_size":                     path,
		"reviewer.family_disjoint_pool":                     path,
		"reviewer.criteria_default":                         path,
		"budget.pause_mode":                                 path,
		"budget.anomaly_z_threshold":                        path,
		"budget.anomaly_window_size":                        path,
		"budget.caps.project":                               path,
		"budget.caps.doctrine":                              path,
		"workforce.writable_paths_policy":                   path,
		"workforce.doctrine_reinforcement_template_pointer": path,
		"apply.merge_strategy":                              path,
		"apply.conflict_handling":                           path,
		"watcher.cadence":                                   path,
		"watcher.cpu_budget":                                path,
	}
	for field, src := range want {
		got, ok := loaded.Provenance[field]
		if !ok {
			t.Errorf("Provenance missing %q", field)
			continue
		}
		if got != src {
			t.Errorf("Provenance[%q] = %q, want %q", field, got, src)
		}
	}
}

func TestLoadFileMissingFile(t *testing.T) {
	_, err := LoadFile("/nonexistent/path.toml")
	if err == nil {
		t.Fatal("LoadFile(nonexistent) returned nil error")
	}
	if !strings.Contains(err.Error(), "open") {
		t.Errorf("err = %v, want 'open' in message", err)
	}
}

func TestLoadFileMalformedTOML(t *testing.T) {
	path := writeTempTOML(t, "this = is = not = toml\n")
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile(garbage) returned nil error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("err = %v, want path %q in message", err, path)
	}
}

func TestLoadFileBadDuration(t *testing.T) {
	path := writeTempTOML(t, `
schema_version = 1
name = "broken"
[research]
cache_ttl = "not-a-duration"
`)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile(bad-duration) returned nil error")
	}
	if !strings.Contains(err.Error(), "duration") && !strings.Contains(err.Error(), "cache_ttl") {
		t.Errorf("err = %v, want 'duration' or 'cache_ttl' in message", err)
	}
}

func TestLoadFileBadMoneyProject(t *testing.T) {
	path := writeTempTOML(t, `
schema_version = 1
name = "broken"
[budget.caps]
project = "garbage"
`)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile(bad-money) returned nil error")
	}
	if !strings.Contains(err.Error(), "money") && !strings.Contains(err.Error(), "project") {
		t.Errorf("err = %v, want 'money' or 'project' in message", err)
	}
}

func TestLoadFileBadMoneyDoctrine(t *testing.T) {
	path := writeTempTOML(t, `
schema_version = 1
[budget.caps]
doctrine = "garbage"
`)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile(bad-money) returned nil error")
	}
	if !strings.Contains(err.Error(), "doctrine") {
		t.Errorf("err = %v, want 'doctrine' in message", err)
	}
}

func TestLoadFileBadMoneyStage(t *testing.T) {
	path := writeTempTOML(t, `
schema_version = 1
[budget.caps.stage]
design = "garbage"
`)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile(bad-money-stage) returned nil error")
	}
	if !strings.Contains(err.Error(), "stage.design") {
		t.Errorf("err = %v, want 'stage.design' in message", err)
	}
}

func TestLoadFileBadMoneyTask(t *testing.T) {
	path := writeTempTOML(t, `
schema_version = 1
[budget.caps.task]
complex = "garbage"
`)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile(bad-money-task) returned nil error")
	}
	if !strings.Contains(err.Error(), "task.complex") {
		t.Errorf("err = %v, want 'task.complex' in message", err)
	}
}

func TestLoadFileBadMoneyOperation(t *testing.T) {
	path := writeTempTOML(t, `
schema_version = 1
[budget.caps.operation]
audit_review = "garbage"
`)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile(bad-money-op) returned nil error")
	}
	if !strings.Contains(err.Error(), "operation.audit_review") {
		t.Errorf("err = %v, want 'operation.audit_review' in message", err)
	}
}

func TestLoadFileEmpty(t *testing.T) {
	path := writeTempTOML(t, "")
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile(empty): %v", err)
	}
	if loaded.Schema.Name != "" {
		t.Errorf("empty file produced Name = %q", loaded.Schema.Name)
	}
	if len(loaded.Provenance) != 0 {
		t.Errorf("empty file produced %d provenance entries, want 0", len(loaded.Provenance))
	}
}

func TestLoadFileFutureSection(t *testing.T) {
	path := writeTempTOML(t, `
schema_version = 1
name = "with-future"
[future.plan_42]
foo = "bar"
[future.plan_42.nested]
a = 1
b = 2
[future.plan_43]
single = "value"
`)
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Schema.Future["plan_42"]["foo"] != "bar" {
		t.Errorf("Future[plan_42][foo] not preserved")
	}
	if loaded.Schema.Future["plan_43"]["single"] != "value" {
		t.Errorf("Future[plan_43][single] not preserved")
	}
}

func TestLoadFileReadOnlyMode(t *testing.T) {
	path := writeTempTOML(t, sampleDoctrineTOML)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	if _, err := LoadFile(path); err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Error("LoadFile mutated source file")
	}
}

func TestLoadedZeroValueSafe(t *testing.T) {
	var l Loaded
	if _, ok := l.Provenance["anything"]; ok {
		t.Error("zero Loaded.Provenance reported ok=true")
	}
}

func TestLoadFileEmptyMoneyValuesSkipped(t *testing.T) {
	path := writeTempTOML(t, `
schema_version = 1
[budget.caps.stage]
design = ""
[budget.caps.task]
trivial = ""
[budget.caps.operation]
audit_review = ""
`)
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile(empty-money-skipped): %v", err)
	}
	if loaded.Schema.Budget.Caps.Stage["design"] != "" {
		t.Errorf("Stage[design] = %q, want empty", loaded.Schema.Budget.Caps.Stage["design"])
	}
}

// TestLoadFileRejectsUnknownField an unknown top-level field (likely a
// typo like `pre_warm_pol_size`) MUST fail to load rather than silently
// dropping. Operator typos are a real failure mode and "looks like
// success" is the worst possible outcome.
func TestLoadFileRejectsUnknownField(t *testing.T) {
	path := writeTempTOML(t, `
schema_version = 1
name = "typo-doctrine"

[subprocess]
pre_warm_pol_size = 5  # typo: "pol" not "pool"
`)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile(unknown-field) returned nil error, want strict-mode rejection")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("err = %v, want 'unknown field' in message", err)
	}
	if !strings.Contains(err.Error(), "pre_warm_pol_size") {
		t.Errorf("err = %v, want 'pre_warm_pol_size' in message", err)
	}
}

func TestLoadFileRejectsUnknownNestedField(t *testing.T) {
	path := writeTempTOML(t, `
schema_version = 1
[research]
depth = "deep"
totally_unknown_key = "value"
`)
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile(unknown-nested) returned nil error")
	}
	if !strings.Contains(err.Error(), "totally_unknown_key") {
		t.Errorf("err = %v, want 'totally_unknown_key' in message", err)
	}
}

func TestLoadFileAcceptsFutureFields(t *testing.T) {
	path := writeTempTOML(t, `
schema_version = 1
name = "with-future"

[future.plan_99]
shape_alpha = "experimental"
totally_arbitrary_key = "passes_strict"

[future.plan_99.deeply.nested]
even_this = 42
`)
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile(future-fields): %v", err)
	}
	if loaded.Schema.Future["plan_99"]["shape_alpha"] != "experimental" {
		t.Errorf("Future[plan_99][shape_alpha] not preserved")
	}
}
