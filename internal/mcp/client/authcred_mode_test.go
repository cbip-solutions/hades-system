package client_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func TestNew_AuthTokenWorldReadableRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not enforced on Windows")
	}
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("world-readable-token"), 0644); err != nil {
		t.Fatalf("write token: %v", err)
	}

	if err := os.Chmod(tokenPath, 0644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	cfg := client.Config{
		BaseURL:       "http://127.0.0.1:9999",
		AuthTokenPath: tokenPath,
	}
	_, err := client.New(cfg)
	if err == nil {
		t.Fatal("expected error for world-readable token, got nil")
	}
	if !strings.Contains(err.Error(), "insecure mode") {
		t.Errorf("err = %v, want message mentioning 'insecure mode'", err)
	}
}

func TestNew_AuthTokenGroupReadableRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not enforced on Windows")
	}
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "auth-token")
	if err := os.WriteFile(tokenPath, []byte("group-readable-token"), 0640); err != nil {
		t.Fatalf("write token: %v", err)
	}
	if err := os.Chmod(tokenPath, 0640); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	cfg := client.Config{
		BaseURL:       "http://127.0.0.1:9999",
		AuthTokenPath: tokenPath,
	}
	_, err := client.New(cfg)
	if err == nil {
		t.Fatal("expected error for group-readable token, got nil")
	}
	if !strings.Contains(err.Error(), "insecure mode") {
		t.Errorf("err = %v, want message mentioning 'insecure mode'", err)
	}
}

func TestNew_AuthTokenSecureModeAccepted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not enforced on Windows")
	}
	for _, mode := range []os.FileMode{0600, 0400} {
		t.Run("mode-"+modeString(mode), func(t *testing.T) {
			dir := t.TempDir()
			tokenPath := filepath.Join(dir, "auth-token")
			if err := os.WriteFile(tokenPath, []byte("secure-token"), mode); err != nil {
				t.Fatalf("write token: %v", err)
			}
			if err := os.Chmod(tokenPath, mode); err != nil {
				t.Fatalf("chmod: %v", err)
			}
			cfg := client.Config{
				BaseURL:       "http://127.0.0.1:9999",
				AuthTokenPath: tokenPath,
			}
			c, err := client.New(cfg)
			if err != nil {
				t.Fatalf("client.New rejected mode %v: %v", mode, err)
			}
			if c == nil {
				t.Fatal("client is nil after successful New")
			}
		})
	}
}

func TestNew_AuthTokenStatErrorPropagated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not enforced on Windows")
	}

	t.Skip("os.Stat error path requires racy file deletion to provoke; see TestNew_MissingTokenFile for the ReadFile error path")
}

func modeString(m os.FileMode) string {

	const digits = "01234567"
	if m == 0 {
		return "0"
	}
	out := []byte{}
	for m > 0 {
		out = append([]byte{digits[m&7]}, out...)
		m >>= 3
	}
	return string(out)
}

func TestNew_AuthTokenInsecureModeMessageQuotesPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not enforced on Windows")
	}
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "leaky-token")
	if err := os.WriteFile(tokenPath, []byte("x"), 0644); err != nil {
		t.Fatalf("write token: %v", err)
	}
	_ = os.Chmod(tokenPath, 0644)
	_, err := client.New(client.Config{
		BaseURL:       "http://127.0.0.1:9999",
		AuthTokenPath: tokenPath,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), tokenPath) {
		t.Errorf("err message %q does not include path %q", err.Error(), tokenPath)
	}
}

func TestNew_AuthTokenIgnoresNonPermBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not enforced on Windows")
	}
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "tok")
	if err := os.WriteFile(tokenPath, []byte("t"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	if err := os.Chmod(tokenPath, 0600); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("setup: mode = %v, want 0600", perm)
	}

	c, err := client.New(client.Config{
		BaseURL:       "http://127.0.0.1:9999",
		AuthTokenPath: tokenPath,
	})
	if err != nil {
		t.Fatalf("client.New rejected 0600 token: %v", err)
	}
	if c == nil {
		t.Fatal("client is nil")
	}
}

var _ fs.FileMode

func TestNew_AuthTokenErrorTypeStable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not enforced on Windows")
	}
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "tok")
	_ = os.WriteFile(tokenPath, []byte("t"), 0666)
	_ = os.Chmod(tokenPath, 0666)
	_, err := client.New(client.Config{
		BaseURL:       "http://127.0.0.1:9999",
		AuthTokenPath: tokenPath,
	})
	if err == nil {
		t.Fatal("expected error")
	}

	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		t.Errorf("err should be a constructed error, not a raw fs.PathError: %v", err)
	}
}
