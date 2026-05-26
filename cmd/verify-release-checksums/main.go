// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type runOptions struct {
	distDir             string
	manifestName        string
	goldenPath          string
	version             string
	requireAllPlatforms bool
	binCheck            bool
	stdout              io.Writer
	stderr              io.Writer
}

func defaultOptions() runOptions {
	return runOptions{
		distDir:      "dist",
		manifestName: "checksums.txt",
		stdout:       os.Stdout,
		stderr:       os.Stderr,
	}
}

var allPlatforms = []string{"darwin-arm64", "linux-amd64", "linux-arm64"}

func main() {
	opts := defaultOptions()
	flag.StringVar(&opts.distDir, "dist", opts.distDir, "GoReleaser dist directory (default 'dist')")
	flag.StringVar(&opts.manifestName, "manifest", opts.manifestName, "Manifest filename (default 'checksums.txt')")
	flag.StringVar(&opts.goldenPath, "golden", "", "Canonical golden manifest path (optional)")
	flag.StringVar(&opts.version, "version", "", "Expected release version (e.g., v1.0.0); required with --golden")
	flag.BoolVar(&opts.requireAllPlatforms, "require-all-platforms", false, "Fail when any of darwin-arm64 / linux-amd64 / linux-arm64 archives are missing from manifest")
	flag.BoolVar(&opts.binCheck, "bin-check", false, "Also exec each top-level zen / zen-swarm-ctld binary present in dist/ to verify --version output matches buildinfo schema")
	flag.Parse()

	if err := verify(opts); err != nil {
		fmt.Fprintf(opts.stderr, "verify-release-checksums: %v\n", err)

		var cfgErr *configError
		if errors.As(err, &cfgErr) {
			os.Exit(2)
		}
		os.Exit(1)
	}
	fmt.Fprintln(opts.stdout, "verify-release-checksums: OK (inv-zen-297)")
}

// configError marks errors that should produce exit code 2 (caller's
// environment is mis-configured) rather than exit code 1 (verification
// signal). Tests do not assert specifics — the main() exit-mapping does.
type configError struct{ inner error }

func (e *configError) Error() string { return e.inner.Error() }
func (e *configError) Unwrap() error { return e.inner }

func verify(opts runOptions) error {
	if opts.distDir == "" {
		return &configError{errors.New("--dist is required")}
	}
	info, err := os.Stat(opts.distDir)
	if err != nil {
		return &configError{fmt.Errorf("--dist %q: %w", opts.distDir, err)}
	}
	if !info.IsDir() {
		return &configError{fmt.Errorf("--dist %q is not a directory", opts.distDir)}
	}

	manifestPath := filepath.Join(opts.distDir, opts.manifestName)
	mf, err := os.Open(manifestPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", opts.manifestName, err)
	}
	defer mf.Close()

	manifest, err := parseChecksumsManifest(mf)
	if err != nil {
		return fmt.Errorf("parse %s: %w", opts.manifestName, err)
	}
	if len(manifest) == 0 {
		return fmt.Errorf("%s contained zero entries (expected at least one artifact line)", opts.manifestName)
	}

	names := make([]string, 0, len(manifest))
	for name := range manifest {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		artifactPath := filepath.Join(opts.distDir, name)
		f, openErr := os.Open(artifactPath)
		if openErr != nil {
			return fmt.Errorf("missing artifact %s: %w", name, openErr)
		}
		h := sha256.New()
		if _, copyErr := io.Copy(h, f); copyErr != nil {
			_ = f.Close()
			return fmt.Errorf("hash %s: %w", name, copyErr)
		}
		_ = f.Close()
		got := hex.EncodeToString(h.Sum(nil))
		want := manifest[name]
		if got != want {
			return fmt.Errorf("checksum mismatch for %s: got %s want %s", name, got, want)
		}
		fmt.Fprintf(opts.stdout, "  OK  %s  %s\n", got, name)
	}

	if opts.requireAllPlatforms {
		if missing := missingPlatforms(manifest); len(missing) > 0 {
			return fmt.Errorf("3-platform matrix incomplete: missing %s", strings.Join(missing, ", "))
		}
	}

	if opts.goldenPath != "" {
		gm, err := loadGolden(opts.goldenPath)
		if err != nil {
			return &configError{fmt.Errorf("load golden %s: %w", opts.goldenPath, err)}
		}
		if err := verifyAgainstGolden(manifest, gm, opts.version); err != nil {
			return err
		}
	}

	if opts.binCheck {
		if err := binCheck(opts); err != nil {
			return err
		}
	}

	return nil
}

