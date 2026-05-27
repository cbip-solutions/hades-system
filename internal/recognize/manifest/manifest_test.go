package manifest

import (
	"embed"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// go:embed testdata
var testdataFS embed.FS

func testFS(t *testing.T, subdir string) fs.FS {
	t.Helper()
	sub, err := fs.Sub(testdataFS, "testdata/"+subdir)
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	return sub
}

func TestGoModDetector_Match_OK(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("module github.com/example/foo\n\ngo 1.22\n")},
	}
	d := GoModDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("Match returned err: %v", err)
	}
	if ev == nil {
		t.Fatal("Match returned nil evidence; want non-nil")
	}
	if ev.Ecosystem != "go" {
		t.Errorf("Ecosystem = %q; want %q", ev.Ecosystem, "go")
	}
	if ev.Language != "Go" {
		t.Errorf("Language = %q; want %q", ev.Language, "Go")
	}
	if ev.Version != "1.22" {
		t.Errorf("Version = %q; want %q", ev.Version, "1.22")
	}
	if ev.Confidence != 1.0 {
		t.Errorf("Confidence = %v; want 1.0", ev.Confidence)
	}
	if ev.Path != "go.mod" {
		t.Errorf("Path = %q; want %q", ev.Path, "go.mod")
	}
}

func TestGoModDetector_Match_NotPresent(t *testing.T) {
	fsys := fstest.MapFS{}
	d := GoModDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("Match err: %v", err)
	}
	if ev != nil {
		t.Errorf("ev = %+v; want nil for absent manifest", ev)
	}
}

func TestPackageJSONDetector_Match_OK(t *testing.T) {
	fsys := testFS(t, "package-json")
	d := PackageJSONDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("Match err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil; want non-nil")
	}
	if ev.Ecosystem != "javascript" {
		t.Errorf("Ecosystem = %q; want javascript", ev.Ecosystem)
	}
	if ev.Confidence < 0.9 {
		t.Errorf("Confidence = %v; want >=0.9", ev.Confidence)
	}

	if ev.Language != "TypeScript" {
		t.Errorf("Language = %q; want TypeScript (deps signal)", ev.Language)
	}
}

func TestPackageJSONDetector_Match_PlainJS(t *testing.T) {
	fsys := fstest.MapFS{
		"package.json": &fstest.MapFile{Data: []byte(`{"name":"plain","version":"1.0.0"}`)},
	}
	d := PackageJSONDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Language != "JavaScript" {
		t.Errorf("Language = %q; want JavaScript", ev.Language)
	}
}

func TestPackageJSONDetector_Match_TsconfigSignal(t *testing.T) {
	fsys := fstest.MapFS{
		"package.json":  &fstest.MapFile{Data: []byte(`{"name":"x","version":"1.0.0"}`)},
		"tsconfig.json": &fstest.MapFile{Data: []byte(`{"compilerOptions":{}}`)},
	}
	d := PackageJSONDetector{}
	ev, _ := d.Match(fsys)
	if ev == nil || ev.Language != "TypeScript" {
		t.Errorf("Language inferred from tsconfig.json failed; ev=%+v", ev)
	}
}

func TestCargoTomlDetector_Match_OK(t *testing.T) {
	fsys := testFS(t, "cargo-toml")
	d := CargoTomlDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("Match err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Ecosystem != "rust" {
		t.Errorf("Ecosystem = %q; want rust", ev.Ecosystem)
	}
	if ev.Language != "Rust" {
		t.Errorf("Language = %q; want Rust", ev.Language)
	}
}

func TestCargoTomlDetector_VirtualWorkspace(t *testing.T) {
	fsys := fstest.MapFS{
		"Cargo.toml": &fstest.MapFile{Data: []byte("[workspace]\nmembers = [\"crates/*\"]\n")},
	}
	d := CargoTomlDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil; want non-nil for virtual workspace")
	}
}

func TestPyprojectDetector_Match_PEP621(t *testing.T) {
	fsys := testFS(t, "pyproject")
	d := PyprojectDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Ecosystem != "python" {
		t.Errorf("Ecosystem = %q; want python", ev.Ecosystem)
	}
}

func TestPyprojectDetector_Poetry(t *testing.T) {
	fsys := fstest.MapFS{
		"pyproject.toml": &fstest.MapFile{Data: []byte("[tool.poetry]\nname = \"poetry-app\"\nversion = \"1.0\"\n")},
	}
	d := PyprojectDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
}

