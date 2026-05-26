package doctrine

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveBuiltinOnly(t *testing.T) {
	r := Resolver{ChosenDoctrine: "max-scope"}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Schema.Name != "max-scope" {
		t.Errorf("Name = %q", res.Schema.Name)
	}
	if res.Schema.Research.Depth != "deep" {
		t.Errorf("Research.Depth = %q", res.Schema.Research.Depth)
	}
	for k, src := range res.Provenance {
		if !strings.HasPrefix(src, "builtin:") {
			t.Errorf("Provenance[%q] = %q, want builtin:*", k, src)
		}
	}
}

func TestResolveBuiltinDefaultEmpty(t *testing.T) {
	var r Resolver
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve(empty): %v", err)
	}
	if res.Schema.Name != "max-scope" {
		t.Errorf("Name = %q, want max-scope (empty defaults)", res.Schema.Name)
	}
}

func TestResolveBuiltinUnknown(t *testing.T) {
	r := Resolver{ChosenDoctrine: "frobnicate"}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("Resolve(unknown) returned nil error")
	}
	if !strings.Contains(err.Error(), "frobnicate") {
		t.Errorf("err = %v", err)
	}
}

func TestResolveCustomNameNoCustomPath(t *testing.T) {
	r := Resolver{ChosenDoctrine: "custom-x"}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("Resolve(custom-no-path) returned nil error")
	}
	if !strings.Contains(err.Error(), "custom") {
		t.Errorf("err = %v, want 'custom' in message", err)
	}
	if !strings.Contains(err.Error(), "custom_path") && !strings.Contains(err.Error(), "--doctrine-path") {
		t.Errorf("err = %v, want operator-facing 'custom_path' or '--doctrine-path' hint", err)
	}
}

func TestResolveCustomDoctrineOverlay(t *testing.T) {
	customPath := writeTempTOML(t, `
schema_version = 1
name = "custom-fast"
[research]
depth = "shallow"
agentic_max_iter = 2
[budget]
pause_mode = "fail_loud"
`)
	r := Resolver{
		ChosenDoctrine:     "custom-fast",
		CustomDoctrinePath: customPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Schema.Name != "custom-fast" {
		t.Errorf("Name = %q", res.Schema.Name)
	}
	if res.Schema.Research.Depth != "shallow" {
		t.Errorf("Research.Depth = %q", res.Schema.Research.Depth)
	}
	if res.Schema.Research.AgenticMaxIter != 2 {
		t.Errorf("AgenticMaxIter = %d", res.Schema.Research.AgenticMaxIter)
	}
	if res.Schema.Budget.PauseMode != "fail_loud" {
		t.Errorf("Budget.PauseMode = %q", res.Schema.Budget.PauseMode)
	}
	if got := res.Provenance["research.depth"]; got != customPath {
		t.Errorf("Provenance[research.depth] = %q, want %q", got, customPath)
	}
}

func TestResolveCustomLoadError(t *testing.T) {
	r := Resolver{
		ChosenDoctrine:     "max-scope",
		CustomDoctrinePath: "/nonexistent/path.toml",
	}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("Resolve(custom-load-error) returned nil error")
	}
	if !strings.Contains(err.Error(), "custom layer") {
		t.Errorf("err = %v, want 'custom layer' in message", err)
	}
}

