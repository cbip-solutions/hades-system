//go:build cgo
// +build cgo

package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const referenceTOMLBody = `[sources]
"pkg.go.dev"                = "7d"
"pypi.org"                  = "1d"
"registry.npmjs.org"        = "1d"
"crates.io"                 = "7d"
"developer.mozilla.org"     = "30d"
"arxiv.org"                 = "permanent"
"raw.githubusercontent.com" = "1d"
"api.github.com"            = "1d"
`

func TestLoadSourceTTLConfigCanonicalReference(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "source-ttls.toml")
	if err := os.WriteFile(path, []byte(referenceTOMLBody), 0o600); err != nil {
		t.Fatalf("write toml: %v", err)
	}

	cfg, err := LoadSourceTTLConfig(path)
	if err != nil {
		t.Fatalf("LoadSourceTTLConfig: %v", err)
	}

	wantTTLs := map[string]time.Duration{
		"pkg.go.dev":                7 * 24 * time.Hour,
		"pypi.org":                  24 * time.Hour,
		"registry.npmjs.org":        24 * time.Hour,
		"crates.io":                 7 * 24 * time.Hour,
		"developer.mozilla.org":     30 * 24 * time.Hour,
		"arxiv.org":                 TTLPermanent,
		"raw.githubusercontent.com": 24 * time.Hour,
		"api.github.com":            24 * time.Hour,
	}
	for host, want := range wantTTLs {
		got, ok := cfg.Sources[host]
		if !ok {
			t.Errorf("host %q missing from loaded config", host)
			continue
		}
		if got != want {
			t.Errorf("host %q TTL = %v; want %v", host, got, want)
		}
	}
	if len(cfg.Sources) != len(wantTTLs) {
		t.Errorf("cfg.Sources has %d entries; want %d", len(cfg.Sources), len(wantTTLs))
	}
}

func TestLoadSourceTTLConfigParsesDurationFormats(t *testing.T) {
	t.Parallel()
	tomlBody := `[sources]
"s.example.com"         = "30s"
"m.example.com"         = "5m"
"h.example.com"         = "2h"
"d.example.com"         = "3d"
"perm.example.com"      = "permanent"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "x.toml")
	if err := os.WriteFile(path, []byte(tomlBody), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := LoadSourceTTLConfig(path)
	if err != nil {
		t.Fatalf("LoadSourceTTLConfig: %v", err)
	}

	cases := []struct {
		host string
		want time.Duration
	}{
		{"s.example.com", 30 * time.Second},
		{"m.example.com", 5 * time.Minute},
		{"h.example.com", 2 * time.Hour},
		{"d.example.com", 3 * 24 * time.Hour},
		{"perm.example.com", TTLPermanent},
	}
	for _, c := range cases {
		got, ok := cfg.Sources[c.host]
		if !ok {
			t.Errorf("host %q missing", c.host)
			continue
		}
		if got != c.want {
			t.Errorf("host %q TTL = %v; want %v", c.host, got, c.want)
		}
	}
}

// TestLoadSourceTTLConfigInvalidDurationReturnsError verifies that an
// unrecognised duration string in the TOML file causes LoadSourceTTLConfig
// to return an error (fast-fail; do not silently skip bad entries).
func TestLoadSourceTTLConfigInvalidDurationReturnsError(t *testing.T) {
	t.Parallel()
	tomlBody := `[sources]
"bad.example.com" = "not-a-duration"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte(tomlBody), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadSourceTTLConfig(path); err == nil {
		t.Error("LoadSourceTTLConfig(bad duration): want error; got nil")
	}
}

func TestLoadSourceTTLConfigMissingFileGracefulDegradation(t *testing.T) {
	t.Parallel()
	cfg, err := LoadSourceTTLConfig("/nonexistent/path/source-ttls.toml")
	if err != nil {
		t.Fatalf("LoadSourceTTLConfig(missing): expected nil error, got %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadSourceTTLConfig(missing): expected non-nil config")
	}
	if cfg.Sources == nil {
		t.Error("missing-file config Sources is nil; want non-nil (default empty map)")
	}
}

func TestSourceTTLLookupFnConfigHitAndFallback(t *testing.T) {
	t.Parallel()
	cfg := &SourceTTLConfig{
		Sources: map[string]time.Duration{
			"pkg.go.dev": 7 * 24 * time.Hour,
		},
	}
	fn := cfg.LookupFn()

	if got := fn("https://pkg.go.dev/crypto/sha256"); got != 7*24*time.Hour {
		t.Errorf("pkg.go.dev TTL via LookupFn = %v; want 7d", got)
	}

	if got := fn("https://www.rfc-editor.org/rfc/rfc9162.html"); got != TTLPermanent {
		t.Errorf("rfc-editor fallback TTL = %v; want TTLPermanent (DefaultTTLRules)", got)
	}
}

