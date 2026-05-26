package compliance_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

var disallowedHosts = []struct {
	name string
	url  string
}{
	{"random domain", "http://example.com/data"},
	{"attacker domain", "http://evil.attacker.com/exfil"},
	{"internal loopback alt", "http://127.0.0.2/secret"},
	{"internal RFC1918 10.x", "http://10.0.0.1/admin"},
	{"internal RFC1918 192.168.x", "http://192.168.1.1/config"},
	{"internal RFC1918 172.16.x", "http://172.16.0.1/secrets"},
	{"cloud metadata AWS", "http://169.254.169.254/latest/meta-data/"},
	{"cloud metadata GCP", "http://metadata.google.internal/computeMetadata/"},
	{"typosquat arxiv", "http://arXiv.org.evil.com/abs/2501.00001"},
	{"typosquat github", "http://api.github.com.attacker.io/users"},
	{"pastebin exfil", "http://pastebin.com/raw/abc123"},
	{"discord webhook", "http://discord.com/api/webhooks/123/token"},
	{"slack webhook", "http://hooks.slack.com/services/XXX"},
	{"s3 exfil", "http://my-bucket.s3.amazonaws.com/data"},
	{"google apis unexpected", "http://www.googleapis.com/oauth2/v4/token"},
	{"openai direct bypass", "http://api.openai.com/v1/chat/completions"},
	{"anthropic direct bypass", "http://api.anthropic.com/v1/messages"},
	{"gemini direct bypass", "http://generativelanguage.googleapis.com/v1beta/models"},
	{"ollama local bypass", "http://localhost:11434/api/chat"},
	{"ollama 127 bypass", "http://127.0.0.1:11434/api/chat"},
	{"raw github content", "http://raw.githubusercontent.com/org/repo/main/secret"},
	{"npm registry exfil", "http://registry.npmjs.org/-/user/token"},
	{"pypi registry", "http://pypi.org/pypi/package/json"},
	{"huggingface direct", "http://huggingface.co/api/models"},
	{"custom LLM proxy", "http://my-llm-proxy.internal:8080/v1/complete"},
}

func newComplianceClient(t *testing.T) *client.Client {
	t.Helper()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("compliance-test-token"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	cfg := client.Config{
		SocketPath:    "/nonexistent-compliance-test.sock",
		AuthTokenPath: tokenPath,
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	return c
}

func TestInvZen085_AllDisallowedHostsRejected(t *testing.T) {
	c := newComplianceClient(t)

	for _, tc := range disallowedHosts {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequestWithContext(
				context.Background(), http.MethodGet, tc.url, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			_, err = c.Do(req)
			if err == nil {
				t.Fatalf("host %q: expected ErrHostNotAllowed, got nil (request succeeded!)", tc.url)
			}
			if !errors.Is(err, client.ErrHostNotAllowed) {
				t.Errorf("host %q: err = %v, want errors.Is(err, ErrHostNotAllowed)", tc.url, err)
			}
		})
	}
}

func TestInvZen085_DefaultWhitelistExact(t *testing.T) {
	// These hosts MUST be allowed by default (test that they are NOT rejected
	// when pointed at a non-listening address — the error should be a dial
	// error, not ErrHostNotAllowed).
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("tok"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := client.Config{
		SocketPath:    "/nonexistent.sock",
		AuthTokenPath: tokenPath,
	}
	c, err := client.New(cfg)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}

	allowedExternals := []string{
		"http://arxiv.org/abs/2501.00001",
		"http://export.arxiv.org/abs/2501.00001",
		"https://api.github.com/repos/owner/repo",
		"http://duckduckgo.com/?q=test",
		"http://html.duckduckgo.com/?q=test",
	}

	for _, u := range allowedExternals {
		u := u
		t.Run(u, func(t *testing.T) {
			req, err2 := http.NewRequestWithContext(
				context.Background(), http.MethodGet, u, nil)
			if err2 != nil {
				t.Fatalf("build request: %v", err2)
			}
			_, err2 = c.Do(req)

			if errors.Is(err2, client.ErrHostNotAllowed) {
				t.Errorf("host %q: got ErrHostNotAllowed — it should be in the default whitelist", u)
			}

		})
	}
}
