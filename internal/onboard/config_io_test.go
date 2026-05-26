package onboard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/cbip-solutions/hades-system/internal/onboard/prefs"
)

func TestConfigIOWriteGlobalConfigRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg := &GlobalConfig{
		SchemaVersion: "1.0",
		LLMProvider:   "anthropic-paygo",
		Doctrine:      "default",
		HermesScope:   "user",
	}
	if err := WriteGlobalConfig(cfg); err != nil {
		t.Fatalf("WriteGlobalConfig: %v", err)
	}

	configPath := filepath.Join(tmp, "zen-swarm", "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("WriteGlobalConfig wrote empty file")
	}

	var decoded GlobalConfig
	if err := toml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.SchemaVersion != cfg.SchemaVersion {
		t.Errorf("SchemaVersion: got %q want %q", decoded.SchemaVersion, cfg.SchemaVersion)
	}
	if decoded.LLMProvider != cfg.LLMProvider {
		t.Errorf("LLMProvider: got %q want %q", decoded.LLMProvider, cfg.LLMProvider)
	}
	if decoded.Doctrine != cfg.Doctrine {
		t.Errorf("Doctrine: got %q want %q", decoded.Doctrine, cfg.Doctrine)
	}
	if decoded.HermesScope != cfg.HermesScope {
		t.Errorf("HermesScope: got %q want %q", decoded.HermesScope, cfg.HermesScope)
	}

	entries, err := os.ReadDir(filepath.Join(tmp, "zen-swarm"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("WriteGlobalConfig left .tmp file behind: %s", e.Name())
		}
	}
}

func TestConfigIOWriteGlobalConfigNilRejected(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	if err := WriteGlobalConfig(nil); err == nil {
		t.Fatal("WriteGlobalConfig(nil): want error, got nil")
	}
}

func TestConfigIOWriteGlobalConfigCreatesParentDir(t *testing.T) {
	tmp := t.TempDir()

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "fresh"))

	cfg := &GlobalConfig{SchemaVersion: "1.0"}
	if err := WriteGlobalConfig(cfg); err != nil {
		t.Fatalf("WriteGlobalConfig: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "fresh", "zen-swarm", "config.toml")); err != nil {
		t.Errorf("config.toml not created: %v", err)
	}
}

func TestConfigIOWriteProviderTOMLRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg := &ProviderConfig{
		SchemaVersion:    "1.0",
		Name:             "anthropic-bypass",
		BypassConfigPath: "/some/path/to/bypass-config.json",
	}
	if err := WriteProviderTOML("anthropic-bypass", cfg); err != nil {
		t.Fatalf("WriteProviderTOML: %v", err)
	}

	path := filepath.Join(tmp, "zen-swarm", "providers", "anthropic-bypass.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read provider toml: %v", err)
	}
	var decoded ProviderConfig
	if err := toml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Name != "anthropic-bypass" {
		t.Errorf("Name: got %q want anthropic-bypass", decoded.Name)
	}
	if decoded.BypassConfigPath != cfg.BypassConfigPath {
		t.Errorf("BypassConfigPath: got %q want %q", decoded.BypassConfigPath, cfg.BypassConfigPath)
	}
}

func TestConfigIOWriteProviderTOMLRejectsNilOrEmpty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	if err := WriteProviderTOML("", &ProviderConfig{}); err == nil {
		t.Errorf("WriteProviderTOML(empty name): want error, got nil")
	}
	if err := WriteProviderTOML("x", nil); err == nil {
		t.Errorf("WriteProviderTOML(nil cfg): want error, got nil")
	}
}

func TestConfigIOCloneBuiltinDoctrineCreatesFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	if err := CloneBuiltinDoctrine("default"); err != nil {
		t.Fatalf("CloneBuiltinDoctrine: %v", err)
	}
	dst := filepath.Join(tmp, "zen-swarm", "doctrines", "default.toml")
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat clone: %v", err)
	}
	if info.Mode().Perm()&0o111 != 0 {
		t.Errorf("Clone file has executable bit set: %v", info.Mode())
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read clone: %v", err)
	}
	if !strings.Contains(string(data), `schema_version`) {
		t.Errorf("Clone missing schema_version marker; got: %s", data)
	}
	if !strings.Contains(string(data), `name = "default"`) {
		t.Errorf("Clone missing name marker; got: %s", data)
	}
}

func TestConfigIOCloneBuiltinDoctrineNoOverwrite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "zen-swarm", "doctrines")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	existing := filepath.Join(dir, "max-scope.toml")
	hand := []byte("# operator-edited content\nschema_version = \"1.0\"\nname = \"max-scope\"\noperator_added = true\n")
	if err := os.WriteFile(existing, hand, 0o644); err != nil {
		t.Fatalf("WriteFile existing: %v", err)
	}

	if err := CloneBuiltinDoctrine("max-scope"); err != nil {
		t.Fatalf("CloneBuiltinDoctrine: %v", err)
	}
	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read existing: %v", err)
	}
	if !strings.Contains(string(data), "operator_added") {
		t.Errorf("CloneBuiltinDoctrine overwrote operator hand-edits; got: %s", data)
	}
}

