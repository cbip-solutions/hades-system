package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reinforcement"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func bootRealRegistry(t *testing.T) map[string]*v1.Schema {
	t.Helper()
	all, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("builtin.LoadAll: %v", err)
	}
	active.SetRegistry(all)
	t.Cleanup(active.ResetForTest)
	return all
}

type recordingEventlog struct {
	mu     sync.Mutex
	events []any
}

func (r *recordingEventlog) Emit(_ context.Context, evt any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, evt)
	return nil
}

type recordingActive struct {
	mu             sync.Mutex
	userDefaultCnt int
	forProjectCnt  int
	clearCnt       int
}

func (r *recordingActive) SetForProject(_ string, _ *v1.Schema) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forProjectCnt++
}
func (r *recordingActive) SetUserDefault(_ *v1.Schema) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userDefaultCnt++
}
func (r *recordingActive) ClearForProject(_ string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearCnt++
}

type passThroughParser struct{}

func (passThroughParser) ParseStrict(_ []byte, _ string, target *v1.Schema, _ parser.ParseOpts) error {
	target.SchemaVersion = "1.0"
	target.DoctrineVersion = "1.0.0"
	target.AutoUpgrade = "patch"

	target.Transverse = v1.TransverseConfig{
		NoTechDebt:        true,
		NoStubs:           true,
		BuildFinalProduct: true,
		NoDefer:           true,
	}
	target.Workforce = v1.WorkforceConfig{
		MinDepth: 1, MaxDepth: 4, MaxWidthPerLayer: 8,
	}
	return nil
}

func installRealWatcher(t *testing.T, srv *Server) (*reload.Watcher, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")

	if err := writeStub(path, "schema_version = \"1.0\"\n"); err != nil {
		t.Fatalf("writeStub: %v", err)
	}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &recordingEventlog{},
		ActiveAccessor: &recordingActive{},
		Parser:         passThroughParser{},
	})
	if err != nil {
		t.Fatalf("reload.New: %v", err)
	}
	if err := w.AddPath(path, ""); err != nil {
		t.Fatalf("AddPath: %v", err)
	}
	t.Cleanup(w.Close)
	srv.SetReloadWatcherForTest(w)
	return w, path
}

func writeStub(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}

func TestServer_DoctrineSetters(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if srv.reloadWatcher != nil {
		t.Fatalf("zero-value reloadWatcher should be nil")
	}
	if srv.reinforceEngine != nil {
		t.Fatalf("zero-value reinforceEngine should be nil")
	}
	if srv.pendingChangesProvider != nil {
		t.Fatalf("zero-value pendingChangesProvider should be nil")
	}

	w := newPlainWatcher(t)
	defer w.Close()
	srv.SetReloadWatcher(w)
	if srv.reloadWatcher != w {
		t.Errorf("SetReloadWatcher: not stored")
	}

	w2 := newPlainWatcher(t)
	defer w2.Close()
	srv.SetReloadWatcherForTest(w2)
	if srv.reloadWatcher != w2 {
		t.Errorf("SetReloadWatcherForTest: not stored")
	}

	eng := reinforcement.New("")
	srv.SetDoctrineReinforceEngine(eng)
	if srv.reinforceEngine != eng {
		t.Errorf("SetDoctrineReinforceEngine: not stored")
	}

	srv.SetDoctrinePendingChangesProvider(func() []string { return []string{"prop-1"} })
	if srv.pendingChangesProvider == nil {
		t.Errorf("SetDoctrinePendingChangesProvider: provider not stored")
	}
	if got := srv.pendingChangesProvider(); len(got) != 1 || got[0] != "prop-1" {
		t.Errorf("provider returned %v, want [prop-1]", got)
	}
}

func newPlainWatcher(t *testing.T) *reload.Watcher {
	t.Helper()
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &recordingEventlog{},
		ActiveAccessor: &recordingActive{},
		Parser:         passThroughParser{},
	})
	if err != nil {
		t.Fatalf("reload.New: %v", err)
	}
	return w
}

