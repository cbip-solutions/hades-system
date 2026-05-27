// go:build compliance
//go:build compliance
// +build compliance

package compliance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/transport"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func inv164RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func inv164ReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestInvZen164CompileAnchorPresent(t *testing.T) {
	root := inv164RepoRoot(t)
	src := inv164ReadFile(t, filepath.Join(root, "internal/daemon/transport/zenswarm_transport.go"))
	if !strings.Contains(src, "var _ providers.TierBackend = (*ZenSwarmTransport)(nil)") {
		t.Errorf(
			"inv-zen-164 compile-anchor missing in zenswarm_transport.go; " +
				"expected `var _ providers.TierBackend = (*ZenSwarmTransport)(nil)`",
		)
	}
}

func TestInvZen164SentinelAnchorReachable(t *testing.T) {
	err := transport.SingleEgressSentinel()
	if err == nil {
		t.Error("inv-zen-164: SingleEgressSentinel() returned nil; anchor must be reachable")
	}
	if err != nil && !strings.Contains(err.Error(), "inv-zen-164") {
		t.Errorf("inv-zen-164 sentinel error message must reference invariant name; got %q", err.Error())
	}
}

func TestInvZen164RuntimeRoutesViaDispatcher(t *testing.T) {
	disp := &testharness.ZenSwarmRecordingDispatcher{
		Resp: &providers.TierResponse{Status: 200, Body: []byte(`{"ok":true}`)},
	}
	zt := transport.NewZenSwarmTransport(disp, nil)
	resp, err := zt.Forward(context.Background(), providers.TierRequest{Body: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if resp == nil {
		t.Fatal("Forward returned nil response with nil error")
	}
	if disp.CallCount() != 1 {
		t.Errorf("dispatcher.Forward called %d times, want 1 (single-egress)", disp.CallCount())
	}
}

func TestInvZen164PythonSideUsesCanonicalEndpoint(t *testing.T) {
	root := inv164RepoRoot(t)
	src := inv164ReadFile(t, filepath.Join(root, "plugin/hades/transports/zen_swarm_transport.py"))
	if !strings.Contains(src, "/v1/messages") {
		t.Errorf("inv-zen-164: Python ZenSwarmTransport must POST /v1/messages")
	}
	if !strings.Contains(src, "class ZenSwarmTransport") {
		t.Errorf("inv-zen-164: Python class must declare `class ZenSwarmTransport`")
	}
	if !strings.Contains(src, `HEADER_TRANSPORT_SOURCE = "X-Zen-Transport"`) {
		t.Errorf("inv-zen-164: Python side must declare HEADER_TRANSPORT_SOURCE = \"X-Zen-Transport\"")
	}
	if !strings.Contains(src, `TRANSPORT_LABEL = "zenswarm"`) {
		t.Errorf("inv-zen-164: Python side must declare TRANSPORT_LABEL = \"zenswarm\"")
	}

	if strings.Contains(src, "\nProviderTransport = ZenSwarmTransport") ||
		strings.Contains(src, "\nProviderTransport=ZenSwarmTransport") {
		t.Errorf("inv-zen-164: misleading ``ProviderTransport = ZenSwarmTransport`` " +
			"alias must be dropped (deep-audit amendment 2026-05-15)")
	}
}

func TestInvZen164PluginNoSecondLLMPath(t *testing.T) {

	root := inv164RepoRoot(t)
	pluginRoot := filepath.Join(root, "plugin/hades")

	forbidden := []string{
		"api.anthropic.com",
		"openai.com",
		"generativelanguage.googleapis.com",
		"api.openai.com",
		"api.cohere.ai",
		"huggingface.co",
	}

	var violations []string
	err := filepath.Walk(pluginRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".py" {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(src)
		for _, host := range forbidden {
			if strings.Contains(text, host) {
				violations = append(violations, path+" contains forbidden host "+host)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk plugin/: %v", err)
	}
	if len(violations) > 0 {
		t.Errorf("inv-zen-164 single-egress violations:\n  %s", strings.Join(violations, "\n  "))
	}
}

func TestInvZen164DispatcherErrorPropagated(t *testing.T) {
	wantErr := errors.New("dispatcher-down")
	disp := &testharness.ZenSwarmRecordingDispatcher{Err: wantErr}
	zt := transport.NewZenSwarmTransport(disp, nil)
	_, err := zt.Forward(context.Background(), providers.TierRequest{})
	if !errors.Is(err, wantErr) {
		t.Errorf("dispatcher error must propagate (errors.Is); got %v", err)
	}
}

func TestInvZen164ProviderPluginRegistersProfile(t *testing.T) {
	root := inv164RepoRoot(t)
	providerInit := filepath.Join(root, "plugin/hades/providers/__init__.py")
	src := inv164ReadFile(t, providerInit)

	if !strings.Contains(src, "register_provider(") {
		t.Errorf("inv-zen-164: %s must call register_provider(profile)", providerInit)
	}

	if !strings.Contains(src, "ProviderProfile(") {
		t.Errorf("inv-zen-164: %s must instantiate ProviderProfile(...)", providerInit)
	}

	if !strings.Contains(src, `name="zen-swarm"`) && !strings.Contains(src, "name='zen-swarm'") {
		t.Errorf("inv-zen-164: %s must register ProviderProfile name=\"zen-swarm\"", providerInit)
	}

	if !strings.Contains(src, `api_mode="anthropic_messages"`) &&
		!strings.Contains(src, `api_mode='anthropic_messages'`) {
		t.Errorf("inv-zen-164: %s must set api_mode=\"anthropic_messages\" "+
			"so Hermes routes via daemon's NewAnthropicProxy format", providerInit)
	}

	// Anchor 5: base_url MUST point at a local-loopback HTTP URL by
	// default (override via env). An external hostname here would let
	// the operator's `hermes model zen` start talking to a foreign
	// service — never permissible under invariant single-egress.
	// We accept both literal default in source and env-var-override
	// markers (operator can configure ZEN_SWARM_BASE_URL).
	if !strings.Contains(src, "127.0.0.1") && !strings.Contains(src, "localhost") {
		t.Errorf("inv-zen-164: %s default base_url must point at "+
			"local-loopback (127.0.0.1 or localhost); got source without either", providerInit)
	}
	if !strings.Contains(src, "ZEN_SWARM_BASE_URL") {
		t.Errorf("inv-zen-164: %s must expose ZEN_SWARM_BASE_URL env "+
			"override for operator-friendly staging/team-daemon targeting", providerInit)
	}

	forbidden := []string{
		"api.anthropic.com",
		"api.openai.com",
		"generativelanguage.googleapis.com",
	}
	for _, host := range forbidden {
		if strings.Contains(src, host) {
			t.Errorf("inv-zen-164: %s contains forbidden upstream host %q; "+
				"would bypass single-egress on operator install", providerInit, host)
		}
	}
}

func TestInvZen164InstallMcpsWiresProviderConfig(t *testing.T) {
	root := inv164RepoRoot(t)
	cmdPath := filepath.Join(root, "plugin/hades/commands/install_mcps.py")
	src := inv164ReadFile(t, cmdPath)

	if !strings.Contains(src, "_install_provider_plugin_symlink") {
		t.Errorf("inv-zen-164: %s must define _install_provider_plugin_symlink "+
			"helper (symlinks <repo>/plugin/hades/providers -> "+
			"$HERMES_HOME/plugins/model-providers/zen-swarm)", cmdPath)
	}
	if !strings.Contains(src, "plugins/model-providers") {
		t.Errorf("inv-zen-164: %s must reference Hermes' plugins/model-providers/ "+
			"discovery path (see providers/__init__.py:_import_plugin_dir)", cmdPath)
	}

	if !strings.Contains(src, "_update_hermes_config_provider") {
		t.Errorf("inv-zen-164: %s must define _update_hermes_config_provider "+
			"helper that writes model.provider/model.base_url into config.yaml", cmdPath)
	}
	if !strings.Contains(src, `"provider"`) || !strings.Contains(src, `"base_url"`) {
		t.Errorf("inv-zen-164: %s must reference model.provider + model.base_url keys", cmdPath)
	}

	if !strings.Contains(src, "_install_provider_plugin_symlink(hermes_home)") {
		t.Errorf("inv-zen-164: handle_install_mcps in %s must invoke "+
			"_install_provider_plugin_symlink(hermes_home) in live mode", cmdPath)
	}
	if !strings.Contains(src, "_update_hermes_config_provider(hermes_home, base_url)") {
		t.Errorf("inv-zen-164: handle_install_mcps in %s must invoke "+
			"_update_hermes_config_provider(hermes_home, base_url) in live mode", cmdPath)
	}

	if !strings.Contains(src, "DEFAULT_BASE_URL") {
		t.Errorf("inv-zen-164: %s must declare DEFAULT_BASE_URL constant", cmdPath)
	}
	if !strings.Contains(src, "DEFAULT_DAEMON_BASE_URL") {
		t.Errorf("inv-zen-164: %s must import DEFAULT_DAEMON_BASE_URL "+
			"from plugin/hades/_constants.py (single source of truth, M1)", cmdPath)
	}
	if !strings.Contains(src, "ZEN_SWARM_BASE_URL") {
		t.Errorf("inv-zen-164: %s must consume ZEN_SWARM_BASE_URL env override", cmdPath)
	}

	constantsPath := filepath.Join(root, "plugin/hades/_constants.py")
	constantsSrc := inv164ReadFile(t, constantsPath)
	if !strings.Contains(constantsSrc, "127.0.0.1") && !strings.Contains(constantsSrc, "localhost") {
		t.Errorf("inv-zen-164: %s default base_url must be local-loopback "+
			"(127.0.0.1 or localhost)", constantsPath)
	}
	if !strings.Contains(constantsSrc, "DEFAULT_DAEMON_BASE_URL") {
		t.Errorf("inv-zen-164: %s must declare DEFAULT_DAEMON_BASE_URL "+
			"as the canonical daemon URL constant", constantsPath)
	}
}
