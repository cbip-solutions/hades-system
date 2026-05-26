package ecosystem_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func TestVersionDetector_Layer1_ExplicitFlag(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Version: "1.22",
		Query:   "how do I use crypto/sha256",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.22" {
		t.Errorf("want version=1.22 got %q", version)
	}
	if layer != 1 {
		t.Errorf("want layer=1 got %d", layer)
	}
}

func TestVersionDetector_Layer1_EmptyStringNotLayer1(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	_, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:     "generic channel usage",
		Ecosystem: ecosystem.EcoGo,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layer == 1 {
		t.Error("want layer != 1 (empty version must not trigger Layer 1)")
	}
}

func TestVersionDetector_Layer1_TakesPrecedenceOverLayer2(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n\ngo 1.21\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Version:     "1.23",
		Query:       "slog package",
		Ecosystem:   ecosystem.EcoGo,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.23" {
		t.Errorf("want version=1.23 got %q", version)
	}
	if layer != 1 {
		t.Errorf("want layer=1 got %d", layer)
	}
}

func TestVersionDetector_Layer1_TakesPrecedenceOverLayer3(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Version:   "1.22",
		Query:     "go 1.19 context deadline",
		Ecosystem: ecosystem.EcoGo,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.22" {
		t.Errorf("want version=1.22 got %q", version)
	}
	if layer != 1 {
		t.Errorf("want layer=1 got %d", layer)
	}
}