func TestResolveProjectOverlay(t *testing.T) {
	projectPath := writeTempTOML(t, `
schema_version = 1
name = "max-scope"
[research]
agentic_max_iter = 2
[budget.caps]
project = "30.00 USD"
`)
	r := Resolver{
		ChosenDoctrine: "max-scope",
		ProjectPath:    projectPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Schema.Research.AgenticMaxIter != 2 {
		t.Errorf("AgenticMaxIter = %d", res.Schema.Research.AgenticMaxIter)
	}
	if res.Schema.Budget.Caps.Project != "30.00 USD" {
		t.Errorf("Caps.Project = %q", res.Schema.Budget.Caps.Project)
	}
	if res.Provenance["research.agentic_max_iter"] != projectPath {
		t.Errorf("Provenance[research.agentic_max_iter] = %q, want %q",
			res.Provenance["research.agentic_max_iter"], projectPath)
	}
}

func TestResolveProjectLoadError(t *testing.T) {
	r := Resolver{
		ChosenDoctrine: "max-scope",
		ProjectPath:    "/nonexistent/zenswarm.toml",
	}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("Resolve(project-load-error) returned nil error")
	}
	if !strings.Contains(err.Error(), "project layer") {
		t.Errorf("err = %v, want 'project layer' in message", err)
	}
}