func TestRequirementsDetector_Match_OK(t *testing.T) {
	fsys := testFS(t, "requirements")
	d := RequirementsDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Ecosystem != "python" {
		t.Errorf("Ecosystem = %q; want python", ev.Ecosystem)
	}
	if ev.Confidence > 0.9 {
		t.Errorf("Confidence = %v; want lower than pyproject (≤0.9)", ev.Confidence)
	}
}

func TestSetupPyDetector_Match_OK(t *testing.T) {
	fsys := testFS(t, "setup-py")
	d := SetupPyDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Ecosystem != "python" {
		t.Errorf("Ecosystem = %q; want python", ev.Ecosystem)
	}
}

func TestGemfileDetector_Match_OK(t *testing.T) {
	fsys := testFS(t, "gemfile")
	d := GemfileDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Ecosystem != "ruby" {
		t.Errorf("Ecosystem = %q; want ruby", ev.Ecosystem)
	}
}

func TestComposerDetector_Match_OK(t *testing.T) {
	fsys := testFS(t, "composer")
	d := ComposerDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Ecosystem != "php" {
		t.Errorf("Ecosystem = %q; want php", ev.Ecosystem)
	}
}

func TestMixExsDetector_Match_OK(t *testing.T) {
	fsys := testFS(t, "mix-exs")
	d := MixExsDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Ecosystem != "elixir" {
		t.Errorf("Ecosystem = %q; want elixir", ev.Ecosystem)
	}
}

func TestPubspecDetector_Match_OK(t *testing.T) {
	fsys := testFS(t, "pubspec")
	d := PubspecDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Ecosystem != "dart" {
		t.Errorf("Ecosystem = %q; want dart", ev.Ecosystem)
	}

	if ev.Language != "Dart (Flutter)" {
		t.Errorf("Language = %q; want Dart (Flutter)", ev.Language)
	}
}

func TestCSProjDetector_Match_OK(t *testing.T) {
	fsys := testFS(t, "csproj")
	d := CSProjDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Ecosystem != "dotnet" {
		t.Errorf("Ecosystem = %q; want dotnet", ev.Ecosystem)
	}
	if ev.Version != "net8.0" {
		t.Errorf("Version = %q; want net8.0", ev.Version)
	}
}

func TestPOMDetector_Match_OK(t *testing.T) {
	fsys := testFS(t, "pom")
	d := POMDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Ecosystem != "java" {
		t.Errorf("Ecosystem = %q; want java", ev.Ecosystem)
	}
}

func TestBuildGradleDetector_Match_OK(t *testing.T) {
	fsys := testFS(t, "gradle-kts")
	d := BuildGradleDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Ecosystem != "java" {
		t.Errorf("Ecosystem = %q; want java", ev.Ecosystem)
	}

	if ev.Language != "Kotlin" {
		t.Errorf("Language = %q; want Kotlin", ev.Language)
	}
}

func TestCMakeListsDetector_Match_OK(t *testing.T) {
	fsys := testFS(t, "cmake")
	d := CMakeListsDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Ecosystem != "cmake" {
		t.Errorf("Ecosystem = %q; want cmake", ev.Ecosystem)
	}

	if ev.Confidence > 0.8 {
		t.Errorf("Confidence = %v; want ≤0.8 (CMake low-confidence)", ev.Confidence)
	}
}

func TestMakefileDetector_Match_OK(t *testing.T) {
	fsys := testFS(t, "makefile")
	d := MakefileDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Ecosystem != "make" {
		t.Errorf("Ecosystem = %q; want make", ev.Ecosystem)
	}

	if ev.Confidence > 0.6 {
		t.Errorf("Confidence = %v; want ≤0.6 (Makefile last-resort)", ev.Confidence)
	}
}

func TestMakefileDetector_VariantNames(t *testing.T) {
	for _, name := range []string{"Makefile", "makefile", "GNUmakefile"} {
		fsys := fstest.MapFS{
			name: &fstest.MapFile{Data: []byte("all:\n\techo hi\n")},
		}
		d := MakefileDetector{}
		ev, _ := d.Match(fsys)
		if ev == nil {
			t.Errorf("MakefileDetector did not detect %q", name)
		}
	}
}

func TestDetect_AggregatesAcrossManifestPresent(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod":       &fstest.MapFile{Data: []byte("module example\n\ngo 1.22\n")},
		"package.json": &fstest.MapFile{Data: []byte(`{"name":"site","version":"1.0.0"}`)},
		"Makefile":     &fstest.MapFile{Data: []byte("build:\n\tgo build .\n")},
	}
	evs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("Detect err: %v", err)
	}
	if got := len(evs); got != 3 {
		t.Errorf("Detect returned %d evidence; want 3", got)
	}
	seen := map[string]bool{}
	for _, ev := range evs {
		seen[ev.Ecosystem] = true
	}
	for _, want := range []string{"go", "javascript", "make"} {
		if !seen[want] {
			t.Errorf("Detect missing ecosystem %q; got %v", want, seen)
		}
	}
}

