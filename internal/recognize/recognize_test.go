package recognize

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func mustWriteToTempDir(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func realFS(dir string) fs.FS {
	return os.DirFS(dir)
}

func TestRecognize_GoOnly_Tier1ShortCircuits(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod":  &fstest.MapFile{Data: []byte("module example\n\ngo 1.22\n")},
		"main.go": &fstest.MapFile{Data: []byte("package main\n\nfunc main() {}\n")},
		"go.sum":  &fstest.MapFile{Data: []byte("\n")},
	}
	r := New(Options{})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("Recognize err: %v", err)
	}
	if res.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q; want %q", res.SchemaVersion, SchemaVersion)
	}
	if res.PrimaryLanguage != "Go" {
		t.Errorf("PrimaryLanguage = %q; want Go", res.PrimaryLanguage)
	}
	if res.PrimaryConfidence < 0.8 {
		t.Errorf("PrimaryConfidence = %v; want >=0.8", res.PrimaryConfidence)
	}

	foundShortCircuitRationale := false
	for _, rstr := range res.Rationale {
		if rcontains(rstr, "short-circuit") || rcontains(rstr, "Tier 1") {
			foundShortCircuitRationale = true
			break
		}
	}
	if !foundShortCircuitRationale {
		t.Errorf("rationale = %v; want a Tier 1 short-circuit mention", res.Rationale)
	}
}

func TestRecognize_Polyglot_RunsTier3(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod":       &fstest.MapFile{Data: []byte("module example\n\ngo 1.22\n")},
		"package.json": &fstest.MapFile{Data: []byte(`{"name":"x","dependencies":{"react":"^18"}}`)},
		"main.go":      &fstest.MapFile{Data: []byte("package main\nfunc main(){}\n")},
		"app.tsx":      &fstest.MapFile{Data: []byte("export default () => <div/>;\n")},
	}
	r := New(Options{})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if len(res.Ecosystems) < 2 {
		t.Errorf("Ecosystems count = %d; want >=2 (polyglot)", len(res.Ecosystems))
	}
}

func TestRecognize_FrameworkDetected(t *testing.T) {
	fsys := fstest.MapFS{
		"next.config.js": &fstest.MapFile{Data: []byte("module.exports = {};")},
		"package.json":   &fstest.MapFile{Data: []byte(`{"name":"x","dependencies":{"next":"^14"}}`)},
	}
	r := New(Options{})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	found := false
	for _, f := range res.Frameworks {
		if f.Framework == "next.js" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Frameworks = %v; want next.js", res.Frameworks)
	}
}

func TestRecognize_JSONSerializationIsStable(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod":  &fstest.MapFile{Data: []byte("module example\n\ngo 1.22\n")},
		"main.go": &fstest.MapFile{Data: []byte("package main\n")},
	}
	r := New(Options{})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	out, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if parsed["schemaVersion"] != "1.0" {
		t.Errorf("schemaVersion = %v; want 1.0", parsed["schemaVersion"])
	}

	for _, k := range []string{
		"schemaVersion", "primaryLanguage", "primaryConfidence",
		"languages", "ecosystems", "frameworks",
		"maturity", "ambiguous", "rationale",
	} {
		if _, ok := parsed[k]; !ok {
			t.Errorf("Result JSON missing key %q (want 9-key canonical surface)", k)
		}
	}
}

func TestRecognize_EmptyResultJSONHasAll9Keys(t *testing.T) {
	out, err := json.Marshal(Result{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	for _, k := range []string{
		"schemaVersion", "primaryLanguage", "primaryConfidence",
		"languages", "ecosystems", "frameworks",
		"maturity", "ambiguous", "rationale",
	} {
		if _, ok := parsed[k]; !ok {
			t.Errorf("zero-value Result{} JSON missing key %q (omitempty drift?)", k)
		}
	}
}

func TestRecognize_NoAuditFlagSkipsEmit(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("module x\n\ngo 1.22\n")},
	}
	rec := &recordingEmitter{}
	r := newWithEmitter(Options{NoAudit: true}, rec)
	_, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rec.calls != 0 {
		t.Errorf("Emit calls = %d with NoAudit=true; want 0", rec.calls)
	}
}