func TestSourceTTLConfigCanonicalReferenceFileMatchesSpec(t *testing.T) {
	t.Parallel()
	cfg, err := LoadSourceTTLConfig("testdata/source-ttls.toml")
	if err != nil {
		t.Fatalf("LoadSourceTTLConfig(testdata): %v", err)
	}
	if len(cfg.Sources) != 8 {
		t.Errorf("canonical config has %d entries; want 8 (spec §2.8 table)", len(cfg.Sources))
	}
}

func TestLoadSourceTTLConfigInvalidTOMLSyntax(t *testing.T) {
	t.Parallel()
	tomlBody := `this is not valid toml ===`
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-syntax.toml")
	if err := os.WriteFile(path, []byte(tomlBody), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadSourceTTLConfig(path); err == nil {
		t.Error("LoadSourceTTLConfig(invalid TOML): want error; got nil")
	}
}

func TestParseTTLStringEmptyStringError(t *testing.T) {
	t.Parallel()
	tomlBody := `[sources]
"empty.example.com" = ""
`
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.toml")
	if err := os.WriteFile(path, []byte(tomlBody), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadSourceTTLConfig(path); err == nil {
		t.Error("LoadSourceTTLConfig(empty TTL): want error; got nil")
	}
}

func TestParseTTLStringZeroDurationError(t *testing.T) {
	t.Parallel()
	tomlBody := `[sources]
"zero.example.com" = "0s"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "zero.toml")
	if err := os.WriteFile(path, []byte(tomlBody), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadSourceTTLConfig(path); err == nil {
		t.Error("LoadSourceTTLConfig(0s TTL): want error; got nil")
	}
}

func TestParseTTLStringNegativeDurationError(t *testing.T) {
	t.Parallel()
	tomlBody := `[sources]
"neg.example.com" = "-1h"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "neg.toml")
	if err := os.WriteFile(path, []byte(tomlBody), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadSourceTTLConfig(path); err == nil {
		t.Error("LoadSourceTTLConfig(-1h TTL): want error; got nil")
	}
}

func TestParseTTLStringZeroDayCountError(t *testing.T) {
	t.Parallel()
	tomlBody := `[sources]
"zerod.example.com" = "0d"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "zerod.toml")
	if err := os.WriteFile(path, []byte(tomlBody), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadSourceTTLConfig(path); err == nil {
		t.Error("LoadSourceTTLConfig(0d TTL): want error; got nil")
	}
}

func TestParseTTLStringNonNumericDayPrefixError(t *testing.T) {
	t.Parallel()
	tomlBody := `[sources]
"nonnumeric.example.com" = "Xd"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "nonnumeric.toml")
	if err := os.WriteFile(path, []byte(tomlBody), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadSourceTTLConfig(path); err == nil {
		t.Error("LoadSourceTTLConfig(Xd TTL): want error; got nil")
	}
}

func TestSourceTTLLookupFnInvalidURLFallback(t *testing.T) {
	t.Parallel()
	cfg := &SourceTTLConfig{Sources: make(map[string]time.Duration)}
	fn := cfg.LookupFn()

	got := fn("://bad-url")
	if got <= 0 {
		t.Errorf("LookupFn(unparseable URL) = %v; want positive duration", got)
	}
}

func TestSourceTTLLookupFnEmptyURLFallback(t *testing.T) {
	t.Parallel()
	cfg := &SourceTTLConfig{Sources: make(map[string]time.Duration)}
	fn := cfg.LookupFn()

	got := fn("")
	if got <= 0 {
		t.Errorf("LookupFn(\"\") = %v; want positive duration (fallback)", got)
	}
}

func TestSourceTTLLookupFnEmptyConfigAllFallback(t *testing.T) {
	t.Parallel()
	cfg := &SourceTTLConfig{Sources: make(map[string]time.Duration)}
	fn := cfg.LookupFn()

	if got := fn("https://arxiv.org/abs/2509.17360"); got != TTLPermanent {
		t.Errorf("empty config LookupFn(arxiv.org) = %v; want TTLPermanent (DefaultTTLRules)", got)
	}
}

func TestLoadSourceTTLConfigReadErrorNonExist(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root; chmod 000 has no effect")
	}
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "unreadable.toml")
	if err := os.WriteFile(path, []byte(`[sources]`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(path, 0o600)

	if _, err := LoadSourceTTLConfig(path); err == nil {
		t.Error("LoadSourceTTLConfig(permission denied): want error; got nil")
	}
}
