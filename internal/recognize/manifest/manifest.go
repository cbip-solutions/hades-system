// SPDX-License-Identifier: MIT
// Package manifest implements Tier 1 of zen recognize per spec §2.4 Q4=B:
// per-ecosystem manifest detection with highest confidence (~0 false positive).
//
// Each detector implements the Detector interface; the package-level Detect()
// aggregates evidence across all 15 detectors. Pure-Go zero-CGO; see Q4 spec
// anti-pattern (NO AST/tree-sitter).
//
// Evidence values feed Phase B B6 orchestrator (recognize.go) which applies
// the confidence thresholds + tier cascade short-circuit logic.
package manifest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

const ManifestReadCap int64 = 50 * 1024

type Evidence struct {
	Ecosystem    string
	Language     string
	Version      string
	Path         string
	Confidence   float64
	Dependencies map[string]string
}

type Detector interface {
	Name() string
	Match(fsys fs.FS) (*Evidence, error)
}

func readManifest(fsys fs.FS, path string, capBytes int64) ([]byte, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, capBytes))
}

func fileExists(fsys fs.FS, path string) bool {
	_, err := fs.Stat(fsys, path)
	return err == nil
}

type GoModDetector struct{}

func (GoModDetector) Name() string { return "go" }

var goVersionRE = regexp.MustCompile(`^go\s+(\d+\.\d+(?:\.\d+)?)\s*$`)
var goModuleRE = regexp.MustCompile(`^module\s+(\S+)\s*$`)

func (d GoModDetector) Match(fsys fs.FS) (*Evidence, error) {
	if !fileExists(fsys, "go.mod") {
		return nil, nil
	}
	buf, err := readManifest(fsys, "go.mod", ManifestReadCap)
	if err != nil {
		return nil, fmt.Errorf("go.mod: %w", err)
	}
	var version, module string
	scanner := bufio.NewScanner(bytes.NewReader(buf))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if m := goVersionRE.FindStringSubmatch(line); m != nil {
			version = m[1]
		}
		if m := goModuleRE.FindStringSubmatch(line); m != nil {
			module = m[1]
		}
	}
	if module == "" {
		return nil, errors.New("go.mod: missing 'module' directive")
	}
	return &Evidence{
		Ecosystem:  "go",
		Language:   "Go",
		Version:    version,
		Path:       "go.mod",
		Confidence: 1.0,
	}, nil
}

type CargoTomlDetector struct{}

func (CargoTomlDetector) Name() string { return "rust" }

type cargoTomlShape struct {
	Package struct {
		Name    string `toml:"name"`
		Version string `toml:"version"`
		Edition string `toml:"edition"`
	} `toml:"package"`
	Workspace map[string]any `toml:"workspace"`

	Dependencies    map[string]any `toml:"dependencies"`
	DevDependencies map[string]any `toml:"dev-dependencies"`
}

func (d CargoTomlDetector) Match(fsys fs.FS) (*Evidence, error) {
	if !fileExists(fsys, "Cargo.toml") {
		return nil, nil
	}
	buf, err := readManifest(fsys, "Cargo.toml", ManifestReadCap)
	if err != nil {
		return nil, fmt.Errorf("Cargo.toml: %w", err)
	}
	var c cargoTomlShape
	if err := toml.Unmarshal(buf, &c); err != nil {
		return nil, fmt.Errorf("Cargo.toml parse: %w", err)
	}

	if c.Package.Name == "" && c.Workspace == nil {
		return nil, errors.New("Cargo.toml: neither [package] nor [workspace] present")
	}
	deps := mergeCargoDeps(c.Dependencies, c.DevDependencies)
	return &Evidence{
		Ecosystem:    "rust",
		Language:     "Rust",
		Version:      c.Package.Version,
		Path:         "Cargo.toml",
		Confidence:   1.0,
		Dependencies: deps,
	}, nil
}

func mergeCargoDeps(deps, devDeps map[string]any) map[string]string {
	if len(deps) == 0 && len(devDeps) == 0 {
		return nil
	}
	out := make(map[string]string, len(deps)+len(devDeps))
	for k, v := range deps {
		out[k] = cargoDepVersion(v)
	}
	for k, v := range devDeps {
		out[k] = cargoDepVersion(v)
	}
	return out
}