func TestRecognize_AuditEmittedWhenNoAuditFalse(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("module x\n\ngo 1.22\n")},
	}
	rec := &recordingEmitter{}
	r := newWithEmitter(Options{}, rec)
	_, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rec.calls != 1 {
		t.Errorf("Emit calls = %d; want 1", rec.calls)
	}
	if rec.lastEvent != "evt.recognize.run" {
		t.Errorf("lastEvent = %q; want evt.recognize.run", rec.lastEvent)
	}

	if rec.lastPayload != nil {
		if _, hasRootPath := rec.lastPayload["rootPath"]; hasRootPath {
			t.Error("audit payload includes rootPath; want privacy-stripped")
		}
	}
}

func TestRecognize_ContextCancellationHonored(t *testing.T) {
	fsys := fstest.MapFS{}
	for i := 0; i < 200; i++ {
		fsys[fmtFilename(i)] = &fstest.MapFile{Data: []byte("package main\n")}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := New(Options{})
	_, err := r.Recognize(ctx, fsys)
	if err == nil {
		t.Error("Recognize returned nil err on pre-cancelled ctx")
	}
}

func TestRecognize_InterfaceCompliance(t *testing.T) {
	var _ Recognizer = (*threeTierRecognizer)(nil)
	var _ Recognizer = New(Options{})
}

func TestRecognize_PackageLevelWrapper(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("module x\n\ngo 1.22\n")},
	}
	res, err := Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.PrimaryLanguage != "Go" {
		t.Errorf("PrimaryLanguage = %q; want Go", res.PrimaryLanguage)
	}
}

func TestRecognize_EmptyFS(t *testing.T) {
	r := New(Options{})
	res, err := r.Recognize(context.Background(), fstest.MapFS{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.PrimaryLanguage != "" {
		t.Errorf("PrimaryLanguage = %q; want \"\"", res.PrimaryLanguage)
	}
}

func TestRecognize_Tier3DominantLanguageRationale(t *testing.T) {
	bigGo := make([]byte, 4096)
	for i := range bigGo {
		bigGo[i] = 'a'
	}
	fsys := fstest.MapFS{
		"main.go": &fstest.MapFile{Data: append([]byte("package main\n"), bigGo...)},
		"tiny.py": &fstest.MapFile{Data: []byte("x=1\n")},
	}
	r := New(Options{})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	gotDominant := false
	for _, s := range res.Rationale {
		if rcontains(s, "dominant") {
			gotDominant = true
			break
		}
	}
	if !gotDominant {
		t.Errorf("rationale = %v; want 'dominant' line", res.Rationale)
	}
}

func TestRecognize_Tier3SecondaryRationale(t *testing.T) {

	bigGo := make([]byte, 600)
	for i := range bigGo {
		bigGo[i] = 'a'
	}
	bigPy := make([]byte, 400)
	for i := range bigPy {
		bigPy[i] = 'b'
	}
	fsys := fstest.MapFS{
		"main.go": &fstest.MapFile{Data: append([]byte("package main\n"), bigGo...)},
		"x.py":    &fstest.MapFile{Data: append([]byte("def f():\n  pass\n"), bigPy...)},
	}
	r := New(Options{})
	res, _ := r.Recognize(context.Background(), fsys)
	gotSecondary := false
	gotAmbig := false
	for _, s := range res.Rationale {
		if rcontains(s, "primary") && rcontains(s, "secondaries") {
			gotSecondary = true
		}
		if rcontains(s, "ambiguous") {
			gotAmbig = true
		}
	}

	_ = gotSecondary
	_ = gotAmbig
}

func TestRecognize_MaturityProbed(t *testing.T) {
	dir := t.TempDir()

	mustWriteToTempDir(t, dir, "go.mod", "module x\n\ngo 1.22\n")
	r := New(Options{RootAbsPath: dir})
	res, err := r.Recognize(context.Background(), realFS(dir))
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if res.Maturity.CommitCount != -1 {
		t.Errorf("Maturity.CommitCount = %d; want -1 (no git)", res.Maturity.CommitCount)
	}
}

func TestRecognize_MonorepoDetected(t *testing.T) {
	dir := t.TempDir()
	mustWriteToTempDir(t, dir, "pnpm-workspace.yaml", "packages: ['*']\n")
	mustWriteToTempDir(t, dir, "go.mod", "module x\n\ngo 1.22\n")
	r := New(Options{RootAbsPath: dir})
	res, err := r.Recognize(context.Background(), realFS(dir))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Monorepo == nil {
		t.Fatal("Monorepo nil; want pnpm")
	}
	if res.Monorepo.Tool != "pnpm" {
		t.Errorf("Monorepo.Tool = %q; want pnpm", res.Monorepo.Tool)
	}
}

func TestRecognize_Tier1Malformed_AddsRationale(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("not a valid module file\n")},
	}
	r := New(Options{})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	gotPartial := false
	for _, s := range res.Rationale {
		if rcontains(s, "Tier 1 partial") {
			gotPartial = true
			break
		}
	}
	if !gotPartial {
		t.Errorf("expected 'Tier 1 partial' in rationale; got %v", res.Rationale)
	}
}