func TestServer_DoctrineActive_HappyPath(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	name, sch, source, err := srv.DoctrineActive("")
	if err != nil {
		t.Fatalf("DoctrineActive: %v", err)
	}
	if sch == nil {
		t.Fatalf("schema nil")
	}
	if name == "" {
		t.Errorf("name empty (registry-resolved)")
	}
	if source != "embed" {
		t.Errorf("source = %q, want embed", source)
	}
	if sch.SchemaVersion != "1.0" {
		t.Errorf("schema_version = %q", sch.SchemaVersion)
	}
}

func TestServer_DoctrineActive_PerProject(t *testing.T) {
	all := bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	cf := all["capa-firewall"]
	if cf == nil {
		t.Fatalf("registry missing capa-firewall")
	}
	active.SetForProject("proj-A", cf)

	name, sch, source, err := srv.DoctrineActive("proj-A")
	if err != nil {
		t.Fatalf("DoctrineActive: %v", err)
	}
	if source != "project" {
		t.Errorf("source = %q, want project", source)
	}
	if sch != cf {
		t.Errorf("schema pointer mismatch (per-project resolution failed)")
	}
	if name != "capa-firewall" {
		t.Errorf("name = %q, want capa-firewall", name)
	}
}

func TestServer_DoctrineActive_RegistryEmpty(t *testing.T) {
	// IMPORTANT do not call bootRealRegistry — we want the singleton in
	// its zero-value state for THIS test only. ResetForTest cleans up.
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)

	st := newTestStore(t)
	srv := New(st, Config{})

	_, _, _, err := srv.DoctrineActive("")
	if !errors.Is(err, doctrineerrors.ErrDoctrineNotFound) {
		t.Errorf("err = %v, want ErrDoctrineNotFound", err)
	}
}

func TestServer_DoctrineList(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	rows, err := srv.DoctrineList("")
	if err != nil {
		t.Fatalf("DoctrineList: %v", err)
	}
	if len(rows) < 3 {
		t.Fatalf("got %d rows, want ≥3 (max-scope, default, capa-firewall)", len(rows))
	}

	gotNames := []string{rows[0].Name, rows[1].Name, rows[2].Name}
	want := []string{"capa-firewall", "default", "max-scope"}
	for i := range want {
		if gotNames[i] != want[i] {
			t.Errorf("rows[%d].Name = %q, want %q", i, gotNames[i], want[i])
		}
	}
	for _, r := range rows {
		if r.Source != "embed" {
			t.Errorf("row %q source=%q, want embed", r.Name, r.Source)
		}
	}
}

func TestServer_DoctrineList_FilterUserOrProject(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	for _, f := range []string{"user", "project"} {
		rows, err := srv.DoctrineList(f)
		if err != nil {
			t.Fatalf("DoctrineList(%q): %v", f, err)
		}
		if len(rows) != 0 {
			t.Errorf("DoctrineList(%q) returned %d rows, want 0 (v0.5.0 doesn't populate %s source)", f, len(rows), f)
		}
	}
}

func TestServer_DoctrineShow_TomlFormat(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	format, body, err := srv.DoctrineShow("max-scope", "toml", "")
	if err != nil {
		t.Fatalf("DoctrineShow: %v", err)
	}
	if format != "toml" {
		t.Errorf("format = %q, want toml", format)
	}
	if !strings.Contains(body, "schema_version") {
		t.Errorf("toml body missing schema_version: %s", body)
	}
	if !strings.Contains(body, "doctrine_version") {
		t.Errorf("toml body missing doctrine_version: %s", body)
	}
}

func TestServer_DoctrineShow_DefaultsToTomlWhenFormatEmpty(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	format, _, err := srv.DoctrineShow("max-scope", "", "")
	if err != nil {
		t.Fatalf("DoctrineShow: %v", err)
	}
	if format != "toml" {
		t.Errorf("default format = %q, want toml", format)
	}
}

func TestServer_DoctrineShow_JsonFormat(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	format, body, err := srv.DoctrineShow("default", "json", "")
	if err != nil {
		t.Fatalf("DoctrineShow: %v", err)
	}
	if format != "json" {
		t.Errorf("format = %q, want json", format)
	}

	if !strings.Contains(body, `"SchemaVersion"`) {
		t.Errorf("json body missing SchemaVersion field: %s", body)
	}
}

func TestServer_DoctrineShow_MdFormatNoEngine(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	format, body, err := srv.DoctrineShow("max-scope", "md", "")
	if err != nil {
		t.Fatalf("DoctrineShow: %v", err)
	}
	if format != "md" {
		t.Errorf("format = %q, want md", format)
	}
	if !strings.Contains(body, "max-scope") {
		t.Errorf("md fallback body missing doctrine name: %s", body)
	}
}