func cargoDepVersion(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		if vv, ok := t["version"].(string); ok {
			return vv
		}
	}
	return ""
}

type PackageJSONDetector struct{}

func (PackageJSONDetector) Name() string { return "javascript" }

type packageJSONShape struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Engines         map[string]string `json:"engines"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Type            string            `json:"type"`
}

func (d PackageJSONDetector) Match(fsys fs.FS) (*Evidence, error) {
	if !fileExists(fsys, "package.json") {
		return nil, nil
	}
	buf, err := readManifest(fsys, "package.json", ManifestReadCap)
	if err != nil {
		return nil, fmt.Errorf("package.json: %w", err)
	}
	var p packageJSONShape
	if err := json.Unmarshal(buf, &p); err != nil {
		return nil, fmt.Errorf("package.json parse: %w", err)
	}
	if p.Name == "" {
		return nil, errors.New("package.json: missing name field")
	}

	language := "JavaScript"
	if _, ok := p.DevDependencies["typescript"]; ok {
		language = "TypeScript"
	} else if _, ok := p.Dependencies["typescript"]; ok {
		language = "TypeScript"
	} else if fileExists(fsys, "tsconfig.json") {
		language = "TypeScript"
	}

	deps := mergePackageJSONDeps(p.Dependencies, p.DevDependencies)
	return &Evidence{
		Ecosystem:    "javascript",
		Language:     language,
		Version:      p.Version,
		Path:         "package.json",
		Confidence:   0.95,
		Dependencies: deps,
	}, nil
}

func mergePackageJSONDeps(deps, devDeps map[string]string) map[string]string {
	if len(deps) == 0 && len(devDeps) == 0 {
		return nil
	}
	out := make(map[string]string, len(deps)+len(devDeps))
	for k, v := range deps {
		out[k] = v
	}
	for k, v := range devDeps {
		out[k] = v
	}
	return out
}

type PyprojectDetector struct{}

func (PyprojectDetector) Name() string { return "python-pyproject" }

type pyprojectShape struct {
	Project struct {
		Name           string   `toml:"name"`
		Version        string   `toml:"version"`
		RequiresPython string   `toml:"requires-python"`
		Dependencies   []string `toml:"dependencies"`
	} `toml:"project"`
	Tool struct {
		Poetry struct {
			Name         string         `toml:"name"`
			Version      string         `toml:"version"`
			Dependencies map[string]any `toml:"dependencies"`
		} `toml:"poetry"`
	} `toml:"tool"`
}

func (d PyprojectDetector) Match(fsys fs.FS) (*Evidence, error) {
	if !fileExists(fsys, "pyproject.toml") {
		return nil, nil
	}
	buf, err := readManifest(fsys, "pyproject.toml", ManifestReadCap)
	if err != nil {
		return nil, fmt.Errorf("pyproject.toml: %w", err)
	}
	var p pyprojectShape
	if err := toml.Unmarshal(buf, &p); err != nil {
		return nil, fmt.Errorf("pyproject.toml parse: %w", err)
	}
	name := p.Project.Name
	version := p.Project.Version
	if name == "" {
		name = p.Tool.Poetry.Name
		version = p.Tool.Poetry.Version
	}
	if name == "" {
		return nil, errors.New("pyproject.toml: neither [project].name nor [tool.poetry].name present")
	}
	deps := mergePyprojectDeps(p.Project.Dependencies, p.Tool.Poetry.Dependencies)
	return &Evidence{
		Ecosystem:    "python",
		Language:     "Python",
		Version:      version,
		Path:         "pyproject.toml",
		Confidence:   1.0,
		Dependencies: deps,
	}, nil
}

func mergePyprojectDeps(pep621 []string, poetry map[string]any) map[string]string {
	if len(pep621) == 0 && len(poetry) == 0 {
		return nil
	}
	out := make(map[string]string, len(pep621)+len(poetry))
	for _, line := range pep621 {
		name, version := parsePEP508Line(line)
		if name != "" {
			out[name] = version
		}
	}
	for k, v := range poetry {

		out[k] = cargoDepVersion(v)
	}
	return out
}

type RequirementsDetector struct{}

func (RequirementsDetector) Name() string { return "python-requirements" }

func (d RequirementsDetector) Match(fsys fs.FS) (*Evidence, error) {
	if !fileExists(fsys, "requirements.txt") {
		return nil, nil
	}
	buf, err := readManifest(fsys, "requirements.txt", ManifestReadCap)
	if err != nil {
		return nil, fmt.Errorf("requirements.txt: %w", err)
	}
	deps := map[string]string{}
	lines := 0
	scanner := bufio.NewScanner(bytes.NewReader(buf))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines++

		if idx := strings.Index(line, "#"); idx > 0 {
			line = strings.TrimSpace(line[:idx])
		}

		if idx := strings.Index(line, ";"); idx > 0 {
			line = strings.TrimSpace(line[:idx])
		}
		name, version := parsePEP508Line(line)
		if name == "" {
			continue
		}
		deps[name] = version
	}

	if lines == 0 {
		return nil, errors.New("requirements.txt: zero dependency lines")
	}
	if len(deps) == 0 {
		deps = nil
	}
	return &Evidence{
		Ecosystem:    "python",
		Language:     "Python",
		Version:      "",
		Path:         "requirements.txt",
		Confidence:   0.85,
		Dependencies: deps,
	}, nil
}

func parsePEP508Line(line string) (string, string) {
	if line == "" || strings.HasPrefix(line, "-") {
		return "", ""
	}

	idx := strings.IndexAny(line, "=<>!~ ")
	if idx < 0 {

		return strings.TrimSpace(stripExtras(line)), ""
	}
	name := strings.TrimSpace(stripExtras(line[:idx]))
	version := strings.TrimSpace(line[idx:])
	return name, version
}

// stripExtras removes the [extras] syntax from a PEP 440 name (e.g.
// "requests[security]" → "requests"). Keeps the surface narrow + matches
// idiomatic pip-resolver behavior.
func stripExtras(name string) string {
	if idx := strings.Index(name, "["); idx >= 0 {
		return name[:idx]
	}
	return name
}

type SetupPyDetector struct{}

func (SetupPyDetector) Name() string { return "python-setup-py" }

var setupNameRE = regexp.MustCompile(`(?s)setup\s*\([^)]*\bname\s*=\s*['"]([^'"]+)['"]`)

func (d SetupPyDetector) Match(fsys fs.FS) (*Evidence, error) {
	if !fileExists(fsys, "setup.py") {
		return nil, nil
	}
	buf, err := readManifest(fsys, "setup.py", ManifestReadCap)
	if err != nil {
		return nil, fmt.Errorf("setup.py: %w", err)
	}
	if !setupNameRE.Match(buf) {
		return nil, errors.New("setup.py: setup(name=...) not found")
	}
	return &Evidence{
		Ecosystem:  "python",
		Language:   "Python",
		Path:       "setup.py",
		Confidence: 0.85,
	}, nil
}

type GemfileDetector struct{}

func (GemfileDetector) Name() string { return "ruby" }

var gemfileSourceRE = regexp.MustCompile(`(?m)^source\s+['"][^'"]+['"]`)

func (d GemfileDetector) Match(fsys fs.FS) (*Evidence, error) {
	if !fileExists(fsys, "Gemfile") {
		return nil, nil
	}
	buf, err := readManifest(fsys, "Gemfile", ManifestReadCap)
	if err != nil {
		return nil, fmt.Errorf("Gemfile: %w", err)
	}
	if !gemfileSourceRE.Match(buf) {
		return nil, errors.New("Gemfile: source directive not found")
	}
	return &Evidence{
		Ecosystem:  "ruby",
		Language:   "Ruby",
		Path:       "Gemfile",
		Confidence: 0.95,
	}, nil
}

type ComposerDetector struct{}

func (ComposerDetector) Name() string { return "php" }

type composerShape struct {
	Name    string            `json:"name"`
	Require map[string]string `json:"require"`
}

func (d ComposerDetector) Match(fsys fs.FS) (*Evidence, error) {
	if !fileExists(fsys, "composer.json") {
		return nil, nil
	}
	buf, err := readManifest(fsys, "composer.json", ManifestReadCap)
	if err != nil {
		return nil, fmt.Errorf("composer.json: %w", err)
	}
	var c composerShape
	if err := json.Unmarshal(buf, &c); err != nil {
		return nil, fmt.Errorf("composer.json parse: %w", err)
	}
	if c.Name == "" {
		return nil, errors.New("composer.json: missing name field")
	}
	var deps map[string]string
	if len(c.Require) > 0 {
		deps = make(map[string]string, len(c.Require))
		for k, v := range c.Require {
			deps[k] = v
		}
	}
	return &Evidence{
		Ecosystem:    "php",
		Language:     "PHP",
		Path:         "composer.json",
		Confidence:   1.0,
		Dependencies: deps,
	}, nil
}

type MixExsDetector struct{}

func (MixExsDetector) Name() string { return "elixir" }

var mixAppRE = regexp.MustCompile(`(?m)\bapp:\s*:(\w+)`)

func (d MixExsDetector) Match(fsys fs.FS) (*Evidence, error) {
	if !fileExists(fsys, "mix.exs") {
		return nil, nil
	}
	buf, err := readManifest(fsys, "mix.exs", ManifestReadCap)
	if err != nil {
		return nil, fmt.Errorf("mix.exs: %w", err)
	}
	if !mixAppRE.Match(buf) {
		return nil, errors.New("mix.exs: app: :name pattern not found")
	}
	return &Evidence{
		Ecosystem:  "elixir",
		Language:   "Elixir",
		Path:       "mix.exs",
		Confidence: 0.9,
	}, nil
}

type PubspecDetector struct{}

func (PubspecDetector) Name() string { return "dart" }

type pubspecShape struct {
	Name         string            `yaml:"name"`
	Version      string            `yaml:"version"`
	Environment  map[string]string `yaml:"environment"`
	Dependencies map[string]any    `yaml:"dependencies"`
}

func (d PubspecDetector) Match(fsys fs.FS) (*Evidence, error) {
	if !fileExists(fsys, "pubspec.yaml") {
		return nil, nil
	}
	buf, err := readManifest(fsys, "pubspec.yaml", ManifestReadCap)
	if err != nil {
		return nil, fmt.Errorf("pubspec.yaml: %w", err)
	}
	var p pubspecShape
	if err := yaml.Unmarshal(buf, &p); err != nil {
		return nil, fmt.Errorf("pubspec.yaml parse: %w", err)
	}
	if p.Name == "" {
		return nil, errors.New("pubspec.yaml: missing name field")
	}

	language := "Dart"
	if _, ok := p.Dependencies["flutter"]; ok {
		language = "Dart (Flutter)"
	}
	return &Evidence{
		Ecosystem:  "dart",
		Language:   language,
		Version:    p.Version,
		Path:       "pubspec.yaml",
		Confidence: 1.0,
	}, nil
}

type CSProjDetector struct{}

func (CSProjDetector) Name() string { return "dotnet" }

type csprojShape struct {
	XMLName       xml.Name `xml:"Project"`
	PropertyGroup struct {
		TargetFramework string `xml:"TargetFramework"`
	} `xml:"PropertyGroup"`
}

func (d CSProjDetector) Match(fsys fs.FS) (*Evidence, error) {

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, nil
	}
	var path string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".csproj") || strings.HasSuffix(n, ".fsproj") {
			path = n
			break
		}
	}
	if path == "" {
		return nil, nil
	}
	buf, err := readManifest(fsys, path, ManifestReadCap)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var p csprojShape
	if err := xml.Unmarshal(buf, &p); err != nil {
		return nil, fmt.Errorf("%s parse: %w", path, err)
	}
	return &Evidence{
		Ecosystem:  "dotnet",
		Language:   "C#",
		Version:    p.PropertyGroup.TargetFramework,
		Path:       path,
		Confidence: 1.0,
	}, nil
}

type POMDetector struct{}

func (POMDetector) Name() string { return "java-maven" }

type pomShape struct {
	XMLName    xml.Name `xml:"project"`
	GroupID    string   `xml:"groupId"`
	ArtifactID string   `xml:"artifactId"`
	Version    string   `xml:"version"`
}

func (d POMDetector) Match(fsys fs.FS) (*Evidence, error) {
	if !fileExists(fsys, "pom.xml") {
		return nil, nil
	}
	buf, err := readManifest(fsys, "pom.xml", ManifestReadCap)
	if err != nil {
		return nil, fmt.Errorf("pom.xml: %w", err)
	}
	var p pomShape
	if err := xml.Unmarshal(buf, &p); err != nil {
		return nil, fmt.Errorf("pom.xml parse: %w", err)
	}
	if p.ArtifactID == "" {
		return nil, errors.New("pom.xml: missing artifactId")
	}
	return &Evidence{
		Ecosystem:  "java",
		Language:   "Java",
		Version:    p.Version,
		Path:       "pom.xml",
		Confidence: 1.0,
	}, nil
}

type BuildGradleDetector struct{}

func (BuildGradleDetector) Name() string { return "java-gradle" }

var gradleKotlinRE = regexp.MustCompile(`kotlin\s*\(`)
var gradlePluginsBlockRE = regexp.MustCompile(`(?m)^\s*(apply\s+plugin|plugins)\s*[({]`)

func (d BuildGradleDetector) Match(fsys fs.FS) (*Evidence, error) {
	paths := []string{"build.gradle.kts", "build.gradle"}
	var path string
	var buf []byte
	for _, p := range paths {
		if fileExists(fsys, p) {
			path = p
			b, err := readManifest(fsys, p, ManifestReadCap)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", p, err)
			}
			buf = b
			break
		}
	}
	if path == "" {
		return nil, nil
	}
	hasKotlin := gradleKotlinRE.Match(buf)
	hasPluginsBlock := gradlePluginsBlockRE.Match(buf)
	if !hasKotlin && !hasPluginsBlock {
		return nil, errors.New("build.gradle: no plugins block found")
	}
	language := "Java"
	if hasKotlin {
		language = "Kotlin"
	}
	return &Evidence{
		Ecosystem:  "java",
		Language:   language,
		Path:       path,
		Confidence: 0.95,
	}, nil
}

type CMakeListsDetector struct{}

func (CMakeListsDetector) Name() string { return "cmake" }

var cmakeProjectRE = regexp.MustCompile(`(?m)^\s*project\s*\(\s*(\w+)`)

func (d CMakeListsDetector) Match(fsys fs.FS) (*Evidence, error) {
	if !fileExists(fsys, "CMakeLists.txt") {
		return nil, nil
	}
	buf, err := readManifest(fsys, "CMakeLists.txt", ManifestReadCap)
	if err != nil {
		return nil, fmt.Errorf("CMakeLists.txt: %w", err)
	}
	if !cmakeProjectRE.Match(buf) {
		return nil, errors.New("CMakeLists.txt: project() directive not found")
	}
	return &Evidence{
		Ecosystem:  "cmake",
		Language:   "C++",
		Path:       "CMakeLists.txt",
		Confidence: 0.7,
	}, nil
}

type MakefileDetector struct{}

func (MakefileDetector) Name() string { return "make" }

func (d MakefileDetector) Match(fsys fs.FS) (*Evidence, error) {
	for _, p := range []string{"Makefile", "makefile", "GNUmakefile"} {
		if fileExists(fsys, p) {
			return &Evidence{
				Ecosystem:  "make",
				Language:   "Make",
				Path:       p,
				Confidence: 0.5,
			}, nil
		}
	}
	return nil, nil
}

var allDetectors = []Detector{
	GoModDetector{},
	CargoTomlDetector{},
	PackageJSONDetector{},
	PyprojectDetector{},
	RequirementsDetector{},
	SetupPyDetector{},
	GemfileDetector{},
	ComposerDetector{},
	MixExsDetector{},
	PubspecDetector{},
	CSProjDetector{},
	POMDetector{},
	BuildGradleDetector{},
	CMakeListsDetector{},
	MakefileDetector{},
}

func Detect(fsys fs.FS) ([]Evidence, error) {
	var out []Evidence
	var errs []error
	for _, d := range allDetectors {
		ev, err := d.Match(fsys)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.Name(), err))
			continue
		}
		if ev != nil {
			out = append(out, *ev)
		}
	}
	return out, errors.Join(errs...)
}
