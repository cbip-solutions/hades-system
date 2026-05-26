package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeMCPRestartClient struct {
	resp *client.MCPRestartResponse
	err  error
	last string
}

func (f *fakeMCPRestartClient) MCPRestart(_ context.Context, name string) (*client.MCPRestartResponse, error) {
	f.last = name
	if f.err != nil {
		return nil, f.err
	}
	if f.resp == nil {
		return &client.MCPRestartResponse{Name: name, Status: "restarted", DurationMs: 120}, nil
	}
	return f.resp, nil
}

func TestDaemonRestartMCPCmdRegistered(t *testing.T) {
	t.Parallel()
	cmd := NewDaemonRestartMCPCmd(func(*cobra.Command) MCPRestartClient { return &fakeMCPRestartClient{} })
	if cmd.Use != "restart-mcp <name>" {
		t.Fatalf("Use=%q, want %q", cmd.Use, "restart-mcp <name>")
	}
}

func TestDaemonRestartMCPSuccess(t *testing.T) {
	t.Parallel()
	fake := &fakeMCPRestartClient{}
	flags := MCPRestartFlags{Name: "research"}
	var buf bytes.Buffer
	if err := RunDaemonRestartMCP(context.Background(), fake, flags, &buf); err != nil {
		t.Fatalf("RunDaemonRestartMCP: %v", err)
	}
	if fake.last != "research" {
		t.Fatalf("client received name=%q, want research", fake.last)
	}
	if !strings.Contains(buf.String(), "restarted") {
		t.Fatalf("output missing 'restarted'; got %q", buf.String())
	}
}

func TestDaemonRestartMCPEmptyNameRecoverable(t *testing.T) {
	t.Parallel()
	err := RunDaemonRestartMCP(context.Background(), &fakeMCPRestartClient{}, MCPRestartFlags{}, &bytes.Buffer{})
	if err == nil || !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable", err)
	}
}

func TestDaemonRestartMCPUnknownNameRecoverable(t *testing.T) {
	t.Parallel()
	flags := MCPRestartFlags{Name: "not-a-real-mcp"}
	err := RunDaemonRestartMCP(context.Background(), &fakeMCPRestartClient{}, flags, &bytes.Buffer{})
	if err == nil || !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable", err)
	}
	if !strings.Contains(err.Error(), "not-a-real-mcp") {
		t.Errorf("err=%q, want includes the bad name", err.Error())
	}
}

func TestDaemonRestartMCPRateLimitRecoverable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "inv-zen-168 rate-limit hit (3 restarts in 5min)",
		})
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	prod := &productionMCPRestartClient{c: c}
	flags := MCPRestartFlags{Name: "research"}
	err := RunDaemonRestartMCP(context.Background(), prod, flags, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for 429; got nil")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable", err)
	}
	if !strings.Contains(err.Error(), "rate-limit") && !strings.Contains(err.Error(), "inv-zen-168") {
		t.Errorf("err=%q, want includes inv-zen-168 hint", err.Error())
	}
}

func TestClassifyMCPRestartError_Nil(t *testing.T) {
	t.Parallel()
	if got := classifyMCPRestartError(nil); got != nil {
		t.Fatalf("classifyMCPRestartError(nil) = %v, want nil", got)
	}
}

func TestClassifyMCPRestartError_AlreadyRecoverable(t *testing.T) {
	t.Parallel()
	input := recoverableWrap(fmt.Errorf("prior"), "already recoverable")
	got := classifyMCPRestartError(input)
	if !errors.Is(got, ErrRecoverable) {
		t.Fatalf("got=%v, want ErrRecoverable", got)
	}

	if got != input {
		t.Errorf("classifyMCPRestartError rewrapped already-recoverable error; got %v, want same pointer", got)
	}
}

func TestClassifyMCPRestartError_422(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid mcp name"})
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	prod := &productionMCPRestartClient{c: c}
	flags := MCPRestartFlags{Name: "research"}
	err := RunDaemonRestartMCP(context.Background(), prod, flags, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for 422; got nil")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v, want ErrRecoverable", err)
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("err=%q, want includes 'rejected'", err.Error())
	}
}

func TestClassifyMCPRestartError_5xx(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal daemon error"})
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	prod := &productionMCPRestartClient{c: c}
	flags := MCPRestartFlags{Name: "research"}
	err := RunDaemonRestartMCP(context.Background(), prod, flags, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for 5xx; got nil")
	}
	if errors.Is(err, ErrRecoverable) {
		t.Fatalf("err=%v should NOT be ErrRecoverable for 5xx", err)
	}
}

func TestDaemonRestartMCPAllValidNamesAccepted(t *testing.T) {
	t.Parallel()
	for _, name := range validMCPNamesSorted {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeMCPRestartClient{}
			flags := MCPRestartFlags{Name: name}
			if err := RunDaemonRestartMCP(context.Background(), fake, flags, &bytes.Buffer{}); err != nil {
				t.Fatalf("RunDaemonRestartMCP(%q): %v", name, err)
			}
		})
	}
}