func parseChecksumsManifest(r io.Reader) (map[string]string, error) {
	out := make(map[string]string)
	scanner := bufio.NewScanner(r)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		var hexPart, namePart string
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("line %d: expected '<hex> <name>', got %q", lineNum, line)
		}
		hexPart = fields[0]

		namePart = strings.Join(fields[1:], " ")

		if len(hexPart) != 64 {
			return nil, fmt.Errorf("line %d: sha256 must be 64 hex chars, got %d (%q)", lineNum, len(hexPart), hexPart)
		}
		if _, err := hex.DecodeString(hexPart); err != nil {
			return nil, fmt.Errorf("line %d: hex decode %q: %w", lineNum, hexPart, err)
		}
		if _, dup := out[namePart]; dup {
			return nil, fmt.Errorf("line %d: duplicate filename %q in manifest", lineNum, namePart)
		}
		out[namePart] = hexPart
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func missingPlatforms(manifest map[string]string) []string {
	var missing []string
	for _, p := range allPlatforms {
		seen := false
		for name := range manifest {
			if strings.Contains(name, p) {
				seen = true
				break
			}
		}
		if !seen {
			missing = append(missing, p)
		}
	}
	return missing
}

type goldenManifest struct {
	SchemaVersion       string           `json:"schema_version"`
	Platforms           []goldenPlatform `json:"platforms"`
	VersionSummaryRegex string           `json:"version_summary_regex"`
	Notes               string           `json:"notes,omitempty"`
}

type goldenPlatform struct {
	Platform     string `json:"platform"`
	NameTemplate string `json:"name_template"`
}

func loadGolden(path string) (*goldenManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var g goldenManifest
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if g.SchemaVersion == "" {
		return nil, errors.New("golden manifest: schema_version is required")
	}
	if len(g.Platforms) == 0 {
		return nil, errors.New("golden manifest: platforms must be non-empty")
	}
	if g.VersionSummaryRegex == "" {
		return nil, errors.New("golden manifest: version_summary_regex is required")
	}

	if _, err := regexp.Compile(g.VersionSummaryRegex); err != nil {
		return nil, fmt.Errorf("golden manifest: compile version_summary_regex: %w", err)
	}
	return &g, nil
}

func verifyAgainstGolden(manifest map[string]string, g *goldenManifest, version string) error {
	if version == "" {
		return &configError{errors.New("--version is required when --golden is set")}
	}
	var missing []string
	for _, p := range g.Platforms {
		want := strings.ReplaceAll(p.NameTemplate, "{{VERSION}}", version)
		if _, ok := manifest[want]; !ok {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("golden manifest mismatch: missing artifacts %v", missing)
	}
	return nil
}

type versionSummary struct {
	Version   string
	Commit    string
	Date      string
	GoVersion string
	Platform  string
}

var versionSummaryRE = regexp.MustCompile(
	`^zen-swarm\s+(?P<version>\S+)\s+commit:(?P<commit>\S+)\s+date:(?P<date>\S+)\s+go:(?P<go>\S+)\s+platform:(?P<platform>\S+)$`,
)

func parseVersionSummary(line string) (versionSummary, error) {
	line = strings.TrimSpace(line)
	m := versionSummaryRE.FindStringSubmatch(line)
	if m == nil {
		return versionSummary{}, fmt.Errorf("does not match canonical Summary() shape: %q", line)
	}
	return versionSummary{
		Version:   m[1],
		Commit:    m[2],
		Date:      m[3],
		GoVersion: m[4],
		Platform:  m[5],
	}, nil
}

func binCheck(opts runOptions) error {
	entries, err := os.ReadDir(opts.distDir)
	if err != nil {
		return fmt.Errorf("readdir dist: %w", err)
	}
	candidates := []string{"zen", "zen-swarm-ctld"}
	checked := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		match := false
		for _, c := range candidates {
			if name == c {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		bin := filepath.Join(opts.distDir, name)

		info, err := os.Stat(bin)
		if err != nil {
			return fmt.Errorf("stat %s: %w", bin, err)
		}
		if info.Mode()&0o111 == 0 {
			return fmt.Errorf("binary %s is not executable (mode %s)", bin, info.Mode())
		}
		out, err := exec.Command(bin, "--version").CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s --version: %w (output=%q)", name, err, string(out))
		}
		var summaryLine string
		for _, ln := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(strings.TrimSpace(ln), "zen-swarm ") {
				summaryLine = ln
				break
			}
		}
		if summaryLine == "" {
			return fmt.Errorf("%s --version emitted no buildinfo Summary line; output=%q", name, string(out))
		}
		if _, err := parseVersionSummary(summaryLine); err != nil {
			return fmt.Errorf("%s --version Summary line malformed: %w", name, err)
		}
		fmt.Fprintf(opts.stdout, "  OK  %s --version Summary parses cleanly\n", name)
		checked++
	}
	if checked == 0 {
		fmt.Fprintln(opts.stdout, "  WARN  --bin-check found no zen / zen-swarm-ctld binaries directly in dist/; skip")
	}
	return nil
}
