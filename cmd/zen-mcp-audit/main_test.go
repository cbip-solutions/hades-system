package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestBuildServerMissingAuthTokenPath(t *testing.T) {
	_, err := buildServer(buildOptions{
		DaemonURL:     "http://localhost:0",
		AuthTokenPath: "",
	})
	if err == nil {
		t.Error("expected error for missing auth-token-path, got nil")
	}
}

func TestBuildServerMissingSocket(t *testing.T) {
	tf := writeTempToken(t, "test-token")
	_, err := buildServer(buildOptions{
		AuthTokenPath: tf,
		DaemonURL:     "",
		Socket:        "",
	})
	if err == nil {
		t.Error("expected error when no socket or daemon URL provided")
	}
}

func TestBuildServerEmptyTokenFile(t *testing.T) {
	tf := writeTempToken(t, "  \n  ")
	_, err := buildServer(buildOptions{
		DaemonURL:     "http://localhost:0",
		AuthTokenPath: tf,
	})
	if err == nil {
		t.Error("expected error for empty (whitespace-only) token file, got nil")
	}
}

func TestBuildServerTokenFileNotFound(t *testing.T) {
	_, err := buildServer(buildOptions{
		DaemonURL:     "http://localhost:0",
		AuthTokenPath: "/nonexistent/path/to/token",
	})
	if err == nil {
		t.Error("expected error for non-existent token file, got nil")
	}
}

func TestBuildServerHappyPath(t *testing.T) {
	fakeDaemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fakeDaemon.Close)

	tf := writeTempToken(t, "test-auth-token")
	srv, err := buildServer(buildOptions{
		DaemonURL:            fakeDaemon.URL,
		AuthTokenPath:        tf,
		ReviewerFamilyPool:   "anthropic,google,deepseek",
		MinPoolSize:          2,
		DefaultReviewerModel: "gemini-2.6-pro",
		Doctrine:             "default",
	})
	if err != nil {
		t.Fatalf("buildServer happy path: %v", err)
	}
	if srv == nil {
		t.Fatal("buildServer returned nil server")
	}
}

func TestBuildServerMaxScopeDoctrine(t *testing.T) {
	tf := writeTempToken(t, "test-token")
	_, err := buildServer(buildOptions{
		DaemonURL:            "http://localhost:0",
		AuthTokenPath:        tf,
		ReviewerFamilyPool:   "anthropic,google,deepseek,local-qwen,openai",
		MinPoolSize:          4,
		DefaultReviewerModel: "gemini-2.6-pro",
		Doctrine:             "max-scope",
	})
	if err != nil {
		t.Errorf("buildServer max-scope: %v", err)
	}
}

// TestUnknownDoctrineRejected verifies the I-3 fix: an unknown doctrine
// flag value (not in {default, max-scope, capa-firewall, "", and the
// underscore variants}) MUST cause buildServer to fail with an explicit
// error rather than silently picking the default-policy mapping.
//
// Pre-fix the switch had no default arm, so e.g. --doctrine=strict (a
// typo) silently mapped to EmptyPoolHardStop and operators never noticed
// the misconfiguration until production, when an unexpected hard-stop
// blocked a review (review I-3).
func TestUnknownDoctrineRejected(t *testing.T) {
	tf := writeTempToken(t, "test-token")
	for _, bad := range []string{"strict", "lenient", "no-such-doctrine", "max scope"} {
		bad := bad
		t.Run(bad, func(t *testing.T) {
			_, err := buildServer(buildOptions{
				DaemonURL:            "http://localhost:0",
				AuthTokenPath:        tf,
				ReviewerFamilyPool:   "anthropic,google,deepseek",
				MinPoolSize:          2,
				DefaultReviewerModel: "model",
				Doctrine:             bad,
			})
			if err == nil {
				t.Fatalf("expected error for unknown doctrine %q, got nil", bad)
			}
			if !strings.Contains(err.Error(), "unknown doctrine") {
				t.Errorf("err = %v, want 'unknown doctrine' substring", err)
			}

			for _, want := range []string{"max-scope", "default", "capa-firewall"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %v, want valid-set substring %q", err, want)
				}
			}
		})
	}
}

func TestBuildServerCapaFirewallDoctrine(t *testing.T) {
	tf := writeTempToken(t, "test-token")
	_, err := buildServer(buildOptions{
		DaemonURL:            "http://localhost:0",
		AuthTokenPath:        tf,
		ReviewerFamilyPool:   "anthropic,google",
		MinPoolSize:          1,
		DefaultReviewerModel: "gemini-2.6-pro",
		Doctrine:             "capa-firewall",
	})
	if err != nil {
		t.Errorf("buildServer capa-firewall: %v", err)
	}
}

func TestBuildServerDefaultPool(t *testing.T) {
	tf := writeTempToken(t, "test-token")
	_, err := buildServer(buildOptions{
		DaemonURL:            "http://localhost:0",
		AuthTokenPath:        tf,
		ReviewerFamilyPool:   "",
		MinPoolSize:          2,
		DefaultReviewerModel: "gemini-2.6-pro",
		Doctrine:             "default",
	})
	if err != nil {
		t.Errorf("buildServer default pool: %v", err)
	}
}

func TestBuildServerWithSocket(t *testing.T) {
	tf := writeTempToken(t, "test-token")

	_, err := buildServer(buildOptions{
		Socket:               "/tmp/zen-swarm-test.sock",
		DaemonURL:            "",
		AuthTokenPath:        tf,
		ReviewerFamilyPool:   "anthropic,google,deepseek",
		MinPoolSize:          2,
		DefaultReviewerModel: "gemini-2.6-pro",
		Doctrine:             "default",
	})
	if err != nil {
		t.Errorf("buildServer with socket: %v", err)
	}
}

func TestBuildServerZeroMinPoolSize(t *testing.T) {
	tf := writeTempToken(t, "test-token")
	_, err := buildServer(buildOptions{
		DaemonURL:            "http://localhost:0",
		AuthTokenPath:        tf,
		ReviewerFamilyPool:   "anthropic,google,deepseek",
		MinPoolSize:          0,
		DefaultReviewerModel: "gemini-2.6-pro",
		Doctrine:             "default",
	})
	if err != nil {
		t.Errorf("buildServer zero min-pool-size: %v", err)
	}
}

func TestBuildServerPoolTooSmall(t *testing.T) {
	tf := writeTempToken(t, "test-token")
	_, err := buildServer(buildOptions{
		DaemonURL:            "http://localhost:0",
		AuthTokenPath:        tf,
		ReviewerFamilyPool:   "anthropic",
		MinPoolSize:          2,
		DefaultReviewerModel: "model",
		Doctrine:             "default",
	})
	if err == nil {
		t.Error("expected error for pool too small, got nil")
	}
}

func TestRunMissingAuthTokenPath(t *testing.T) {

	origArgs := os.Args
	os.Args = []string{"zen-mcp-audit", "--daemon-url", "http://localhost:0"}
	defer func() { os.Args = origArgs }()

	err := run()
	if err == nil {
		t.Error("expected error from run() with missing auth-token-path, got nil")
	}
}

func TestRunFlagParseError(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"zen-mcp-audit", "--nonexistent-flag-xyz"}
	defer func() { os.Args = origArgs }()

	err := run()
	if err == nil {
		t.Error("expected error from run() for unknown flag, got nil")
	}
}

func writeTempToken(t *testing.T, token string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "token-*")
	if err != nil {
		t.Fatalf("create temp token: %v", err)
	}
	if _, err := f.WriteString(token); err != nil {
		t.Fatalf("write temp token: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp token: %v", err)
	}
	return f.Name()
}