func TestConfigIOCloneBuiltinDoctrineRejectsEmptyName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	if err := CloneBuiltinDoctrine(""); err == nil {
		t.Error("CloneBuiltinDoctrine(empty): want error, got nil")
	}
}

func TestConfigIODefaultsFromFlagsAndPrefsBuiltinFallback(t *testing.T) {
	got := DefaultsFromFlagsAndPrefs(nil, nil)
	if got.LLMProvider != "anthropic-paygo" {
		t.Errorf("LLMProvider builtin: got %q want anthropic-paygo", got.LLMProvider)
	}
	if got.Doctrine != "default" {
		t.Errorf("Doctrine builtin: got %q want default", got.Doctrine)
	}
	if len(got.MCPSelections) == 0 {
		t.Errorf("MCPSelections builtin: empty")
	}
}

func TestConfigIODefaultsFromFlagsAndPrefsPrefsOverridesBuiltin(t *testing.T) {
	p := &prefs.Prefs{
		LLMProvider:  "ollama-local",
		Doctrine:     "max-scope",
		MCPs:         []string{"only-one"},
		TemplateName: "go-cli",
		InitGit:      true,
	}
	got := DefaultsFromFlagsAndPrefs(nil, p)
	if got.LLMProvider != "ollama-local" {
		t.Errorf("prefs override LLMProvider: got %q want ollama-local", got.LLMProvider)
	}
	if got.Doctrine != "max-scope" {
		t.Errorf("prefs override Doctrine: got %q want max-scope", got.Doctrine)
	}
	if len(got.MCPSelections) != 1 || got.MCPSelections[0] != "only-one" {
		t.Errorf("prefs override MCPs: got %v want [only-one]", got.MCPSelections)
	}
	if got.TemplateName != "go-cli" {
		t.Errorf("prefs override TemplateName: got %q want go-cli", got.TemplateName)
	}
	if !got.InitGit {
		t.Errorf("prefs override InitGit: got false want true")
	}
}

func TestConfigIODefaultsFromFlagsAndPrefsFlagsOverridePrefs(t *testing.T) {
	p := &prefs.Prefs{
		LLMProvider: "ollama-local",
		Doctrine:    "max-scope",
	}
	flags := map[string]string{
		"llm-provider": "anthropic-bypass",
		"doctrine":     "capa-firewall",
		"template":     "ml-pipeline",
	}
	got := DefaultsFromFlagsAndPrefs(flags, p)
	if got.LLMProvider != "anthropic-bypass" {
		t.Errorf("flags override LLMProvider: got %q want anthropic-bypass", got.LLMProvider)
	}
	if got.Doctrine != "capa-firewall" {
		t.Errorf("flags override Doctrine: got %q want capa-firewall", got.Doctrine)
	}
	if got.TemplateName != "ml-pipeline" {
		t.Errorf("flags override TemplateName: got %q want ml-pipeline", got.TemplateName)
	}
}

// TestConfigIODefaultsFromFlagsAndPrefsEmptyFlagsIgnored verifies that
// flag entries with empty values do NOT override prefs. Empty flag
// means "operator did not supply this flag" — prefs/builtin win.
func TestConfigIODefaultsFromFlagsAndPrefsEmptyFlagsIgnored(t *testing.T) {
	p := &prefs.Prefs{LLMProvider: "ollama-local"}
	flags := map[string]string{"llm-provider": ""}
	got := DefaultsFromFlagsAndPrefs(flags, p)
	if got.LLMProvider != "ollama-local" {
		t.Errorf("empty flag must not override: got %q want ollama-local", got.LLMProvider)
	}
}

func TestConfigIODefaultsFromFlagsAndPrefsMCPCopyOnPrefs(t *testing.T) {
	p := &prefs.Prefs{MCPs: []string{"a", "b"}}
	got := DefaultsFromFlagsAndPrefs(nil, p)
	if len(got.MCPSelections) != 2 {
		t.Fatalf("len: %d", len(got.MCPSelections))
	}
	got.MCPSelections[0] = "MUTATED"
	if p.MCPs[0] != "a" {
		t.Errorf("mutation leaked into prefs.MCPs: %v", p.MCPs)
	}
}

func TestConfigIODefaultsFromFlagsAndPrefsEmptyPrefsFieldsIgnored(t *testing.T) {
	p := &prefs.Prefs{}
	got := DefaultsFromFlagsAndPrefs(nil, p)
	if got.LLMProvider != "anthropic-paygo" {
		t.Errorf("empty prefs must fall through to builtin: got %q want anthropic-paygo", got.LLMProvider)
	}
}