func TestServer_DoctrineShow_MdFormatWithEngine(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})
	srv.SetDoctrineReinforceEngine(reinforcement.New(""))

	_, body, err := srv.DoctrineShow("max-scope", "md", "")
	if err != nil {
		t.Fatalf("DoctrineShow: %v", err)
	}
	if body == "" {
		t.Errorf("rendered md body empty")
	}
}

func TestServer_DoctrineShow_NotFound(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	_, _, err := srv.DoctrineShow("bogus", "toml", "")
	if !errors.Is(err, doctrineerrors.ErrDoctrineNotFound) {
		t.Errorf("err = %v, want ErrDoctrineNotFound wrap", err)
	}
}

func TestServer_DoctrineShow_DefaultBranchUnknownFormat(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	format, body, err := srv.DoctrineShow("max-scope", "yaml", "")
	if err != nil {
		t.Fatalf("DoctrineShow: %v", err)
	}
	if format != "toml" {
		t.Errorf("unknown-format fallback = %q, want toml", format)
	}
	if body == "" {
		t.Errorf("body empty after fallback")
	}
}

func TestServer_DoctrineShow_SectionFilter(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	_, body, err := srv.DoctrineShow("max-scope", "toml", "workforce")
	if err != nil {
		t.Fatalf("DoctrineShow: %v", err)
	}
	if !strings.Contains(body, "[workforce]") {
		t.Errorf("section body missing [workforce]: %s", body)
	}
}

func TestServer_DoctrineShow_SectionFilterMissingHeader(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	_, body, err := srv.DoctrineShow("max-scope", "toml", "nonexistent_section")
	if err != nil {
		t.Fatalf("DoctrineShow: %v", err)
	}

	if !strings.Contains(body, "schema_version") {
		t.Errorf("missing-section fallback body should contain full TOML; got: %.200s", body)
	}
}

func TestServer_DoctrineShow_SectionFilterIgnoredOnNonToml(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	_, body, err := srv.DoctrineShow("max-scope", "json", "anything")
	if err != nil {
		t.Fatalf("DoctrineShow: %v", err)
	}
	if !strings.Contains(body, "SchemaVersion") {
		t.Errorf("json body unexpectedly mutated by section filter: %s", body)
	}
}

func TestServer_DoctrineValidate_GoodSchema(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	tomlContent := minimalValidTOML()
	if err := srv.DoctrineValidate(tomlContent, ""); err != nil {
		t.Errorf("expected nil for valid TOML; got: %v", err)
	}
}

func TestServer_DoctrineValidate_ParseFail(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	bad := minimalValidTOML() + "\nunknown_top_level_key = \"x\"\n"
	err := srv.DoctrineValidate(bad, "")
	if err == nil {
		t.Fatalf("want ErrParseFailed wrap")
	}
	if !errors.Is(err, doctrineerrors.ErrParseFailed) {
		t.Errorf("err = %v, want ErrParseFailed wrap", err)
	}
}

func TestServer_DoctrineValidate_ValidateFail(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	bad := `schema_version = "1.0"
doctrine_version = "1.0.0"
auto_upgrade = "patch"
`
	err := srv.DoctrineValidate(bad, "")
	if err == nil {
		t.Fatalf("want ErrValidationFailed wrap")
	}
	if !errors.Is(err, doctrineerrors.ErrValidationFailed) {
		t.Errorf("err = %v, want ErrValidationFailed wrap", err)
	}
}

func TestServer_DoctrineValidate_TightenFail(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	all := mustLoadAll(t)
	active.SetForProject("proj-loosen", all["max-scope"])

	loose := minimalValidTOML()
	err := srv.DoctrineValidate(loose, "proj-loosen")
	if err == nil {
		t.Fatalf("want ErrTightenViolation wrap on loosen attempt vs max-scope baseline")
	}
	if !errors.Is(err, doctrineerrors.ErrTightenViolation) {
		t.Errorf("err = %v, want ErrTightenViolation wrap", err)
	}
}

