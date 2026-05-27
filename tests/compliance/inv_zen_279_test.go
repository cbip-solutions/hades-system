package compliance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/keychain"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/internal/redact"
)

func repoRoot279(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found (no go.mod in ancestors)")
		}
		dir = parent
	}
	t.Fatalf("repo root not found within 8 levels of %s", dir)
	return ""
}

func readSource279(t *testing.T, relPath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot279(t), relPath))
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	return string(data)
}

// TestInvZen279_SchemaUsesRawMessage pins the canonical schema extension
// from The frozen contract requires System on canonicalRequest
// and Content on canonicalMessage to be json.RawMessage (so the wire
// body can carry either a string or a content-block array). A revert to
// `string` MUST fail this gate.
func TestInvZen279_SchemaUsesRawMessage(t *testing.T) {
	src := readSource279(t, filepath.Join("internal", "providers", "translate.go"))
	if !strings.Contains(src, "System      json.RawMessage") {
		t.Error("inv-zen-279: canonicalRequest.System must be json.RawMessage (sister-test for schema extension)")
	}
	if !strings.Contains(src, "Content json.RawMessage") {
		t.Error("inv-zen-279: canonicalMessage.Content must be json.RawMessage (sister-test for schema extension)")
	}

	if !strings.Contains(src, "type contentBlock struct") {
		t.Error("inv-zen-279: contentBlock type missing")
	}

	if !strings.Contains(src, "Tools       []json.RawMessage") {
		t.Error("inv-zen-279: canonicalRequest.Tools must be []json.RawMessage")
	}
}

func TestInvZen279_HelpersDefined(t *testing.T) {
	src := readSource279(t, filepath.Join("internal", "providers", "translate.go"))
	for _, name := range []string{
		"func resolveSystemText",
		"func resolveContentText",
		"func hasToolsField",
	} {
		if !strings.Contains(src, name) {
			t.Errorf("inv-zen-279: %q missing in translate.go", name)
		}
	}
}

func TestInvZen279_ErrToolsUnsupportedDefined(t *testing.T) {
	src := readSource279(t, filepath.Join("internal", "providers", "errors.go"))
	if !strings.Contains(src, "var ErrToolsUnsupported = errors.New") {
		t.Error("inv-zen-279: ErrToolsUnsupported sentinel missing from internal/providers/errors.go")
	}
}

func TestInvZen279_BackendsRejectTools(t *testing.T) {
	for _, rel := range []string{
		filepath.Join("internal", "providers", "openai_compat_backend.go"),
		filepath.Join("internal", "providers", "gemini_backend.go"),
		filepath.Join("internal", "providers", "ollama_backend.go"),
	} {
		src := readSource279(t, rel)
		if !strings.Contains(src, "hasToolsField(") {
			t.Errorf("inv-zen-279: %s must invoke hasToolsField()", rel)
		}
		if !strings.Contains(src, "ErrToolsUnsupported") {
			t.Errorf("inv-zen-279: %s must return ErrToolsUnsupported", rel)
		}
	}
}

func TestInvZen279_DispatcherSkipsBreakerOnToolsUnsupported(t *testing.T) {
	src := readSource279(t, filepath.Join("internal", "daemon", "dispatcher", "dispatcher.go"))
	if !strings.Contains(src, "errors.Is(err, providers.ErrToolsUnsupported)") {
		t.Error("inv-zen-279: dispatcher must short-circuit on providers.ErrToolsUnsupported (capability mismatch path)")
	}
}

type keychainStub struct{}

func (keychainStub) Lookup(_ string, _ string) (redact.Secret, error) {
	return redact.NewSecret("test-key"), nil
}

var _ keychain.Resolver = keychainStub{}

func TestInvZen279_BehaviouralNarrowDown(t *testing.T) {
	cases := []struct {
		name             string
		body             string
		wantContentSub   string
		wantSystemSub    string
		wantNoCacheCtrl  bool
		wantToolsRefused bool
	}{
		{
			name:            "string-system-string-content",
			body:            `{"model":"x","max_tokens":1,"system":"sysmsg","messages":[{"role":"user","content":"hello"}]}`,
			wantSystemSub:   "sysmsg",
			wantContentSub:  "hello",
			wantNoCacheCtrl: true,
		},
		{
			name:            "string-system-block-content",
			body:            `{"model":"x","max_tokens":1,"system":"sysmsg","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`,
			wantSystemSub:   "sysmsg",
			wantContentSub:  "hello",
			wantNoCacheCtrl: true,
		},
		{
			name:            "block-system-string-content",
			body:            `{"model":"x","max_tokens":1,"system":[{"type":"text","text":"sysmsg"}],"messages":[{"role":"user","content":"hello"}]}`,
			wantSystemSub:   "sysmsg",
			wantContentSub:  "hello",
			wantNoCacheCtrl: true,
		},
		{
			name: "full-hermes-with-cache-control",
			body: `{"model":"x","max_tokens":1,
				"system":[{"type":"text","text":"sysmsg","cache_control":{"type":"ephemeral"}}],
				"messages":[{"role":"user","content":[
					{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}
				]}]}`,
			wantSystemSub:   "sysmsg",
			wantContentSub:  "hello",
			wantNoCacheCtrl: true,
		},
		{
			name: "tools-field-rejected",
			body: `{"model":"x","max_tokens":1,
				"tools":[{"name":"weather","input_schema":{}}],
				"messages":[{"role":"user","content":"hi"}]}`,
			wantToolsRefused: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var receivedBody []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				receivedBody = body
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": "chatcmpl-c", "model": "test-model",
					"choices": []any{map[string]any{
						"message":       map[string]any{"role": "assistant", "content": "ok"},
						"finish_reason": "stop",
					}},
					"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
				})
			}))
			defer srv.Close()

			backend, err := providers.NewOpenAICompatBackend(providers.ProviderConfig{
				Name: "test-direct", Type: "openai-compat", Endpoint: srv.URL,
				Model: "test-model", Family: "test", APIKeyKeychain: "zen-swarm/test",
			}, keychainStub{})
			if err != nil {
				t.Fatalf("NewOpenAICompatBackend: %v", err)
			}
			resp, err := backend.Forward(context.Background(), providers.TierRequest{
				Body:  []byte(tc.body),
				Model: "test-model",
			})

			if tc.wantToolsRefused {
				if !errors.Is(err, providers.ErrToolsUnsupported) {
					t.Fatalf("want ErrToolsUnsupported; got err=%v resp=%+v", err, resp)
				}
				if receivedBody != nil {
					t.Errorf("backend MUST short-circuit (no HTTP call) on tools field; got body %s", string(receivedBody))
				}
				return
			}
			if err != nil {
				t.Fatalf("Forward: %v", err)
			}
			if resp == nil || resp.Status != 200 {
				t.Fatalf("expected 200 response; got %+v", resp)
			}
			if tc.wantNoCacheCtrl && bytes.Contains(receivedBody, []byte("cache_control")) {
				t.Errorf("cache_control leaked to OpenAI body: %s", string(receivedBody))
			}
			if tc.wantSystemSub != "" && !bytes.Contains(receivedBody, []byte(tc.wantSystemSub)) {
				t.Errorf("system substring %q missing from received body: %s", tc.wantSystemSub, string(receivedBody))
			}
			if tc.wantContentSub != "" && !bytes.Contains(receivedBody, []byte(tc.wantContentSub)) {
				t.Errorf("content substring %q missing from received body: %s", tc.wantContentSub, string(receivedBody))
			}
		})
	}
}