func TestDetect_EmptyFS(t *testing.T) {
	fsys := fstest.MapFS{}
	evs, err := Detect(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(evs) != 0 {
		t.Errorf("Detect returned %d evidence; want 0", len(evs))
	}
}

func TestReadManifest_CapEnforced(t *testing.T) {
	huge := make([]byte, 1024*1024)
	for i := range huge {
		huge[i] = 'a'
	}
	fsys := fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: huge},
	}
	buf, err := readManifest(fsys, "go.mod", ManifestReadCap)
	if err != nil {
		t.Fatalf("readManifest err: %v", err)
	}
	if got := int64(len(buf)); got != ManifestReadCap {
		t.Errorf("read %d bytes; want %d", got, ManifestReadCap)
	}
}

func TestGoModDetector_Match_Malformed(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod": &fstest.MapFile{Data: []byte("not a real go.mod content\n")},
	}
	d := GoModDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil for malformed", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for malformed go.mod")
	}
}

func TestDetect_MalformedReportsButContinues(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod":       &fstest.MapFile{Data: []byte("not a real go.mod\n")},
		"package.json": &fstest.MapFile{Data: []byte(`{"name":"x","version":"1"}`)},
	}
	evs, err := Detect(fsys)
	if err == nil {
		t.Error("Detect err nil; want non-nil for malformed go.mod")
	}
	if len(evs) != 1 {
		t.Errorf("evidence count = %d; want 1 (package.json)", len(evs))
	}
	if !strings.Contains(err.Error(), "go") {
		t.Errorf("error message %q missing 'go'", err.Error())
	}
}

func TestComposerDetector_Malformed(t *testing.T) {
	fsys := fstest.MapFS{
		"composer.json": &fstest.MapFile{Data: []byte("not json")},
	}
	d := ComposerDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for malformed composer.json")
	}
}

func TestCargoTomlDetector_Malformed(t *testing.T) {
	fsys := fstest.MapFS{
		"Cargo.toml": &fstest.MapFile{Data: []byte("[[[ not toml")},
	}
	d := CargoTomlDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for malformed Cargo.toml")
	}
}

func TestPubspecDetector_Malformed(t *testing.T) {
	fsys := fstest.MapFS{
		"pubspec.yaml": &fstest.MapFile{Data: []byte("name: [unclosed\n")},
	}
	d := PubspecDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for malformed pubspec.yaml")
	}
}

func TestPOMDetector_Malformed(t *testing.T) {
	fsys := fstest.MapFS{
		"pom.xml": &fstest.MapFile{Data: []byte("<not xml")},
	}
	d := POMDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for malformed pom.xml")
	}
}

func TestPackageJSONDetector_Malformed(t *testing.T) {
	fsys := fstest.MapFS{
		"package.json": &fstest.MapFile{Data: []byte("not json")},
	}
	d := PackageJSONDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for malformed package.json")
	}
}

func TestRequirementsDetector_EmptyFile(t *testing.T) {
	fsys := fstest.MapFS{
		"requirements.txt": &fstest.MapFile{Data: []byte("# only comments\n\n")},
	}
	d := RequirementsDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil for empty requirements", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for empty requirements")
	}
}

func TestSetupPyDetector_NoSetupCall(t *testing.T) {
	fsys := fstest.MapFS{
		"setup.py": &fstest.MapFile{Data: []byte("# empty file\n")},
	}
	d := SetupPyDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for setup.py without setup() call")
	}
}

func TestGemfileDetector_NoSource(t *testing.T) {
	fsys := fstest.MapFS{
		"Gemfile": &fstest.MapFile{Data: []byte("# just a comment\n")},
	}
	d := GemfileDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for Gemfile without source")
	}
}

func TestMixExsDetector_NoAppPattern(t *testing.T) {
	fsys := fstest.MapFS{
		"mix.exs": &fstest.MapFile{Data: []byte("defmodule Empty do\nend\n")},
	}
	d := MixExsDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for mix.exs without app:")
	}
}

func TestBuildGradleDetector_NoPluginsBlock(t *testing.T) {
	fsys := fstest.MapFS{
		"build.gradle": &fstest.MapFile{Data: []byte("// empty\n")},
	}
	d := BuildGradleDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for build.gradle without plugins")
	}
}