func TestVersionDetector_Layer2_GoMod(t *testing.T) {
	dir := t.TempDir()

	gomod := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(gomod, []byte("module example.com/foo\n\ngo 1.21\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "context package usage",
		Ecosystem:   ecosystem.EcoGo,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.21" {
		t.Errorf("want version=1.21 got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}

func TestVersionDetector_Layer2_GoMod_MajorMinorPatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.21.5\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "structured logging",
		Ecosystem:   ecosystem.EcoGo,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.21.5" {
		t.Errorf("want version=1.21.5 got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}

func TestVersionDetector_Layer2_GoMod_Missing(t *testing.T) {
	dir := t.TempDir()

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	_, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "channels usage",
		Ecosystem:   ecosystem.EcoGo,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if layer == 2 {
		t.Errorf("want layer != 2 (no go.mod) got layer=2")
	}
}

func TestVersionDetector_Layer2_PyprojectTOML(t *testing.T) {
	dir := t.TempDir()
	content := `[project]
name = "myapp"
requires-python = ">=3.11"
`
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "asyncio usage",
		Ecosystem:   ecosystem.EcoPython,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "3.11" {
		t.Errorf("want version=3.11 got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}

func TestVersionDetector_Layer2_PyprojectTOML_PoetrySection(t *testing.T) {
	dir := t.TempDir()
	content := `[tool.poetry]
name = "mypoetryapp"

[tool.poetry.dependencies]
python = "^3.9"
`
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "dataclass usage",
		Ecosystem:   ecosystem.EcoPython,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "3.9" {
		t.Errorf("want version=3.9 got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}

func TestVersionDetector_Layer2_SetupCfg(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "setup.cfg"), []byte("[metadata]\npython_requires=>=3.10\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "multiprocessing",
		Ecosystem:   ecosystem.EcoPython,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "3.10" {
		t.Errorf("want version=3.10 got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}

func TestVersionDetector_Layer2_Pyproject_Beats_SetupCfg(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]\nrequires-python = \">=3.12\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "setup.cfg"), []byte("[metadata]\npython_requires=>=3.9\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "typing",
		Ecosystem:   ecosystem.EcoPython,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "3.12" {
		t.Errorf("want version=3.12 (pyproject wins) got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}

func TestVersionDetector_Layer2_PackageJSON_EnginesNode(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"name":"myapp","engines":{"node":">=20.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "fetch API usage",
		Ecosystem:   ecosystem.EcoTypeScript,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "20.0.0" {
		t.Errorf("want version=20.0.0 got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}

func TestVersionDetector_Layer2_PackageJSON_AtTypesNode(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"name":"myapp","devDependencies":{"@types/node":"^18.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "EventEmitter usage",
		Ecosystem:   ecosystem.EcoTypeScript,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "18.0.0" {
		t.Errorf("want version=18.0.0 got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}

func TestVersionDetector_Layer2_PackageJSON_EnginesBeatsAtTypes(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"name":"myapp","engines":{"node":">=22.0.0"},"devDependencies":{"@types/node":"^18.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "streams",
		Ecosystem:   ecosystem.EcoTypeScript,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "22.0.0" {
		t.Errorf("want version=22.0.0 (engines.node wins) got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}

func TestVersionDetector_Layer2_CargoToml_RustVersion(t *testing.T) {
	dir := t.TempDir()
	cargoToml := `[package]
name = "mypkg"
version = "0.1.0"
edition = "2021"
rust-version = "1.77"
`
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargoToml), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "async trait usage",
		Ecosystem:   ecosystem.EcoRust,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.77" {
		t.Errorf("want version=1.77 got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}

func TestVersionDetector_Layer2_CargoToml_EditionFallback(t *testing.T) {
	dir := t.TempDir()

	cargoToml := `[package]
name = "mypkg"
version = "0.1.0"
edition = "2021"
`
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargoToml), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "ownership rules",
		Ecosystem:   ecosystem.EcoRust,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.56" {
		t.Errorf("want version=1.56 (edition 2021 MSRV) got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}

func TestVersionDetector_Layer2_CargoToml_Edition2024(t *testing.T) {
	dir := t.TempDir()
	cargoToml := `[package]
name = "mypkg"
version = "0.2.0"
edition = "2024"
`
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargoToml), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "async closures",
		Ecosystem:   ecosystem.EcoRust,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.85" {
		t.Errorf("want version=1.85 (edition 2024 MSRV) got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}

func TestVersionDetector_Layer2_CargoToml_Edition2018(t *testing.T) {
	dir := t.TempDir()
	cargoToml := `[package]
name = "mypkg"
version = "0.0.1"
edition = "2018"
`
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargoToml), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "Result handling",
		Ecosystem:   ecosystem.EcoRust,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.31" {
		t.Errorf("want version=1.31 (edition 2018 MSRV) got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}

func TestVersionDetector_Layer2_UnknownEcosystem_FallsThrough(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n\ngo 1.21\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "generic usage",
		Ecosystem:   ecosystem.EcoRust,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if layer != 5 {
		t.Errorf("want layer=5 (Rust no Cargo.toml) got layer=%d (version=%q)", layer, version)
	}
	if version != "latest_stable" {
		t.Errorf("want latest_stable got %q", version)
	}
}

func TestVersionDetector_Layer2_UnrecognizedEcosystem_DefaultBranch(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n\ngo 1.21\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "class usage",
		Ecosystem:   ecosystem.Ecosystem("java"),
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layer != 5 {
		t.Errorf("want layer=5 (unrecognized ecosystem default branch) got layer=%d version=%q", layer, version)
	}
	if version != "latest_stable" {
		t.Errorf("want latest_stable got %q", version)
	}
}

func TestVersionDetector_Layer3_InQueryRegex(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	cases := []struct {
		query     string
		wantVer   string
		ecosystem ecosystem.Ecosystem
	}{
		{"how to use generics in go 1.21", "1.21", ecosystem.EcoGo},
		{"python 3.12 match statement", "3.12", ecosystem.EcoPython},
		{"node 20.5.1 fetch API", "20.5.1", ecosystem.EcoTypeScript},
		{"react 18.2 concurrent mode", "18.2", ecosystem.EcoTypeScript},
		{"rust 1.75 async traits", "1.75", ecosystem.EcoRust},
		{"using go 1.22.0 toolchain", "1.22.0", ecosystem.EcoGo},
		{"python 3.13.1 type narrowing", "3.13.1", ecosystem.EcoPython},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
				Query:     tc.query,
				Ecosystem: tc.ecosystem,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if version != tc.wantVer {
				t.Errorf("want version=%q got %q", tc.wantVer, version)
			}
			if layer != 3 {
				t.Errorf("want layer=3 got %d", layer)
			}
		})
	}
}

func TestVersionDetector_Layer3_NoMatchCascades(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	_, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:     "how do I use channels",
		Ecosystem: ecosystem.EcoGo,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layer == 3 {
		t.Error("want layer!=3 (no version in query) got layer=3")
	}
}

type mockHaikuClassifier struct {
	version    string
	confidence float64
	err        error
	callCount  int
}

func (m *mockHaikuClassifier) Classify(_ context.Context, _ string, _ ecosystem.Ecosystem) (string, float64, error) {
	m.callCount++
	return m.version, m.confidence, m.err
}

func TestVersionDetector_Layer4_Skipped(t *testing.T) {
	mock := &mockHaikuClassifier{version: "1.21", confidence: 0.99}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
		HaikuClassifier:  mock,
	})
	ctx := context.Background()

	_, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:     "how do I use the range over int feature",
		Ecosystem: ecosystem.EcoGo,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if layer == 4 {
		t.Error("want layer!=4 (LLM skipped) got layer=4")
	}

	if mock.callCount != 0 {
		t.Errorf("want HaikuClassifier.Classify not called; got %d calls", mock.callCount)
	}
}

func TestVersionDetector_Layer4_InvokedWhenEnabled(t *testing.T) {
	mock := &mockHaikuClassifier{version: "1.22", confidence: 0.85}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: false,
		HaikuClassifier:  mock,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:     "range over function values introduction",
		Ecosystem: ecosystem.EcoGo,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layer != 4 {
		t.Errorf("want layer=4 got %d (version=%q)", layer, version)
	}
	if version != "1.22" {
		t.Errorf("want version=1.22 got %q", version)
	}
	if mock.callCount != 1 {
		t.Errorf("want exactly 1 HaikuClassifier call; got %d", mock.callCount)
	}
}

func TestVersionDetector_Layer4_LowConfidenceFallsToLayer5(t *testing.T) {

	mock := &mockHaikuClassifier{version: "1.21", confidence: 0.7}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: false,
		HaikuClassifier:  mock,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:     "goroutine lifecycle",
		Ecosystem: ecosystem.EcoGo,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if layer != 5 {
		t.Errorf("want layer=5 (confidence 0.7 not > threshold) got layer=%d (version=%q)", layer, version)
	}
	if version != "latest_stable" {
		t.Errorf("want latest_stable got %q", version)
	}
}

func TestVersionDetector_Layer4_AboveThresholdAccepted(t *testing.T) {
	mock := &mockHaikuClassifier{version: "3.12", confidence: 0.71}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: false,
		HaikuClassifier:  mock,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:     "walrus operator advanced usage",
		Ecosystem: ecosystem.EcoPython,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layer != 4 {
		t.Errorf("want layer=4 got %d (version=%q)", layer, version)
	}
	if version != "3.12" {
		t.Errorf("want version=3.12 got %q", version)
	}
}

func TestVersionDetector_Layer4_EmptyVersionFallsThrough(t *testing.T) {

	mock := &mockHaikuClassifier{version: "", confidence: 0.95}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: false,
		HaikuClassifier:  mock,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:     "memory layout optimization",
		Ecosystem: ecosystem.EcoRust,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layer != 5 {
		t.Errorf("want layer=5 (empty version from Haiku) got layer=%d (version=%q)", layer, version)
	}
	if version != "latest_stable" {
		t.Errorf("want latest_stable got %q", version)
	}
}

func TestVersionDetector_Layer4_HaikuErrorFallsThrough(t *testing.T) {
	mock := &mockHaikuClassifier{err: errors.New("classifier unavailable")}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: false,
		HaikuClassifier:  mock,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:     "generic constraints",
		Ecosystem: ecosystem.EcoGo,
	})
	if err != nil {
		t.Fatalf("unexpected error (Layer 4 errors must be absorbed): %v", err)
	}
	if layer != 5 {
		t.Errorf("want layer=5 (Haiku error absorbed) got layer=%d (version=%q)", layer, version)
	}
	if version != "latest_stable" {
		t.Errorf("want latest_stable got %q", version)
	}
}

func TestVersionDetector_Layer4_NilClassifier_GracefulSkip(t *testing.T) {

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: false,
		HaikuClassifier:  nil,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:     "interface embedding",
		Ecosystem: ecosystem.EcoGo,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layer != 5 {
		t.Errorf("want layer=5 (nil classifier graceful skip) got layer=%d (version=%q)", layer, version)
	}
	if version != "latest_stable" {
		t.Errorf("want latest_stable got %q", version)
	}
}

func TestVersionDetector_Layer5_Default(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:     "how do I use channels",
		Ecosystem: ecosystem.EcoGo,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "latest_stable" {
		t.Errorf("want version=latest_stable got %q", version)
	}
	if layer != 5 {
		t.Errorf("want layer=5 got %d", layer)
	}
}

func TestVersionDetector_EmptyRequest(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "latest_stable" {
		t.Errorf("want latest_stable got %q", version)
	}
	if layer != 5 {
		t.Errorf("want layer=5 got %d", layer)
	}
}

func TestVersionDetector_Layer5_Default_Python(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:     "decorators usage",
		Ecosystem: ecosystem.EcoPython,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "latest_stable" {
		t.Errorf("want latest_stable got %q", version)
	}
	if layer != 5 {
		t.Errorf("want layer=5 got %d", layer)
	}
}

func TestVersionDetector_Determinism(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n\ngo 1.21\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	req := ecosystem.QueryRequest{
		Query:       "slog package",
		Ecosystem:   ecosystem.EcoGo,
		ProjectPath: dir,
	}

	var firstVer string
	var firstLayer int
	for i := 0; i < 5; i++ {
		v, l, err := vd.Detect(ctx, req)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if i == 0 {
			firstVer, firstLayer = v, l
		} else {
			if v != firstVer || l != firstLayer {
				t.Errorf("iteration %d: want (%q,%d) got (%q,%d) — violates inv-zen-192", i, firstVer, firstLayer, v, l)
			}
		}
	}
}

func TestVersionDetector_Layer2_NoProjectPath_SkipsLayer2(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	_, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:     "goroutine leak detection",
		Ecosystem: ecosystem.EcoGo,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if layer != 5 {
		t.Errorf("want layer=5 (no ProjectPath) got layer=%d", layer)
	}
}

func TestVersionDetector_Layer2_GoMod_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission checks have no effect")
	}
	dir := t.TempDir()
	gomodPath := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(gomodPath, []byte("module example\n\ngo 1.21\n"), 0000); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(gomodPath, 0600) })

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	_, _, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "slog",
		Ecosystem:   ecosystem.EcoGo,
		ProjectPath: dir,
	})

	if err == nil {
		t.Error("want error for unreadable go.mod, got nil")
	}
}

func TestVersionDetector_Layer2_CargoToml_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission checks have no effect")
	}
	dir := t.TempDir()
	cargoPath := filepath.Join(dir, "Cargo.toml")
	if err := os.WriteFile(cargoPath, []byte("[package]\nname=\"x\"\nversion=\"0.1.0\"\nedition=\"2021\"\n"), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cargoPath, 0600) })

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	_, _, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "lifetime annotation",
		Ecosystem:   ecosystem.EcoRust,
		ProjectPath: dir,
	})
	if err == nil {
		t.Error("want error for unreadable Cargo.toml, got nil")
	}
}

func TestVersionDetector_Layer2_PackageJSON_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: permission checks have no effect")
	}
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "package.json")
	if err := os.WriteFile(pkgPath, []byte(`{"name":"x","engines":{"node":">=20"}}`), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(pkgPath, 0600) })

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	_, _, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "fetch API",
		Ecosystem:   ecosystem.EcoTypeScript,
		ProjectPath: dir,
	})
	if err == nil {
		t.Error("want error for unreadable package.json, got nil")
	}
}

func TestVersionDetector_Layer2_PackageJSON_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{not valid json"), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "streams",
		Ecosystem:   ecosystem.EcoTypeScript,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error for malformed JSON (should cascade): %v", err)
	}
	if layer != 5 {
		t.Errorf("want layer=5 (malformed JSON) got layer=%d version=%q", layer, version)
	}
}

func TestVersionDetector_Layer2_PackageJSON_NoVersionSignal(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"name":"myapp","scripts":{"build":"tsc"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "module resolution",
		Ecosystem:   ecosystem.EcoTypeScript,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layer != 5 {
		t.Errorf("want layer=5 (no version in package.json) got layer=%d version=%q", layer, version)
	}
	if version != "latest_stable" {
		t.Errorf("want latest_stable got %q", version)
	}
}

func TestVersionDetector_Layer2_GoMod_NoGoDirective(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "channel usage",
		Ecosystem:   ecosystem.EcoGo,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layer != 5 {
		t.Errorf("want layer=5 (no go directive in go.mod) got layer=%d version=%q", layer, version)
	}
	if version != "latest_stable" {
		t.Errorf("want latest_stable got %q", version)
	}
}

func TestVersionDetector_Layer2_GoMod_Malformed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("this is not a valid go.mod\n!!!!\n"), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "generic types",
		Ecosystem:   ecosystem.EcoGo,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error for malformed go.mod: %v", err)
	}
	if layer != 5 {
		t.Errorf("want layer=5 (malformed go.mod) got layer=%d version=%q", layer, version)
	}
}

func TestVersionDetector_Layer2_CargoToml_NoVersionSignal(t *testing.T) {
	dir := t.TempDir()

	cargoToml := "[package]\nname = \"mypkg\"\nversion = \"0.1.0\"\n"
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargoToml), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "ownership",
		Ecosystem:   ecosystem.EcoRust,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layer != 5 {
		t.Errorf("want layer=5 (no version signal in Cargo.toml) got layer=%d version=%q", layer, version)
	}
	if version != "latest_stable" {
		t.Errorf("want latest_stable got %q", version)
	}
}

func TestVersionDetector_Layer2_Pyproject_NoVersionSignal(t *testing.T) {
	dir := t.TempDir()

	content := "[tool.black]\nline-length = 88\n"
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "type hints",
		Ecosystem:   ecosystem.EcoPython,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if layer != 5 {
		t.Errorf("want layer=5 (no version in pyproject.toml) got layer=%d version=%q", layer, version)
	}
	if version != "latest_stable" {
		t.Errorf("want latest_stable got %q", version)
	}
}

func TestVersionDetector_Layer2_PackageJSON_AtTypesNodeInDependencies(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{"name":"myapp","dependencies":{"@types/node":"^16.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0600); err != nil {
		t.Fatal(err)
	}

	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{SkipLLMDetection: true})
	ctx := context.Background()

	version, layer, err := vd.Detect(ctx, ecosystem.QueryRequest{
		Query:       "Buffer usage",
		Ecosystem:   ecosystem.EcoTypeScript,
		ProjectPath: dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "16.0.0" {
		t.Errorf("want version=16.0.0 got %q", version)
	}
	if layer != 2 {
		t.Errorf("want layer=2 got %d", layer)
	}
}