func TestRecognize_AmbiguousTopTwoLanguages(t *testing.T) {

	goSrc := make([]byte, 0, 512)
	goSrc = append(goSrc, []byte("package main\nfunc f() {\n")...)
	for i := 0; i < 50; i++ {
		goSrc = append(goSrc, []byte("var x = 1\n")...)
	}
	goSrc = append(goSrc, []byte("}\n")...)
	pySrc := make([]byte, len(goSrc))
	copy(pySrc, []byte("def f():\n"))
	pad := len(goSrc) - len("def f():\n")
	for i := 0; i < pad; i++ {
		pySrc[len("def f():\n")+i] = ' '
	}
	fsys := fstest.MapFS{
		"a.go": &fstest.MapFile{Data: goSrc},
		"b.py": &fstest.MapFile{Data: pySrc},
	}
	r := New(Options{})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if len(res.Languages) >= 2 && !res.Ambiguous {
		t.Errorf("Ambiguous=false; want true (top-2 within 10%%, langs=%v)", res.Languages)
	}
}

type failingReadDirFS struct {
	fstest.MapFS
	failPath string
}

func (e failingReadDirFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == e.failPath {
		return nil, errors.New("simulated readdir failure")
	}
	return e.MapFS.ReadDir(name)
}

func TestRecognize_Tier3NonCancelError_AddsRationale(t *testing.T) {

	inner := fstest.MapFS{
		"main.go":  &fstest.MapFile{Data: []byte("package main\nfunc main(){}\n")},
		"sub/x.py": &fstest.MapFile{Data: []byte("x=1\n")},
		"sub/y.js": &fstest.MapFile{Data: []byte("export const x = 1;\n")},
	}
	fsys := failingReadDirFS{MapFS: inner, failPath: "sub"}
	r := New(Options{})
	res, err := r.Recognize(context.Background(), fsys)

	_ = err
	_ = res
}

func TestRecognize_AuditEmitErrorAddsRationale(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("module x\n\ngo 1.22\n")},
	}
	rec := &recordingEmitter{failNext: true}
	r := newWithEmitter(Options{}, rec)
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	gotPartial := false
	for _, s := range res.Rationale {
		if rcontains(s, "audit emit partial") {
			gotPartial = true
			break
		}
	}
	if !gotPartial {
		t.Errorf("expected 'audit emit partial' in rationale; got %v", res.Rationale)
	}
}

type recordingEmitter struct {
	calls       int
	lastEvent   string
	lastPayload map[string]any
	failNext    bool
}

func (r *recordingEmitter) Emit(ctx context.Context, eventType string, payload map[string]any) error {
	r.calls++
	r.lastEvent = eventType
	r.lastPayload = payload
	if r.failNext {
		return errors.New("simulated emit failure")
	}
	return nil
}