func TestServer_DoctrineValidate_AgainstBaselineFallback(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	err := srv.DoctrineValidate(minimalValidTOML(), "no-such-project")
	if !errors.Is(err, doctrineerrors.ErrTightenViolation) {
		t.Errorf("err = %v, want ErrTightenViolation (last-resort fallback to max-scope)", err)
	}
}

func TestServer_DoctrineValidate_AgainstBaselineRegistryUnset(t *testing.T) {
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)

	st := newTestStore(t)
	srv := New(st, Config{})

	err := srv.DoctrineValidate(minimalValidTOML(), "any-name")
	if err != nil {
		t.Errorf("expected nil (registry unset → tighten skipped); got %v", err)
	}
}

func TestServer_DoctrineStatus(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	snap, err := srv.DoctrineStatus("")
	if err != nil {
		t.Fatalf("DoctrineStatus: %v", err)
	}
	if snap.Active.Name == "" {
		t.Errorf("active name empty")
	}
	if snap.WatcherHealthy {
		t.Errorf("WatcherHealthy = true; want false (no watcher wired)")
	}
	if snap.PendingChanges == nil {
		t.Errorf("PendingChanges should be non-nil (empty slice)")
	}
	if len(snap.PendingChanges) != 0 {
		t.Errorf("PendingChanges len = %d, want 0", len(snap.PendingChanges))
	}
}

func TestServer_DoctrineStatus_WithWatcherAndProvider(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})
	w := newPlainWatcher(t)
	defer w.Close()
	srv.SetReloadWatcher(w)
	srv.SetDoctrinePendingChangesProvider(func() []string { return []string{"prop-A", "prop-B"} })

	snap, err := srv.DoctrineStatus("")
	if err != nil {
		t.Fatalf("DoctrineStatus: %v", err)
	}
	if !snap.WatcherHealthy {
		t.Errorf("WatcherHealthy = false; want true (watcher wired)")
	}
	if got := len(snap.PendingChanges); got != 2 {
		t.Errorf("PendingChanges len = %d, want 2", got)
	}
}

func TestServer_DoctrineStatus_RegistryEmptyReturnsErr(t *testing.T) {
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)

	st := newTestStore(t)
	srv := New(st, Config{})

	_, err := srv.DoctrineStatus("")
	if !errors.Is(err, doctrineerrors.ErrDoctrineNotFound) {
		t.Errorf("err = %v, want ErrDoctrineNotFound", err)
	}
}