func TestBuildGradleDetector_JavaPlain(t *testing.T) {
	fsys := fstest.MapFS{
		"build.gradle": &fstest.MapFile{Data: []byte("plugins {\n  id 'java'\n}\n")},
	}
	d := BuildGradleDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev == nil {
		t.Fatal("ev nil")
	}
	if ev.Language != "Java" {
		t.Errorf("Language = %q; want Java", ev.Language)
	}
}

func TestCSProjDetector_NoFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"README.md": &fstest.MapFile{Data: []byte("# nothing\n")},
	}
	d := CSProjDetector{}
	ev, err := d.Match(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ev != nil {
		t.Errorf("ev = %+v; want nil for fs without csproj", ev)
	}
}

func TestPOMDetector_MissingArtifactID(t *testing.T) {
	fsys := fstest.MapFS{
		"pom.xml": &fstest.MapFile{Data: []byte(`<?xml version="1.0"?><project><groupId>g</groupId></project>`)},
	}
	d := POMDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for pom.xml without artifactId")
	}
}

func TestCMakeListsDetector_NoProject(t *testing.T) {
	fsys := fstest.MapFS{
		"CMakeLists.txt": &fstest.MapFile{Data: []byte("# empty\n")},
	}
	d := CMakeListsDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for CMakeLists.txt without project()")
	}
}

func TestComposerDetector_MissingName(t *testing.T) {
	fsys := fstest.MapFS{
		"composer.json": &fstest.MapFile{Data: []byte(`{"require":{}}`)},
	}
	d := ComposerDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for composer.json without name")
	}
}

func TestPyprojectDetector_NoName(t *testing.T) {
	fsys := fstest.MapFS{
		"pyproject.toml": &fstest.MapFile{Data: []byte("[project]\nversion = \"1.0\"\n")},
	}
	d := PyprojectDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for pyproject.toml without name")
	}
}

func TestPubspecDetector_MissingName(t *testing.T) {
	fsys := fstest.MapFS{
		"pubspec.yaml": &fstest.MapFile{Data: []byte("version: 1.0\n")},
	}
	d := PubspecDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for pubspec.yaml without name")
	}
}

func TestPackageJSONDetector_MissingName(t *testing.T) {
	fsys := fstest.MapFS{
		"package.json": &fstest.MapFile{Data: []byte(`{"version":"1.0"}`)},
	}
	d := PackageJSONDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for package.json without name")
	}
}

func TestCargoTomlDetector_EmptyFile(t *testing.T) {
	fsys := fstest.MapFS{
		"Cargo.toml": &fstest.MapFile{Data: []byte("# nothing\n")},
	}
	d := CargoTomlDetector{}
	ev, err := d.Match(fsys)
	if ev != nil {
		t.Errorf("ev = %+v; want nil", ev)
	}
	if err == nil {
		t.Error("err nil; want non-nil for empty Cargo.toml")
	}
}

func TestDetectorNames(t *testing.T) {
	expectedNames := map[string]string{
		"GoMod":        "go",
		"CargoToml":    "rust",
		"PackageJSON":  "javascript",
		"Pyproject":    "python-pyproject",
		"Requirements": "python-requirements",
		"SetupPy":      "python-setup-py",
		"Gemfile":      "ruby",
		"Composer":     "php",
		"MixExs":       "elixir",
		"Pubspec":      "dart",
		"CSProj":       "dotnet",
		"POM":          "java-maven",
		"BuildGradle":  "java-gradle",
		"CMakeLists":   "cmake",
		"Makefile":     "make",
	}
	got := map[string]string{
		"GoMod":        GoModDetector{}.Name(),
		"CargoToml":    CargoTomlDetector{}.Name(),
		"PackageJSON":  PackageJSONDetector{}.Name(),
		"Pyproject":    PyprojectDetector{}.Name(),
		"Requirements": RequirementsDetector{}.Name(),
		"SetupPy":      SetupPyDetector{}.Name(),
		"Gemfile":      GemfileDetector{}.Name(),
		"Composer":     ComposerDetector{}.Name(),
		"MixExs":       MixExsDetector{}.Name(),
		"Pubspec":      PubspecDetector{}.Name(),
		"CSProj":       CSProjDetector{}.Name(),
		"POM":          POMDetector{}.Name(),
		"BuildGradle":  BuildGradleDetector{}.Name(),
		"CMakeLists":   CMakeListsDetector{}.Name(),
		"Makefile":     MakefileDetector{}.Name(),
	}
	for k, want := range expectedNames {
		if got[k] != want {
			t.Errorf("%s.Name() = %q; want %q", k, got[k], want)
		}
	}
}