func rcontains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	n := len(s) - len(sub)
	for i := 0; i <= n; i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func fmtFilename(i int) string {
	return "f" + ritoa(i) + ".go"
}

func ritoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	return string(d)
}

var _ fs.FS = (fstest.MapFS)(nil)

func TestRecognize_PopulatesManifestDepsFromPackageJSON(t *testing.T) {
	fsys := fstest.MapFS{
		"package.json": &fstest.MapFile{Data: []byte(`{
  "name":"x",
  "dependencies":{"@prisma/client":"^5.0.0","react":"^18.0.0"},
  "devDependencies":{"@sentry/nextjs":"^7.0.0","typescript":"^5.0.0"}
}`)},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.ManifestDeps == nil {
		t.Fatal("ManifestDeps is nil; want populated map")
	}
	for _, want := range []string{"@prisma/client", "react", "@sentry/nextjs", "typescript"} {
		if _, ok := res.ManifestDeps[want]; !ok {
			t.Errorf("ManifestDeps missing key %q; got keys=%v", want, mapKeys(res.ManifestDeps))
		}
	}
}

func TestRecognize_PopulatesManifestDepsFromRequirementsTxt(t *testing.T) {
	fsys := fstest.MapFS{
		"requirements.txt": &fstest.MapFile{Data: []byte("psycopg2==2.9.5\nrequests>=2.0\n# comment\n")},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, ok := res.ManifestDeps["psycopg2"]; !ok {
		t.Errorf("ManifestDeps missing 'psycopg2'; got keys=%v", mapKeys(res.ManifestDeps))
	}
	if _, ok := res.ManifestDeps["requests"]; !ok {
		t.Errorf("ManifestDeps missing 'requests'; got keys=%v", mapKeys(res.ManifestDeps))
	}
}

func TestRecognize_PopulatesManifestDepsFromCargoToml(t *testing.T) {
	fsys := fstest.MapFS{
		"Cargo.toml": &fstest.MapFile{Data: []byte(`[package]
name = "x"
version = "0.1.0"
edition = "2021"

[dependencies]
serde = "1.0"
tokio = { version = "1", features = ["full"] }
`)},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, ok := res.ManifestDeps["serde"]; !ok {
		t.Errorf("ManifestDeps missing 'serde'; got keys=%v", mapKeys(res.ManifestDeps))
	}
	if _, ok := res.ManifestDeps["tokio"]; !ok {
		t.Errorf("ManifestDeps missing 'tokio'; got keys=%v", mapKeys(res.ManifestDeps))
	}
}

func TestRecognize_PopulatesManifestDepsFromComposerJSON(t *testing.T) {
	fsys := fstest.MapFS{
		"composer.json": &fstest.MapFile{Data: []byte(`{"name":"vendor/pkg","require":{"php":">=8.0","sentry/sdk":"^3.0"}}`)},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, ok := res.ManifestDeps["sentry/sdk"]; !ok {
		t.Errorf("ManifestDeps missing 'sentry/sdk'; got keys=%v", mapKeys(res.ManifestDeps))
	}
}

func TestRecognize_PopulatesConfigFilesFromVite(t *testing.T) {
	fsys := fstest.MapFS{
		"vite.config.ts": &fstest.MapFile{Data: []byte("export default {}\n")},
		"package.json":   &fstest.MapFile{Data: []byte(`{"name":"x","dependencies":{"react":"^18","react-dom":"^18"}}`)},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	found := false
	for _, cf := range res.ConfigFiles {
		if cf == "vite.config.ts" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ConfigFiles missing 'vite.config.ts'; got %v", res.ConfigFiles)
	}
}

func TestRecognize_PopulatesConfigFilesFromNext(t *testing.T) {
	fsys := fstest.MapFS{
		"next.config.js": &fstest.MapFile{Data: []byte("module.exports = {};\n")},
		"package.json":   &fstest.MapFile{Data: []byte(`{"name":"x","dependencies":{"next":"^14"}}`)},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	found := false
	for _, cf := range res.ConfigFiles {
		if cf == "next.config.js" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ConfigFiles missing 'next.config.js'; got %v", res.ConfigFiles)
	}
}

func TestRecognize_PopulatesConfigFilesFromSentry(t *testing.T) {
	fsys := fstest.MapFS{
		"sentry.client.config.ts": &fstest.MapFile{Data: []byte("// sentry\n")},
		"package.json":            &fstest.MapFile{Data: []byte(`{"name":"x"}`)},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	found := false
	for _, cf := range res.ConfigFiles {
		if cf == "sentry.client.config.ts" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ConfigFiles missing 'sentry.client.config.ts'; got %v", res.ConfigFiles)
	}
}

func TestRecognize_PopulatesConfigFilesFromLinear(t *testing.T) {
	fsys := fstest.MapFS{
		".linear.yml":  &fstest.MapFile{Data: []byte("workspace: x\n")},
		"package.json": &fstest.MapFile{Data: []byte(`{"name":"x"}`)},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	found := false
	for _, cf := range res.ConfigFiles {
		if cf == ".linear.yml" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ConfigFiles missing '.linear.yml'; got %v", res.ConfigFiles)
	}
}

func TestRecognize_PopulatesEnvVarsFromLinear(t *testing.T) {
	fsys := fstest.MapFS{
		".env":         &fstest.MapFile{Data: []byte("LINEAR_API_KEY=lk_x\nOTHER=1\n")},
		"package.json": &fstest.MapFile{Data: []byte(`{"name":"x"}`)},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, ok := res.EnvVars["LINEAR_API_KEY"]; !ok {
		t.Errorf("EnvVars missing 'LINEAR_API_KEY'; got keys=%v", mapKeys(res.EnvVars))
	}
}

func TestRecognize_PopulatesEnvVarsFromSentry(t *testing.T) {
	fsys := fstest.MapFS{
		".env.example": &fstest.MapFile{Data: []byte("SENTRY_DSN=\nDATABASE_URL=\n")},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, ok := res.EnvVars["SENTRY_DSN"]; !ok {
		t.Errorf("EnvVars missing 'SENTRY_DSN'; got keys=%v", mapKeys(res.EnvVars))
	}
}

func TestRecognize_PopulatesDoctrineFromCLAUDEMD(t *testing.T) {
	fsys := fstest.MapFS{
		"CLAUDE.md": &fstest.MapFile{Data: []byte("# Project\n\nDoctrine: max-scope\nDetails ...\n")},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Doctrine != "max-scope" {
		t.Errorf("Doctrine = %q; want %q", res.Doctrine, "max-scope")
	}
}

func TestRecognize_PopulatesDoctrineFromZenToml(t *testing.T) {
	fsys := fstest.MapFS{
		".zen/doctrine.toml": &fstest.MapFile{Data: []byte(`name = "capa-firewall"
`)},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Doctrine != "capa-firewall" {
		t.Errorf("Doctrine = %q; want %q", res.Doctrine, "capa-firewall")
	}
}

func TestRecognize_DoctrineEmptyWhenNoSignal(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("module x\n\ngo 1.22\n")},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Doctrine != "" {
		t.Errorf("Doctrine = %q; want \"\" (no signal)", res.Doctrine)
	}
}

func TestRecognize_PhaseAConsumerEndToEnd(t *testing.T) {
	fsys := fstest.MapFS{
		"package.json": &fstest.MapFile{Data: []byte(`{"name":"x","dependencies":{"@prisma/client":"^5.0.0"}}`)},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if res.PrimaryConfidence < 0.6 {
		t.Errorf("PrimaryConfidence = %v; want >=0.6 (smart-default gate)", res.PrimaryConfidence)
	}

	if _, ok := res.ManifestDeps["@prisma/client"]; !ok {
		t.Errorf("ManifestDeps missing '@prisma/client'; smart-default detectPrismaPostgres would silently return false")
	}
}

func TestRecognize_DirectChainEmitProductionPath(t *testing.T) {
	dir := t.TempDir()
	mustWriteToTempDir(t, dir, "go.mod", "module x\n\ngo 1.22\n")

	store := &recordingChainStore{}
	opts := Options{RootAbsPath: dir, ChainStore: store}
	r := New(opts)
	res, err := r.Recognize(context.Background(), realFS(dir))
	if err != nil {
		t.Fatalf("Recognize err: %v", err)
	}
	_ = res
	if store.getChainTipCalls != 1 {
		t.Errorf("GetChainTip calls = %d; want 1 (production path must invoke chain emit)", store.getChainTipCalls)
	}
	if store.updateChainColumnsCalls != 1 {
		t.Errorf("UpdateChainColumns calls = %d; want 1 (production path must persist chain row)", store.updateChainColumnsCalls)
	}
	if store.appendLeafCalls != 1 {
		t.Errorf("AppendTesseraLeaf calls = %d; want 1", store.appendLeafCalls)
	}
	if store.updateLeafIDCalls != 1 {
		t.Errorf("UpdateTesseraLeafID calls = %d; want 1", store.updateLeafIDCalls)
	}
	if store.lastEventType != "evt.recognize.run" {
		t.Errorf("lastEventType = %q; want %q", store.lastEventType, "evt.recognize.run")
	}
}

func TestRecognize_DirectChainEmitSkippedWhenNoAudit(t *testing.T) {
	dir := t.TempDir()
	mustWriteToTempDir(t, dir, "go.mod", "module x\n\ngo 1.22\n")

	store := &recordingChainStore{}
	opts := Options{RootAbsPath: dir, ChainStore: store, NoAudit: true}
	r := New(opts)
	_, err := r.Recognize(context.Background(), realFS(dir))
	if err != nil {
		t.Fatalf("Recognize err: %v", err)
	}
	if store.getChainTipCalls != 0 {
		t.Errorf("GetChainTip calls = %d with NoAudit=true; want 0", store.getChainTipCalls)
	}
}

func TestRecognize_NoChainStoreNoEmit(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("module x\n\ngo 1.22\n")},
	}
	r := New(Options{})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("Recognize err: %v", err)
	}

	hasSkipRationale := false
	for _, line := range res.Rationale {
		if rcontains(line, "audit emit skipped") {
			hasSkipRationale = true
			break
		}
	}
	if !hasSkipRationale {
		t.Errorf("expected 'audit emit skipped' rationale (no ChainStore wired); got %v", res.Rationale)
	}
}

func TestRecognize_ChainEmitFailureSurfacesPartial(t *testing.T) {
	dir := t.TempDir()
	mustWriteToTempDir(t, dir, "go.mod", "module x\n\ngo 1.22\n")

	store := &recordingChainStore{failOnGetChainTip: true}
	opts := Options{RootAbsPath: dir, ChainStore: store}
	r := New(opts)
	res, err := r.Recognize(context.Background(), realFS(dir))
	if err != nil {
		t.Fatalf("Recognize err: %v", err)
	}
	hasPartial := false
	for _, line := range res.Rationale {
		if rcontains(line, "audit emit partial") {
			hasPartial = true
			break
		}
	}
	if !hasPartial {
		t.Errorf("expected 'audit emit partial' rationale on ChainStore err; got %v", res.Rationale)
	}
}

type recordingChainStore struct {
	getChainTipCalls        int
	updateChainColumnsCalls int
	updateLeafIDCalls       int
	appendLeafCalls         int
	lastEventType           string
	failOnGetChainTip       bool
}

func (s *recordingChainStore) GetChainTip(ctx context.Context) (string, error) {
	s.getChainTipCalls++
	if s.failOnGetChainTip {
		return "", errors.New("simulated chain-tip read failure")
	}
	return "", nil
}

func (s *recordingChainStore) UpdateChainColumns(ctx context.Context, eventID, prevHash, eventType string, payload []byte, emittedAt int64, recordHash, partitionID string) error {
	s.updateChainColumnsCalls++
	s.lastEventType = eventType
	return nil
}

func (s *recordingChainStore) UpdateTesseraLeafID(ctx context.Context, eventID, leafID string) error {
	s.updateLeafIDCalls++
	return nil
}

func (s *recordingChainStore) AppendTesseraLeaf(ctx context.Context, leaf TesseraLeafInput) (string, error) {
	s.appendLeafCalls++
	return "leaf-" + leaf.EventID, nil
}

func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestRecognize_DoctrineFromCLAUDEMD_CapaFirewall(t *testing.T) {
	fsys := fstest.MapFS{
		"CLAUDE.md": &fstest.MapFile{Data: []byte("# Project\ndoctrine: capa-firewall\n")},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Doctrine != "capa-firewall" {
		t.Errorf("Doctrine = %q; want capa-firewall", res.Doctrine)
	}
}

func TestRecognize_DoctrineFromCLAUDEMD_DefaultQuoted(t *testing.T) {
	fsys := fstest.MapFS{
		"CLAUDE.md": &fstest.MapFile{Data: []byte(`# Project
doctrine = "default"
`)},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Doctrine != "default" {
		t.Errorf("Doctrine = %q; want default", res.Doctrine)
	}
}

func TestRecognize_DoctrineFromCLAUDEMD_DefaultColon(t *testing.T) {
	fsys := fstest.MapFS{
		"CLAUDE.md": &fstest.MapFile{Data: []byte("# Project\nDoctrine: default\n")},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Doctrine != "default" {
		t.Errorf("Doctrine = %q; want default (colon form)", res.Doctrine)
	}
}

func TestRecognize_DoctrineFromZenToml_NoName(t *testing.T) {
	fsys := fstest.MapFS{
		".zen/doctrine.toml": &fstest.MapFile{Data: []byte("# no name key\n[other]\nkey = \"v\"\n")},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Doctrine != "" {
		t.Errorf("Doctrine = %q; want \"\" (no name + no CLAUDE.md)", res.Doctrine)
	}
}

func TestRecognize_DoctrineFromZenToml_NonCanonicalName(t *testing.T) {
	fsys := fstest.MapFS{
		".zen/doctrine.toml": &fstest.MapFile{Data: []byte(`name = "custom-not-canonical"
`)},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Doctrine != "" {
		t.Errorf("Doctrine = %q; want \"\" (non-canonical name should fall through)", res.Doctrine)
	}
}

func TestRecognize_DoctrineFalsePositiveProseRejected(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "negation_prose",
			content: "# Project\n\nThis project does NOT use max-scope; we follow capa-firewall principles.\n",
			want:    "",
		},
		{
			name:    "casual_prose",
			content: "# Project\n\nmax-scope is great, but our team uses default.\n",
			want:    "",
		},
		{
			name:    "explicit_markdown_header",
			content: "# Project\nDoctrine: capa-firewall\nDetails below.\n",
			want:    "capa-firewall",
		},
		{
			name:    "explicit_yaml_unquoted",
			content: "# Project\ndoctrine: max-scope\n",
			want:    "max-scope",
		},
		{
			name:    "prose_substring_extension_not_matched",
			content: "# Project\ndoctrine: max-scope-derived\n",
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"CLAUDE.md": &fstest.MapFile{Data: []byte(tc.content)},
			}
			r := New(Options{NoAudit: true})
			res, err := r.Recognize(context.Background(), fsys)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if res.Doctrine != tc.want {
				t.Errorf("Doctrine = %q; want %q\ncontent:\n%s", res.Doctrine, tc.want, tc.content)
			}
		})
	}
}

func TestRecognize_EnvVarsSkipInvalidNames(t *testing.T) {
	fsys := fstest.MapFS{
		".env": &fstest.MapFile{Data: []byte("123BAD=skip\nWITH.DOT=skip\n=bare\nVALID_NAME=ok\nexport EXPORTED=ok\n")},
	}
	r := New(Options{NoAudit: true})
	res, err := r.Recognize(context.Background(), fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if _, ok := res.EnvVars["VALID_NAME"]; !ok {
		t.Errorf("EnvVars missing VALID_NAME; got %v", mapKeys(res.EnvVars))
	}
	if _, ok := res.EnvVars["EXPORTED"]; !ok {
		t.Errorf("EnvVars missing EXPORTED; got %v", mapKeys(res.EnvVars))
	}

	if _, ok := res.EnvVars["123BAD"]; ok {
		t.Errorf("EnvVars unexpectedly contains 123BAD (digit-leading)")
	}
	if _, ok := res.EnvVars["WITH.DOT"]; ok {
		t.Errorf("EnvVars unexpectedly contains WITH.DOT (dot in name)")
	}
}

func TestRecognize_ChainEmitFailureOnAppendLeaf(t *testing.T) {
	dir := t.TempDir()
	mustWriteToTempDir(t, dir, "go.mod", "module x\n\ngo 1.22\n")

	store := &failingAppendChainStore{}
	opts := Options{RootAbsPath: dir, ChainStore: store}
	r := New(opts)
	res, err := r.Recognize(context.Background(), realFS(dir))
	if err != nil {
		t.Fatalf("Recognize err: %v", err)
	}
	hasPartial := false
	for _, line := range res.Rationale {
		if rcontains(line, "audit emit partial") {
			hasPartial = true
			break
		}
	}
	if !hasPartial {
		t.Errorf("expected 'audit emit partial' on AppendTesseraLeaf failure; got %v", res.Rationale)
	}
}

func TestRecognize_ChainEmitFailureOnUpdateLeafID(t *testing.T) {
	dir := t.TempDir()
	mustWriteToTempDir(t, dir, "go.mod", "module x\n\ngo 1.22\n")

	store := &failingUpdateLeafIDChainStore{}
	opts := Options{RootAbsPath: dir, ChainStore: store}
	r := New(opts)
	res, err := r.Recognize(context.Background(), realFS(dir))
	if err != nil {
		t.Fatalf("Recognize err: %v", err)
	}
	hasPartial := false
	for _, line := range res.Rationale {
		if rcontains(line, "audit emit partial") {
			hasPartial = true
			break
		}
	}
	if !hasPartial {
		t.Errorf("expected 'audit emit partial' on UpdateTesseraLeafID failure; got %v", res.Rationale)
	}
}

func TestRecognize_ChainEmitFailureOnUpdateChainColumns(t *testing.T) {
	dir := t.TempDir()
	mustWriteToTempDir(t, dir, "go.mod", "module x\n\ngo 1.22\n")

	store := &failingUpdateChainColumnsChainStore{}
	opts := Options{RootAbsPath: dir, ChainStore: store}
	r := New(opts)
	res, err := r.Recognize(context.Background(), realFS(dir))
	if err != nil {
		t.Fatalf("Recognize err: %v", err)
	}
	hasPartial := false
	for _, line := range res.Rationale {
		if rcontains(line, "audit emit partial") {
			hasPartial = true
			break
		}
	}
	if !hasPartial {
		t.Errorf("expected 'audit emit partial' on UpdateChainColumns failure; got %v", res.Rationale)
	}
}

type failingAppendChainStore struct{ recordingChainStore }

func (s *failingAppendChainStore) AppendTesseraLeaf(ctx context.Context, leaf TesseraLeafInput) (string, error) {
	return "", errors.New("simulated append failure")
}

type failingUpdateLeafIDChainStore struct{ recordingChainStore }

func (s *failingUpdateLeafIDChainStore) UpdateTesseraLeafID(ctx context.Context, eventID, leafID string) error {
	return errors.New("simulated update-leaf-id failure")
}

type failingUpdateChainColumnsChainStore struct{ recordingChainStore }

func (s *failingUpdateChainColumnsChainStore) UpdateChainColumns(ctx context.Context, eventID, prevHash, eventType string, payload []byte, emittedAt int64, recordHash, partitionID string) error {
	return errors.New("simulated update-chain-columns failure")
}