func TestServer_DoctrineHistory_Empty(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	rows, err := srv.DoctrineHistory(time.Now().Add(-24*time.Hour), "any-filter", 100)
	if err != nil {
		t.Fatalf("DoctrineHistory: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("rows len = %d, want 0 (v0.5.0 stub)", len(rows))
	}
}

func TestServer_DoctrineDiff_HappyPath(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	from, to, diffs, err := srv.DoctrineDiff("max-scope", "default", "")
	if err != nil {
		t.Fatalf("DoctrineDiff: %v", err)
	}
	if from != "max-scope" {
		t.Errorf("from = %q, want max-scope", from)
	}
	if to != "default" {
		t.Errorf("to = %q, want default", to)
	}
	if len(diffs) == 0 {
		t.Errorf("diffs empty; max-scope and default should differ on at least one knob")
	}

	for _, d := range diffs {
		switch d.Status {
		case "changed", "added", "removed":
		default:
			t.Errorf("diff entry %q has invalid status %q", d.Path, d.Status)
		}
	}
}

func TestServer_DoctrineDiff_NoChange(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	_, _, diffs, err := srv.DoctrineDiff("max-scope", "max-scope", "")
	if err != nil {
		t.Fatalf("DoctrineDiff: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("same-vs-same diff len = %d, want 0; got: %v", len(diffs), diffs)
	}
}

func TestServer_DoctrineDiff_SectionFilter(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	_, _, diffs, err := srv.DoctrineDiff("max-scope", "default", "workforce")
	if err != nil {
		t.Fatalf("DoctrineDiff: %v", err)
	}
	for _, d := range diffs {
		if !strings.HasPrefix(d.Path, "workforce.") && d.Path != "workforce" {
			t.Errorf("section-filtered diff includes out-of-section path %q", d.Path)
		}
	}
}

func TestServer_DoctrineDiff_NotFoundA(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	_, _, _, err := srv.DoctrineDiff("bogus", "default", "")
	if !errors.Is(err, doctrineerrors.ErrDoctrineNotFound) {
		t.Errorf("err = %v, want ErrDoctrineNotFound", err)
	}
}

func TestServer_DoctrineDiff_NotFoundB(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	_, _, _, err := srv.DoctrineDiff("max-scope", "bogus", "")
	if !errors.Is(err, doctrineerrors.ErrDoctrineNotFound) {
		t.Errorf("err = %v, want ErrDoctrineNotFound", err)
	}
}

// TestServer_DoctrineMigrate_HappyPath identity-passthrough on V1.0.
// The current accessor parses the input then re-emits TOML; target version
// is the constant currentSchemaVersion="1.0".
//
// NOTE(plan-15) on Plan-J FIX-PASS scope: a future task should wire
// MigrateChain into DoctrineMigrate so downgrade-rejection
// flows through the HTTP API. The current accessor is identity-passthrough;
// we do NOT add a downgrade-rejection test against the present
// implementation because that would be testing speculative future
// behaviour. See server_doctrine.go:505-509 godoc for the deferral note.
func TestServer_DoctrineMigrate_HappyPath(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	in := minimalValidTOML()
	to, body, warnings, err := srv.DoctrineMigrate(in, "1.0")
	if err != nil {
		t.Fatalf("DoctrineMigrate: %v", err)
	}
	if to != "1.0" {
		t.Errorf("to_schema_version = %q, want 1.0", to)
	}
	if !strings.Contains(body, "schema_version") {
		t.Errorf("body missing schema_version: %s", body)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want empty slice (identity migration)", warnings)
	}
}

func TestServer_DoctrineMigrate_ParseFail(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	bad := minimalValidTOML() + "\nstray = key = value\n"
	_, _, _, err := srv.DoctrineMigrate(bad, "1.0")
	if err == nil {
		t.Fatalf("want parse error")
	}
	if !errors.Is(err, doctrineerrors.ErrParseFailed) {
		t.Errorf("err = %v, want ErrParseFailed wrap", err)
	}
}

func TestServer_DoctrineReinforce_HappyPath(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})
	srv.SetDoctrineReinforceEngine(reinforcement.New(""))

	out, err := srv.DoctrineReinforce(client.DoctrineV2ReinforceReq{
		TaskKind:     "worker",
		ProjectAlias: "zen-swarm",
		Stage:        "Build",
		Phase:        "J-fix",
		PlanID:       "plan-8",
	})
	if err != nil {
		t.Fatalf("DoctrineReinforce: %v", err)
	}
	if out == "" {
		t.Errorf("rendered output empty")
	}
}

func TestServer_DoctrineReinforce_PerProjectBranch(t *testing.T) {
	all := bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})
	srv.SetDoctrineReinforceEngine(reinforcement.New(""))
	active.SetForProject("proj-rein", all["default"])

	out, err := srv.DoctrineReinforce(client.DoctrineV2ReinforceReq{
		TaskKind:     "worker",
		ProjectAlias: "proj-rein",
	})
	if err != nil {
		t.Fatalf("DoctrineReinforce: %v", err)
	}
	if out == "" {
		t.Errorf("rendered output empty")
	}
}

func TestServer_DoctrineReinforce_NoEngineWired(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})

	_, err := srv.DoctrineReinforce(client.DoctrineV2ReinforceReq{TaskKind: "worker"})
	if err == nil || !strings.Contains(err.Error(), "reinforce engine not wired") {
		t.Errorf("err = %v, want 'reinforce engine not wired'", err)
	}
}

func TestServer_DoctrineReinforce_RegistryEmpty(t *testing.T) {
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)

	st := newTestStore(t)
	srv := New(st, Config{})
	srv.SetDoctrineReinforceEngine(reinforcement.New(""))

	_, err := srv.DoctrineReinforce(client.DoctrineV2ReinforceReq{TaskKind: "worker"})
	if !errors.Is(err, doctrineerrors.ErrDoctrineNotFound) {
		t.Errorf("err = %v, want ErrDoctrineNotFound", err)
	}
}

