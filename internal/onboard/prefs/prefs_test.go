package prefs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrefsLoadMissingReturnsZero(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if p == nil {
		t.Fatal("Load missing: returned nil; want zero-Prefs")
	}
	if p.SchemaVersion != "" {
		t.Errorf("Load missing: SchemaVersion = %q, want empty (caller sets on Save)", p.SchemaVersion)
	}
}

func TestPrefsSaveAndLoadRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")

	in := &Prefs{
		Doctrine:           "max-scope",
		DoctrineSource:     "built-in",
		LLMProvider:        "anthropic-paygo",
		BypassConfigPath:   "/tmp/bypass.json",
		OllamaEndpoint:     "http://localhost:11434",
		CustomProviderURL:  "https://example.com/v1",
		AuditRetentionDays: 30,
		GitConfigName:      "testuser",
		GitConfigEmail:     "testuser@example.com",
		InstallHermes:      true,
		EnableAuditChain:   true,
		ProjectKind:        "go-cli",
		TemplateName:       "embedded://go-cli",
		TemplateVersion:    "v1.0.0",
		MCPs:               []string{"zen-swarm-ctld", "playwright"},
		InitGit:            true,
		LinkHermesPlugin:   true,
		PingDaemon:         true,
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("SchemaVersion: got %q, want %q", out.SchemaVersion, CurrentSchemaVersion)
	}
	if out.LLMProvider != "anthropic-paygo" {
		t.Errorf("LLMProvider mismatch: %q", out.LLMProvider)
	}
	if out.TemplateName != "embedded://go-cli" {
		t.Errorf("TemplateName mismatch: %q", out.TemplateName)
	}
	if len(out.MCPs) != 2 || out.MCPs[0] != "zen-swarm-ctld" || out.MCPs[1] != "playwright" {
		t.Errorf("MCPs mismatch: %v", out.MCPs)
	}
	if !out.InstallHermes || !out.EnableAuditChain {
		t.Errorf("bool flags mismatch: %+v", out)
	}
	if out.AuditRetentionDays != 30 {
		t.Errorf("AuditRetentionDays mismatch: %d", out.AuditRetentionDays)
	}
}

func TestPrefsSaveAlwaysStampsCurrentSchemaVersion(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	in := &Prefs{SchemaVersion: "0.9", LLMProvider: "x"}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("Save did not stamp CurrentSchemaVersion: got %q", out.SchemaVersion)
	}
}

func TestPrefsSaveNilReturnsError(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	err := Save(path, nil)
	if err == nil {
		t.Fatal("Save(nil): want error, got nil")
	}
}

func TestPrefsSaveAtomicNoTmpLeftBehind(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	if err := Save(path, &Prefs{LLMProvider: "x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("Save left .tmp file behind: %s", e.Name())
		}
	}
}

func TestPrefsSaveDirsCreated(t *testing.T) {
	tmp := t.TempDir()

	path := filepath.Join(tmp, "nested", "dir", "onboard-prefs.toml")
	if err := Save(path, &Prefs{LLMProvider: "x"}); err != nil {
		t.Fatalf("Save nested: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("Save nested: file missing: %v", err)
	}
}

func TestPrefsSaveMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits not enforced on windows")
	}
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	if err := Save(path, &Prefs{LLMProvider: "x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("Mode: got %o, want 0600", got)
	}
}

func TestPrefsLoadRejectsMajorSchemaBump(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	content := `schema_version = "2.0"
llm_provider = "x"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("Load major-bump: want ErrUnsupportedSchemaVersion, got %v", err)
	}
}

func TestPrefsLoadAcceptsMinorSchemaBump(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")

	content := `schema_version = "1.99"
llm_provider = "anthropic-paygo"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load minor-bump: %v", err)
	}
	if p.LLMProvider != "anthropic-paygo" {
		t.Errorf("Load minor-bump: %+v", p)
	}
}