func TestResolveProjectClampedByCeiling(t *testing.T) {
	projectPath := writeTempTOML(t, `
schema_version = 1
name = "max-scope"
[budget.caps]
project = "500.00 USD"
`)
	r := Resolver{
		ChosenDoctrine: "max-scope",
		ProjectPath:    projectPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Schema.Budget.Caps.Project != "100.00 USD" {
		t.Errorf("Caps.Project = %q, want clamped to 100.00 USD",
			res.Schema.Budget.Caps.Project)
	}
	if got := res.Provenance["budget.caps.project"]; !strings.Contains(got, "clamped") {
		t.Errorf("Provenance[budget.caps.project] = %q, want substring 'clamped'", got)
	}
}

func TestResolveProjectClampedDoctrineCap(t *testing.T) {
	projectPath := writeTempTOML(t, `
schema_version = 1
name = "max-scope"
[budget.caps]
doctrine = "9999.00 USD"
`)
	r := Resolver{
		ChosenDoctrine: "max-scope",
		ProjectPath:    projectPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Schema.Budget.Caps.Doctrine != "500.00 USD" {
		t.Errorf("Caps.Doctrine = %q, want clamped to 500.00 USD",
			res.Schema.Budget.Caps.Doctrine)
	}
	if got := res.Provenance["budget.caps.doctrine"]; !strings.Contains(got, "clamped") {
		t.Errorf("Provenance[budget.caps.doctrine] = %q, want substring 'clamped'", got)
	}
}

func TestResolveProjectClampedStageMap(t *testing.T) {
	projectPath := writeTempTOML(t, `
schema_version = 1
name = "max-scope"
[budget.caps.stage]
build = "999.00 USD"
[budget.caps.task]
complex = "999.00 USD"
[budget.caps.operation]
audit_review = "999.00 USD"
`)
	r := Resolver{
		ChosenDoctrine: "max-scope",
		ProjectPath:    projectPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if res.Schema.Budget.Caps.Stage["build"] != "10.00 USD" {
		t.Errorf("Stage[build] = %q", res.Schema.Budget.Caps.Stage["build"])
	}
	if res.Schema.Budget.Caps.Task["complex"] != "2.00 USD" {
		t.Errorf("Task[complex] = %q", res.Schema.Budget.Caps.Task["complex"])
	}
	if res.Schema.Budget.Caps.Operation["audit_review"] != "0.10 USD" {
		t.Errorf("Operation[audit_review] = %q", res.Schema.Budget.Caps.Operation["audit_review"])
	}
}

func TestResolveProjectStageMapKeyAbsentFromCeiling(t *testing.T) {
	projectPath := writeTempTOML(t, `
schema_version = 1
name = "max-scope"
[budget.caps.stage]
new_phase = "2.00 USD"
[budget.caps.task]
new_tier = "2.00 USD"
[budget.caps.operation]
new_op = "2.00 USD"
`)
	r := Resolver{
		ChosenDoctrine: "max-scope",
		ProjectPath:    projectPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Schema.Budget.Caps.Stage["new_phase"] != "2.00 USD" {
		t.Errorf("Stage[new_phase] = %q", res.Schema.Budget.Caps.Stage["new_phase"])
	}
	if res.Schema.Budget.Caps.Task["new_tier"] != "2.00 USD" {
		t.Errorf("Task[new_tier] = %q", res.Schema.Budget.Caps.Task["new_tier"])
	}
	if res.Schema.Budget.Caps.Operation["new_op"] != "2.00 USD" {
		t.Errorf("Operation[new_op] = %q", res.Schema.Budget.Caps.Operation["new_op"])
	}
}

func TestResolveProjectMoneyCurrencyMismatch(t *testing.T) {
	projectPath := writeTempTOML(t, `
schema_version = 1
name = "max-scope"
[budget.caps]
project = "9999.00 EUR"
`)
	r := Resolver{
		ChosenDoctrine: "max-scope",
		ProjectPath:    projectPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Schema.Budget.Caps.Project != "9999.00 EUR" {
		t.Errorf("Caps.Project = %q, want preserved (currency mismatch)", res.Schema.Budget.Caps.Project)
	}
	if got := res.Provenance["budget.caps.project"]; strings.Contains(got, "clamped") {
		t.Errorf("Provenance[budget.caps.project] = %q, did NOT expect 'clamped' prefix", got)
	}
	if got := res.Provenance["budget.caps.project"]; !strings.HasPrefix(got, "currency-mismatch:") {
		t.Errorf("Provenance[budget.caps.project] = %q, want 'currency-mismatch:' prefix (I-1)", got)
	}
}

func TestResolveCurrencyMismatchTagged(t *testing.T) {
	projectPath := writeTempTOML(t, `
schema_version = 1
name = "max-scope"

[budget.caps]
project = "9999.00 EUR"
doctrine = "999.00 EUR"

[budget.caps.stage]
build = "999.00 EUR"

[budget.caps.task]
complex = "999.00 EUR"

[budget.caps.operation]
audit_review = "999.00 EUR"
`)
	r := Resolver{
		ChosenDoctrine: "max-scope",
		ProjectPath:    projectPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	mismatches := []string{
		"budget.caps.project",
		"budget.caps.doctrine",
		"budget.caps.stage.build",
		"budget.caps.task.complex",
		"budget.caps.operation.audit_review",
	}
	for _, field := range mismatches {
		got := res.Provenance[field]
		if !strings.HasPrefix(got, "currency-mismatch:") {
			t.Errorf("Provenance[%q] = %q, want 'currency-mismatch:' prefix", field, got)
		}
		if strings.Contains(got, "clamped:") {
			t.Errorf("Provenance[%q] = %q, must NOT carry 'clamped:' (no FX comparison occurred)", field, got)
		}

		if !strings.Contains(got, projectPath) {
			t.Errorf("Provenance[%q] = %q, want project path in tag", field, got)
		}
	}
}

func TestApplyClampMarkersWithMismatches(t *testing.T) {
	input := map[string]string{
		"budget.caps.project":  "/p.toml",
		"budget.caps.doctrine": "/p.toml",
		"budget.caps.stage.x":  "/p.toml",
		"research.depth":       "/p.toml",
	}
	clamps := map[string]bool{"budget.caps.project": true}
	mismatches := map[string]bool{
		"budget.caps.doctrine": true,
		"budget.caps.stage.x":  true,
	}
	out := applyClampMarkersWithMismatches(input, clamps, mismatches)
	if got := out["budget.caps.project"]; got != "clamped:/p.toml" {
		t.Errorf("clamp tag missing: %q", got)
	}
	if got := out["budget.caps.doctrine"]; got != "currency-mismatch:/p.toml" {
		t.Errorf("currency-mismatch tag missing: %q", got)
	}
	if got := out["budget.caps.stage.x"]; got != "currency-mismatch:/p.toml" {
		t.Errorf("currency-mismatch tag missing on stage: %q", got)
	}
	if got := out["research.depth"]; got != "/p.toml" {
		t.Errorf("non-budget field altered: %q", got)
	}

	if input["budget.caps.project"] != "/p.toml" {
		t.Error("input mutated")
	}
}

func TestApplyClampMarkersClampWinsOnCollision(t *testing.T) {
	input := map[string]string{"budget.caps.project": "/p.toml"}
	clamps := map[string]bool{"budget.caps.project": true}
	mismatches := map[string]bool{"budget.caps.project": true}
	out := applyClampMarkersWithMismatches(input, clamps, mismatches)
	if got := out["budget.caps.project"]; got != "clamped:/p.toml" {
		t.Errorf("collision precedence wrong: got %q, want 'clamped:/p.toml'", got)
	}
}

func TestResolveFlagOverride(t *testing.T) {
	r := Resolver{
		ChosenDoctrine: "max-scope",
		FlagDoctrine:   "default",
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Schema.Name != "default" {
		t.Errorf("Name = %q", res.Schema.Name)
	}
}

func TestResolveFlagOverrideUnknown(t *testing.T) {
	r := Resolver{FlagDoctrine: "totally-bogus"}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("Resolve(flag-unknown) returned nil error")
	}
	if !strings.Contains(err.Error(), "totally-bogus") {
		t.Errorf("err = %v, want bogus name in message", err)
	}
}

func TestResolveLayerOrder(t *testing.T) {
	customPath := writeTempTOML(t, `
schema_version = 1
name = "custom-x"
[research]
agentic_max_iter = 7
`)
	projectPath := writeTempTOML(t, `
schema_version = 1
[research]
agentic_max_iter = 4
`)
	r := Resolver{
		ChosenDoctrine:     "custom-x",
		CustomDoctrinePath: customPath,
		ProjectPath:        projectPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Schema.Research.AgenticMaxIter != 4 {
		t.Errorf("AgenticMaxIter = %d, want 4", res.Schema.Research.AgenticMaxIter)
	}
	if res.Provenance["research.agentic_max_iter"] != projectPath {
		t.Errorf("Provenance[research.agentic_max_iter] = %q, want %q",
			res.Provenance["research.agentic_max_iter"], projectPath)
	}
}

func TestResolveProjectNoCustom(t *testing.T) {
	projectPath := writeTempTOML(t, `
schema_version = 1
[subprocess]
pre_warm_pool_size = 7
`)
	r := Resolver{
		ChosenDoctrine: "default",
		ProjectPath:    projectPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Schema.Name != "default" {
		t.Errorf("Name = %q", res.Schema.Name)
	}
	if res.Schema.Subprocess.PreWarmPoolSize != 7 {
		t.Errorf("PreWarmPoolSize = %d, want 7", res.Schema.Subprocess.PreWarmPoolSize)
	}
}

func TestResolveCustomFutureMerge(t *testing.T) {
	customPath := writeTempTOML(t, `
schema_version = 1
name = "custom-fut"
[future.plan_42]
foo = "bar"
[future.plan_99]
existing = "value"
`)
	projectPath := writeTempTOML(t, `
schema_version = 1
[future.plan_42]
extra = "added"
[future.plan_99]
existing = "overridden"
`)
	r := Resolver{
		ChosenDoctrine:     "custom-fut",
		CustomDoctrinePath: customPath,
		ProjectPath:        projectPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Schema.Future["plan_42"]["foo"] != "bar" {
		t.Errorf("plan_42.foo = %v, want bar", res.Schema.Future["plan_42"]["foo"])
	}
	if res.Schema.Future["plan_42"]["extra"] != "added" {
		t.Errorf("plan_42.extra = %v, want added", res.Schema.Future["plan_42"]["extra"])
	}
	if res.Schema.Future["plan_99"]["existing"] != "overridden" {
		t.Errorf("plan_99.existing = %v, want overridden (top wins)",
			res.Schema.Future["plan_99"]["existing"])
	}
}

func TestResolveAllAxesOverlay(t *testing.T) {
	projectPath := writeTempTOML(t, `
schema_version = 2
name = "renamed-project"

[research]
depth = "medium"
sources = ["arxiv"]
cache_ttl = "2h"
agentic_max_iter = 1
[research.cadence_per_stage]
custom = "always"

[subprocess]
ephemeral_default_timeout = "10m"
persistent_ttl_sliding = "2h"
pre_warm_pool_size = 9

[reviewer]
family_disjoint_pool = ["x", "y"]
criteria_default = "security"

[budget]
pause_mode = "quiet"
anomaly_z_threshold = 2.0
anomaly_window_size = 90

[budget.caps]
project = "10.00 USD"
doctrine = "20.00 USD"

[workforce]
writable_paths_policy = "shared-readonly"
doctrine_reinforcement_template_pointer = "templates/p.txt"

[apply]
merge_strategy = "ours"
conflict_handling = "auto-prefer-ours"

[watcher]
cadence = "1m"
cpu_budget = 0.5
`)
	r := Resolver{
		ChosenDoctrine: "max-scope",
		ProjectPath:    projectPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	s := res.Schema
	if s.SchemaVersion != 2 {
		t.Errorf("SchemaVersion = %d", s.SchemaVersion)
	}
	if s.Name != "renamed-project" {
		t.Errorf("Name = %q", s.Name)
	}
	if s.Research.Depth != "medium" {
		t.Errorf("Research.Depth = %q", s.Research.Depth)
	}
	if len(s.Research.Sources) != 1 || s.Research.Sources[0] != "arxiv" {
		t.Errorf("Research.Sources = %v", s.Research.Sources)
	}
	if s.Research.CadencePerStage["custom"] != "always" {
		t.Errorf("CadencePerStage[custom] missing")
	}
	if s.Subprocess.PreWarmPoolSize != 9 {
		t.Errorf("PreWarmPoolSize = %d", s.Subprocess.PreWarmPoolSize)
	}
	if len(s.Reviewer.FamilyDisjointPool) != 2 || s.Reviewer.FamilyDisjointPool[0] != "x" {
		t.Errorf("FamilyDisjointPool = %v", s.Reviewer.FamilyDisjointPool)
	}
	if s.Reviewer.CriteriaDefault != "security" {
		t.Errorf("CriteriaDefault = %q", s.Reviewer.CriteriaDefault)
	}
	if s.Budget.AnomalyZThreshold != 2.0 {
		t.Errorf("AnomalyZThreshold = %f", s.Budget.AnomalyZThreshold)
	}
	if s.Budget.AnomalyWindowSize != 90 {
		t.Errorf("AnomalyWindowSize = %d", s.Budget.AnomalyWindowSize)
	}
	if s.Workforce.WritablePathsPolicy != "shared-readonly" {
		t.Errorf("WritablePathsPolicy = %q", s.Workforce.WritablePathsPolicy)
	}
	if s.Workforce.DoctrineReinforcementTemplatePointer != "templates/p.txt" {
		t.Errorf("DoctrineReinforcementTemplatePointer = %q",
			s.Workforce.DoctrineReinforcementTemplatePointer)
	}
	if s.Apply.MergeStrategy != "ours" {
		t.Errorf("Apply.MergeStrategy = %q", s.Apply.MergeStrategy)
	}
	if s.Apply.ConflictHandling != "auto-prefer-ours" {
		t.Errorf("Apply.ConflictHandling = %q", s.Apply.ConflictHandling)
	}
	if s.Watcher.CPUBudget != 0.5 {
		t.Errorf("Watcher.CPUBudget = %f", s.Watcher.CPUBudget)
	}
}

func TestResolveFromSettingsJSON(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"zenswarm":{"doctrine":"capa-firewall"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	doctrine, err := ReadSettingsDoctrine(settingsPath)
	if err != nil {
		t.Fatalf("ReadSettingsDoctrine: %v", err)
	}
	if doctrine != "capa-firewall" {
		t.Errorf("doctrine = %q, want capa-firewall", doctrine)
	}
}

func TestResolveFromSettingsJSONMissing(t *testing.T) {
	doctrine, err := ReadSettingsDoctrine("/nonexistent/settings.json")
	if err != nil {
		t.Errorf("ReadSettingsDoctrine(missing): %v, want nil", err)
	}
	if doctrine != "" {
		t.Errorf("doctrine = %q, want empty", doctrine)
	}
}

func TestResolveFromSettingsJSONMalformed(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadSettingsDoctrine(settingsPath)
	if err == nil {
		t.Error("ReadSettingsDoctrine(malformed) returned nil error")
	}
}

func TestResolveFromSettingsJSONNoZenswarmKey(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	doctrine, err := ReadSettingsDoctrine(settingsPath)
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if doctrine != "" {
		t.Errorf("doctrine = %q, want empty", doctrine)
	}
}

func TestResolveFromSettingsJSONReadError(t *testing.T) {
	dir := t.TempDir()

	_, err := ReadSettingsDoctrine(dir)
	if err == nil {
		t.Error("ReadSettingsDoctrine(dir) returned nil error")
	}
}

func TestBuildBuiltinProvenanceCoversAllSchemaFields(t *testing.T) {
	want := flattenSchemaTomlPaths(reflect.TypeOf(Schema{}), "")
	got := buildBuiltinProvenance("builtin:max-scope")
	for _, key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("buildBuiltinProvenance missing key %q (declared in Schema struct)", key)
		}
	}

	wantSet := map[string]bool{}
	for _, k := range want {
		wantSet[k] = true
	}
	for key := range got {
		if !wantSet[key] {
			t.Errorf("buildBuiltinProvenance has stale key %q (not declared in Schema)", key)
		}
	}
}

func flattenSchemaTomlPaths(t reflect.Type, prefix string) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("toml")
		if tag == "" {
			continue
		}

		if comma := strings.Index(tag, ","); comma >= 0 {
			tag = tag[:comma]
		}
		path := tag
		if prefix != "" {
			path = prefix + "." + tag
		}

		if path == "future" {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Struct {
			out = append(out, flattenSchemaTomlPaths(ft, path)...)
			continue
		}

		out = append(out, path)
	}
	return out
}

func TestProvenanceMergeOrdering(t *testing.T) {
	base := map[string]string{"foo": "A", "bar": "A"}
	top := map[string]string{"bar": "B", "baz": "B"}
	merged := mergeProvenance(base, top)
	if merged["foo"] != "A" {
		t.Errorf("foo = %q, want A", merged["foo"])
	}
	if merged["bar"] != "B" {
		t.Errorf("bar = %q, want B", merged["bar"])
	}
	if merged["baz"] != "B" {
		t.Errorf("baz = %q, want B", merged["baz"])
	}
}

func TestResolvedZeroValueSafe(t *testing.T) {
	var r Resolved
	if _, ok := r.Provenance["x"]; ok {
		t.Error("zero Resolved.Provenance reported ok=true")
	}
}

func TestClampMoneyEmptyValues(t *testing.T) {
	v := Money("")
	if clamped, mismatch := clampMoney(&v, Money("100.00 USD")); clamped || mismatch {
		t.Errorf("empty value: clamped=%v mismatch=%v, want both false", clamped, mismatch)
	}
	v2 := Money("50.00 USD")
	if clamped, mismatch := clampMoney(&v2, Money("")); clamped || mismatch {
		t.Errorf("empty ceiling: clamped=%v mismatch=%v, want both false", clamped, mismatch)
	}
	if v2 != "50.00 USD" {
		t.Errorf("value mutated: %q", v2)
	}
}

func TestClampMoneyMalformed(t *testing.T) {
	v := Money("garbage")
	if clamped, mismatch := clampMoney(&v, Money("100.00 USD")); clamped || mismatch {
		t.Errorf("malformed value: clamped=%v mismatch=%v, want both false", clamped, mismatch)
	}
	v2 := Money("50.00 USD")
	if clamped, mismatch := clampMoney(&v2, Money("garbage")); clamped || mismatch {
		t.Errorf("malformed ceiling: clamped=%v mismatch=%v, want both false", clamped, mismatch)
	}
}

func TestClampMoneyCurrencyMismatch(t *testing.T) {
	v := Money("9999.00 EUR")
	clamped, mismatch := clampMoney(&v, Money("100.00 USD"))
	if clamped {
		t.Error("currency mismatch must not clamp")
	}
	if !mismatch {
		t.Error("currency mismatch flag = false, want true")
	}
	if v != "9999.00 EUR" {
		t.Errorf("value mutated: %q", v)
	}
}

func TestClampMoneyHappyPath(t *testing.T) {
	v := Money("500.00 USD")
	clamped, mismatch := clampMoney(&v, Money("100.00 USD"))
	if !clamped {
		t.Error("clamped = false, want true")
	}
	if mismatch {
		t.Error("mismatch = true, want false (same currency)")
	}
	if v != "100.00 USD" {
		t.Errorf("value = %q, want clamped to ceiling", v)
	}
}

func TestClampMoneyEqualOrBelow(t *testing.T) {
	v := Money("50.00 USD")
	clamped, mismatch := clampMoney(&v, Money("100.00 USD"))
	if clamped || mismatch {
		t.Errorf("clamped=%v mismatch=%v, want both false", clamped, mismatch)
	}
}

func TestMergeStringMapEmpty(t *testing.T) {
	m := mergeStringMap(nil, nil)
	if len(m) != 0 {
		t.Errorf("len(merge nil nil) = %d", len(m))
	}
	m2 := mergeStringMap(map[string]string{"a": "A"}, nil)
	if m2["a"] != "A" {
		t.Errorf("mergeStringMap base lost: %v", m2)
	}
	m3 := mergeStringMap(nil, map[string]string{"a": "A"})
	if m3["a"] != "A" {
		t.Errorf("mergeStringMap top lost: %v", m3)
	}
}

func TestMergeMoneyMapEmpty(t *testing.T) {
	m := mergeMoneyMap(nil, nil)
	if len(m) != 0 {
		t.Errorf("len(merge nil nil) = %d", len(m))
	}
	m2 := mergeMoneyMap(map[string]Money{"a": "1 USD"}, map[string]Money{"a": "2 USD", "b": "3 USD"})
	if m2["a"] != "2 USD" {
		t.Errorf("top should win for collision; got %q", m2["a"])
	}
	if m2["b"] != "3 USD" {
		t.Errorf("merge missing 'b': %v", m2)
	}
}

func TestOverlayPureBaseEmpty(t *testing.T) {
	base := MaxScopeBuiltin()
	out := overlay(base, Schema{}, map[string]string{})
	if out.Name != base.Name {
		t.Errorf("Name changed: %q vs %q", out.Name, base.Name)
	}
	if out.Research.Depth != base.Research.Depth {
		t.Errorf("Research.Depth changed: %q vs %q", out.Research.Depth, base.Research.Depth)
	}
}

func TestOverlayInitsNilFuture(t *testing.T) {
	base := Schema{}
	top := Schema{
		Future: map[string]map[string]any{
			"plan_x": {"foo": "bar"},
		},
	}
	out := overlay(base, top, map[string]string{})
	if out.Future == nil {
		t.Fatal("overlay did not init Future")
	}
	if out.Future["plan_x"]["foo"] != "bar" {
		t.Errorf("Future[plan_x][foo] = %v", out.Future["plan_x"]["foo"])
	}
}

func TestApplyClampMarkersDoesNotMutateInput(t *testing.T) {
	input := map[string]string{
		"budget.caps.project":  "/some/zenswarm.toml",
		"budget.caps.doctrine": "/some/zenswarm.toml",
		"research.depth":       "/some/zenswarm.toml",
	}

	want := map[string]string{
		"budget.caps.project":  "/some/zenswarm.toml",
		"budget.caps.doctrine": "/some/zenswarm.toml",
		"research.depth":       "/some/zenswarm.toml",
	}
	clamps := map[string]bool{
		"budget.caps.project": true,
	}
	out := applyClampMarkers(input, clamps)

	for k, v := range want {
		if got := input[k]; got != v {
			t.Errorf("input[%q] mutated: got %q, want %q", k, got, v)
		}
	}

	if got := out["budget.caps.project"]; got != "clamped:/some/zenswarm.toml" {
		t.Errorf("out[budget.caps.project] = %q, want clamp prefix", got)
	}

	if got := out["research.depth"]; got != "/some/zenswarm.toml" {
		t.Errorf("out[research.depth] = %q, want pass-through", got)
	}

	if reflect.ValueOf(out).Pointer() == reflect.ValueOf(input).Pointer() {
		t.Error("applyClampMarkers returned the same map header (alias, not copy)")
	}
}

func TestResolveDoesNotMutateLoaderProvenance(t *testing.T) {
	projectPath := writeTempTOML(t, `
schema_version = 1
name = "max-scope"
[budget.caps]
project = "9999.00 USD"
doctrine = "9999.00 USD"
`)

	preLoaded, err := LoadFile(projectPath)
	if err != nil {
		t.Fatalf("LoadFile pre: %v", err)
	}
	preCopy := make(map[string]string, len(preLoaded.Provenance))
	for k, v := range preLoaded.Provenance {
		preCopy[k] = v
	}

	r := Resolver{
		ChosenDoctrine: "max-scope",
		ProjectPath:    projectPath,
	}
	if _, err := r.Resolve(); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	postLoaded, err := LoadFile(projectPath)
	if err != nil {
		t.Fatalf("LoadFile post: %v", err)
	}
	if !reflect.DeepEqual(preCopy, postLoaded.Provenance) {
		t.Errorf("Provenance differs after Resolve:\npre  = %#v\npost = %#v", preCopy, postLoaded.Provenance)
	}
	for k, v := range postLoaded.Provenance {
		if strings.HasPrefix(v, "clamped:") {
			t.Errorf("post-Resolve LoadFile saw clamp marker leak at %q = %q", k, v)
		}
	}
}

func TestResolveGatewayDisabledToolsOverlay(t *testing.T) {
	projectPath := writeTempTOML(t, `
schema_version = 1
name = "max-scope"
[gateway]
disabled_tools = ["mcp_zen-swarm_research_agentic"]
`)
	r := Resolver{
		ChosenDoctrine: "max-scope",
		ProjectPath:    projectPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []string{"mcp_zen-swarm_research_agentic"}
	if !reflect.DeepEqual(res.Schema.Gateway.DisabledTools, want) {
		t.Errorf("Gateway.DisabledTools = %v, want %v",
			res.Schema.Gateway.DisabledTools, want)
	}
	if res.Provenance["gateway.disabled_tools"] != projectPath {
		t.Errorf("Provenance[gateway.disabled_tools] = %q, want %q",
			res.Provenance["gateway.disabled_tools"], projectPath)
	}
}

func TestResolveGatewayDisabledToolsNotOverridden(t *testing.T) {
	projectPath := writeTempTOML(t, `
schema_version = 1
name = "max-scope"
[research]
agentic_max_iter = 2
`)
	r := Resolver{
		ChosenDoctrine: "max-scope",
		ProjectPath:    projectPath,
	}
	res, err := r.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res.Schema.Gateway.DisabledTools) != 0 {
		t.Errorf("max-scope without project [gateway] should have empty DisabledTools; got %v",
			res.Schema.Gateway.DisabledTools)
	}
	if src := res.Provenance["gateway.disabled_tools"]; !strings.HasPrefix(src, "builtin:") {
		t.Errorf("Provenance[gateway.disabled_tools] = %q, want builtin:* (no overlay)", src)
	}
}