func TestServer_DoctrineReinforce_BadTemplateName(t *testing.T) {
	bootRealRegistry(t)
	st := newTestStore(t)
	srv := New(st, Config{})
	srv.SetDoctrineReinforceEngine(reinforcement.New(""))

	rogue := &v1.Schema{SchemaVersion: "9.9", DoctrineVersion: "9.9.9"}
	active.SetForProject("proj-rogue", rogue)

	out, err := srv.DoctrineReinforce(client.DoctrineV2ReinforceReq{
		TaskKind:     "worker",
		ProjectAlias: "proj-rogue",
	})
	if err != nil {

		t.Errorf("DoctrineReinforce with rogue schema: %v", err)
	}
	if out == "" {
		t.Errorf("rendered output empty after name-fallback to max-scope")
	}
}

func TestServer_DoctrineReload_NoWatcher(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	err := srv.DoctrineReload("/some/path")
	if err == nil {
		t.Fatalf("want 'no active reload.Watcher' err")
	}
	if !strings.Contains(err.Error(), "no active reload.Watcher") {
		t.Errorf("err = %v, want 'no active reload.Watcher'", err)
	}
}

func TestServer_DoctrineReload_NotifyForceUnregisteredPath(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})
	w := newPlainWatcher(t)
	defer w.Close()
	srv.SetReloadWatcherForTest(w)

	err := srv.DoctrineReload("/nonexistent/path.toml")
	if err == nil {
		t.Fatalf("want NotifyForce path-not-registered err")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Errorf("err = %v, want 'not registered'", err)
	}
}

func TestServer_DoctrineReload_HappyPathWithRegisteredPath(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})
	_, path := installRealWatcher(t, srv)

	if err := srv.DoctrineReload(path); err != nil {
		t.Fatalf("DoctrineReload(%q): %v", path, err)
	}
}

func TestServer_DoctrineReloadEvents_NoWatcher(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if ch := srv.DoctrineReloadEvents(); ch != nil {
		t.Errorf("DoctrineReloadEvents nil-watcher returned non-nil channel")
	}
	if ch := srv.DoctrineReloadFailedEvents(); ch != nil {
		t.Errorf("DoctrineReloadFailedEvents nil-watcher returned non-nil channel")
	}
}

func TestServer_DoctrineReloadEvents_WithWatcher(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})
	w := newPlainWatcher(t)
	defer w.Close()
	srv.SetReloadWatcherForTest(w)

	successCh := srv.DoctrineReloadEvents()
	if successCh == nil {
		t.Fatalf("DoctrineReloadEvents returned nil with watcher wired")
	}
	failureCh := srv.DoctrineReloadFailedEvents()
	if failureCh == nil {
		t.Fatalf("DoctrineReloadFailedEvents returned nil with watcher wired")
	}

	srv.DoctrineUnsubscribeReloadEvents(successCh)
	srv.DoctrineUnsubscribeReloadEvents(successCh)
	srv.DoctrineUnsubscribeReloadFailedEvents(failureCh)
	srv.DoctrineUnsubscribeReloadFailedEvents(failureCh)
}

func TestServer_DoctrineUnsubscribe_NoWatcher(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	srv.DoctrineUnsubscribeReloadEvents(nil)
	srv.DoctrineUnsubscribeReloadFailedEvents(nil)
}

func TestServer_DoctrineReloadTimeout(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if got := srv.DoctrineReloadTimeout(); got != 5*time.Second {
		t.Errorf("DoctrineReloadTimeout = %s, want 5s", got)
	}
}

func minimalValidTOML() string {
	raw, ok := builtin.Bytes("default")
	if !ok {
		panic("server_doctrine_test: builtin.Bytes(default) returned !ok")
	}
	return stripDoctrineTransverse(string(raw))
}

func stripDoctrineTransverse(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if !skipping && trimmed == "[doctrine_transverse]" {
			skipping = true
			continue
		}
		if skipping {

			if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") &&
				!strings.HasPrefix(trimmed, "[doctrine_transverse") {
				skipping = false
				out = append(out, l)
				continue
			}

			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func mustLoadAll(t *testing.T) map[string]*v1.Schema {
	t.Helper()
	all, err := builtin.LoadAll()
	if err != nil {
		t.Fatalf("builtin.LoadAll: %v", err)
	}
	return all
}

func TestServerSatisfiesDoctrineHandlerCtx(t *testing.T) {
	var _ handlers.DoctrineHandlerCtx = (*Server)(nil)
}