func TestPrefsLoadRejectsMalformedSchemaVersion(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	content := `schema_version = "not-a-version"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrCorruptPrefs) {
		t.Fatalf("Load malformed schema_version: want ErrCorruptPrefs, got %v", err)
	}
}

func TestPrefsLoadCorruptBacksUpAndErrors(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	if err := os.WriteFile(path, []byte("this is not toml { { broken"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrCorruptPrefs) {
		t.Fatalf("Load corrupt: want ErrCorruptPrefs, got %v", err)
	}

	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	foundBackup := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "onboard-prefs.toml.corrupt-") {
			foundBackup = true

			b, rerr := os.ReadFile(filepath.Join(tmp, e.Name()))
			if rerr != nil {
				t.Fatalf("ReadFile backup: %v", rerr)
			}
			if string(b) != "this is not toml { { broken" {
				t.Errorf("backup mismatch: %q", string(b))
			}
		}
	}
	if !foundBackup {
		t.Errorf("Load corrupt: backup file not created; entries=%v", entries)
	}
}

func TestPrefsLoadCorruptBackupInUnwritableDirReturnsCombinedError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX chmod-deny semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses 0o500 directory perm; cannot exercise backup-write-failure branch")
	}
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	if err := os.WriteFile(path, []byte("not toml { broken"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := os.Chmod(tmp, 0o500); err != nil {
		t.Fatalf("Chmod tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmp, 0o700) })

	_, err := Load(path)
	if !errors.Is(err, ErrCorruptPrefs) {
		t.Fatalf("Load corrupt + backup-fail: want errors.Is ErrCorruptPrefs, got %v", err)
	}
	if !strings.Contains(err.Error(), "backup failed") {
		t.Errorf("expected error to mention backup failure, got %v", err)
	}
}

func TestPrefsLoadHonorsXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test-config")
	got := Path()
	want := filepath.Join("/tmp/xdg-test-config", "zen-swarm", "onboard-prefs.toml")
	if got != want {
		t.Errorf("Path() with XDG_CONFIG_HOME: got %q, want %q", got, want)
	}
}

func TestPrefsLoadDefaultPathHasZenSwarmSubdir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home-test")
	got := Path()
	want := filepath.Join("/tmp/home-test", ".config", "zen-swarm", "onboard-prefs.toml")
	if got != want {
		t.Errorf("Path() default: got %q, want %q", got, want)
	}
}

func TestPrefsStructHasNoSecretFields(t *testing.T) {

	forbiddenNames := []string{"AnthropicAPIKey", "CustomProviderAuth", "APIKey", "Auth", "Token", "Secret", "Password"}
	p := Prefs{}

	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	if err := Save(path, &p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lower := strings.ToLower(string(data))
	for _, n := range forbiddenNames {
		if strings.Contains(lower, strings.ToLower(n)) {
			t.Errorf("Prefs encoded TOML contains forbidden secret-suggesting key %q: %s", n, string(data))
		}
	}
}

func TestPrefsLoadReadFailureNonNotExistPropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX chmod-deny semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses 0o000 file perm; cannot exercise read-failure branch")
	}
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	if err := os.WriteFile(path, []byte("schema_version = \"1.0\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load: want error on unreadable file, got nil")
	}
	if errors.Is(err, ErrCorruptPrefs) || errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Errorf("Load: read failure should NOT be conflated with corrupt/schema; got %v", err)
	}
}

func TestPrefsSaveOpenFailurePropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX chmod-deny semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses 0o000 directory perm; cannot exercise mkdir-failure branch")
	}
	tmp := t.TempDir()

	if err := os.Chmod(tmp, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmp, 0o700) })

	path := filepath.Join(tmp, "nested", "onboard-prefs.toml")
	err := Save(path, &Prefs{LLMProvider: "x"})
	if err == nil {
		t.Fatal("Save: want error on unwritable parent, got nil")
	}
}

func TestPrefsSaveWriteTmpFailurePropagates(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")

	if err := os.Mkdir(path+".tmp", 0o755); err != nil {
		t.Fatalf("Mkdir collision: %v", err)
	}
	err := Save(path, &Prefs{LLMProvider: "x"})
	if err == nil {
		t.Fatal("Save: want error when <path>.tmp is a directory, got nil")
	}
	if !strings.Contains(err.Error(), "write tmp") {
		t.Errorf("Save: expected write-tmp error wrapper, got %v", err)
	}
}

func TestPrefsSaveRenameFailurePropagates(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")

	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir collision: %v", err)
	}

	if err := os.WriteFile(filepath.Join(path, "marker"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile marker: %v", err)
	}
	err := Save(path, &Prefs{LLMProvider: "x"})
	if err == nil {
		t.Fatal("Save: want error when canonical path is a non-empty dir, got nil")
	}

	if _, statErr := os.Stat(path + ".tmp"); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("Save: .tmp not cleaned up on rename failure: statErr=%v", statErr)
	}
}

func TestValidateSchemaVersionEmptyPasses(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	if err := os.WriteFile(path, []byte("llm_provider = \"x\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load empty-schema_version: want nil error, got %v", err)
	}
	if p.LLMProvider != "x" {
		t.Errorf("Load empty-schema_version: payload not preserved: %+v", p)
	}
}

func TestParseSchemaVersionMalformedMajor(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	if err := os.WriteFile(path, []byte("schema_version = \"x.0\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrCorruptPrefs) {
		t.Fatalf("Load malformed major: want ErrCorruptPrefs, got %v", err)
	}
}

func TestParseSchemaVersionMalformedMinor(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "onboard-prefs.toml")
	if err := os.WriteFile(path, []byte("schema_version = \"1.x\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrCorruptPrefs) {
		t.Fatalf("Load malformed minor: want ErrCorruptPrefs, got %v", err)
	}
}

func TestMustParseMajorPanicsOnMalformed(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("mustParseMajor: expected panic on malformed input")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("mustParseMajor: expected string panic, got %T: %v", r, r)
		}
		if !strings.Contains(msg, "malformed") {
			t.Errorf("mustParseMajor panic msg: %q", msg)
		}
	}()
	_ = mustParseMajor("not-a-version")
}

func TestPathUserHomeDirFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	switch runtime.GOOS {
	case "windows":

		t.Setenv("USERPROFILE", "")
		t.Setenv("HOMEDRIVE", "")
		t.Setenv("HOMEPATH", "")
	case "plan9":
		t.Setenv("home", "")
	default:

		t.Setenv("HOME", "")
	}
	got := Path()

	wantSuffix := filepath.Join(".config", "zen-swarm", "onboard-prefs.toml")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("Path() fallback: got %q, want suffix %q", got, wantSuffix)
	}
}