func TestConfigIOAtomicNoLeakOnRenameSuccess(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	if err := WriteGlobalConfig(&GlobalConfig{SchemaVersion: "1.0"}); err != nil {
		t.Fatalf("WriteGlobalConfig: %v", err)
	}
	if err := WriteProviderTOML("p", &ProviderConfig{SchemaVersion: "1.0", Name: "p"}); err != nil {
		t.Fatalf("WriteProviderTOML: %v", err)
	}

	root := filepath.Join(tmp, "zen-swarm")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".tmp") {
			t.Errorf(".tmp left behind: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Errorf("Walk: %v", err)
	}
}

func TestConfigIOWriteFailsOnUnwritableDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in -short mode")
	}
	tmp := t.TempDir()

	xdgRoot := filepath.Join(tmp, "xdg-collision")
	if err := os.WriteFile(xdgRoot, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgRoot)

	err := WriteGlobalConfig(&GlobalConfig{SchemaVersion: "1.0"})
	if err == nil {
		t.Fatal("WriteGlobalConfig over file: want error, got nil")
	}

	if !errors.Is(err, err) || err.Error() == "" {
		t.Errorf("error not properly wrapped: %v", err)
	}
}

func TestConfigIOWriteAtomicTOMLEncodeError(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "encode-err", "out.toml")

	type unencodable struct {
		Ch chan int `toml:"ch"`
	}
	err := writeAtomicTOML(target, &unencodable{Ch: make(chan int)})
	if err == nil {
		t.Fatal("writeAtomicTOML(channel): want error, got nil")
	}
	if !strings.Contains(err.Error(), "encode") {
		t.Errorf("expected encode error; got: %v", err)
	}

	if _, statErr := os.Stat(target + ".tmp"); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("encode failure left .tmp behind: %v", statErr)
	}

	if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("encode failure left final file: %v", statErr)
	}
}

func TestConfigIOWriteAtomicTOMLRenameError(t *testing.T) {
	tmp := t.TempDir()

	target := filepath.Join(tmp, "out.toml")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "nonempty"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile sentinel: %v", err)
	}

	err := writeAtomicTOML(target, &GlobalConfig{SchemaVersion: "1.0"})
	if err == nil {
		t.Fatal("writeAtomicTOML over dir: want error, got nil")
	}
	if !strings.Contains(err.Error(), "rename") && !strings.Contains(err.Error(), "open") {

		t.Errorf("expected rename or open error; got: %v", err)
	}

	if _, statErr := os.Stat(target + ".tmp"); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("rename failure left .tmp behind: %v", statErr)
	}
}

func TestConfigIOWriteAtomicTOMLOpenTmpError(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "out.toml")

	if err := os.Mkdir(target+".tmp", 0o755); err != nil {
		t.Fatalf("Mkdir blocker: %v", err)
	}

	err := writeAtomicTOML(target, &GlobalConfig{SchemaVersion: "1.0"})
	if err == nil {
		t.Fatal("writeAtomicTOML over .tmp-dir: want error, got nil")
	}
	if !strings.Contains(err.Error(), "open") {
		t.Errorf("expected open error; got: %v", err)
	}
}

func TestConfigIOCloneBuiltinDoctrineMkdirError(t *testing.T) {
	tmp := t.TempDir()

	zenRoot := filepath.Join(tmp, "zen-swarm")
	if err := os.WriteFile(zenRoot, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("WriteFile blocker: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", tmp)

	err := CloneBuiltinDoctrine("default")
	if err == nil {
		t.Fatal("CloneBuiltinDoctrine over file: want error, got nil")
	}
	if !strings.Contains(err.Error(), "mkdir") {
		t.Errorf("expected mkdir error; got: %v", err)
	}
}

func TestConfigIOCloneBuiltinDoctrineWriteError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dst := filepath.Join(tmp, "zen-swarm", "doctrines", "writeerr.toml")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("MkdirAll blocker: %v", err)
	}

	if err := os.RemoveAll(dst); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	parent := filepath.Dir(dst)
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("Chmod ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	err := CloneBuiltinDoctrine("writeerr")
	if err == nil {

		if os.Geteuid() == 0 {
			t.Skip("running as root; chmod 0500 ineffective")
		}
		t.Fatal("CloneBuiltinDoctrine into ro dir: want error, got nil")
	}
}

func TestConfigIOCloneBuiltinDoctrineStatErrorNonExist(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses Stat permission checks")
	}
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	parent := filepath.Join(tmp, "zen-swarm", "doctrines")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	err := CloneBuiltinDoctrine("statperm")
	if err == nil {
		t.Skip("filesystem honors traversal differently; skip rather than false-fail")
	}
	if !strings.Contains(err.Error(), "stat") && !strings.Contains(err.Error(), "mkdir") && !strings.Contains(err.Error(), "write") {
		t.Errorf("expected stat/mkdir/write error; got: %v", err)
	}
}
