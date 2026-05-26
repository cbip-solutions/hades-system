package sshexec

import (
	"context"
	"testing"
	"time"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func serveOverInMemory(t *testing.T, srv *Server) (context.Context, *mcp.ClientSession) {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.mcpServer.Run(ctx, serverTransport)
	}()
	cli := mcp.NewClient(&mcp.Implementation{Name: "test-sshexec-client", Version: "0"}, nil)
	session, err := cli.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		<-serveErr
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
		<-serveErr
	})
	return ctx, session
}

func TestInMemValidateToolWire(t *testing.T) {
	srv := NewServer(ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{"vps"}),
	})
	ctx, session := serveOverInMemory(t, srv)

	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range tools.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"validate", "exec", "list_allowed"} {
		if !got[want] {
			t.Errorf("ListTools missing %q (got %v)", want, got)
		}
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "validate",
		Arguments: map[string]any{
			"cmd":     "alembic upgrade head",
			"project": "internal-platform-x",
		},
	})
	if err != nil {
		t.Fatalf("CallTool validate: %v", err)
	}
	if res.IsError {
		t.Errorf("validate returned IsError=true: %v", res.Content)
	}
}

func TestInMemValidateToolError(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{"vps"}),
	})
	ctx, session := serveOverInMemory(t, srv)
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "validate",
		Arguments: map[string]any{

			"project": "internal-platform-x",
		},
	})

	if err == nil && !res.IsError {
		t.Errorf("expected error response for missing cmd; got %v", res)
	}
}

func TestInMemListAllowedToolWire(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{"vps"}),
	})
	ctx, session := serveOverInMemory(t, srv)
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_allowed",
		Arguments: map[string]any{
			"project": "internal-platform-x",
		},
	})
	if err != nil {
		t.Fatalf("CallTool list_allowed: %v", err)
	}
	if res.IsError {
		t.Errorf("list_allowed returned IsError=true: %v", res.Content)
	}
}

func TestInMemExecToolWire(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	sshd, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "ok\n", ExitCode: 0}
	}))
	defer sshd.Close()
	srv := NewServer(ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{sshd.Addr()}),
		Auth:              AgentAuthForTest(),
		Emitter:           NopAuditEmitter{},
	})
	ctx, session := serveOverInMemory(t, srv)
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "exec",
		Arguments: map[string]any{
			"host":    sshd.Addr(),
			"cmd":     "alembic upgrade head",
			"project": "internal-platform-x",
			"timeout": "5s",
		},
	})
	if err != nil {
		t.Fatalf("CallTool exec: %v", err)
	}
	if res.IsError {
		t.Errorf("exec returned IsError=true: %v", res.Content)
	}
}

func TestInMemValidateClosureResolverError(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: func(string) (*Allowlist, error) {
			return nil, errStubResolver
		},
	})
	ctx, session := serveOverInMemory(t, srv)
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "validate",
		Arguments: map[string]any{
			"cmd":     "alembic upgrade",
			"project": "internal-platform-x",
		},
	})
	if err == nil && !res.IsError {
		t.Error("expected resolver error to propagate")
	}
}

func TestInMemListAllowedClosureResolverError(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: func(string) (*Allowlist, error) {
			return nil, errStubResolver
		},
	})
	ctx, session := serveOverInMemory(t, srv)
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_allowed",
		Arguments: map[string]any{
			"project": "internal-platform-x",
		},
	})
	if err == nil && !res.IsError {
		t.Error("expected resolver error to propagate")
	}
}

func TestInMemExecToolStreamsProgress(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	sshd, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "streaming-output\n", ExitCode: 0}
	}))
	defer sshd.Close()

	srv := NewServer(ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{sshd.Addr()}),
		Auth:              AgentAuthForTest(),
		Emitter:           NopAuditEmitter{},
	})

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.mcpServer.Run(ctx, serverTransport) }()
	defer func() { <-serveErr }()

	progressNotifs := make(chan *mcp.ProgressNotificationParams, 16)
	cli := mcp.NewClient(&mcp.Implementation{Name: "progress-test", Version: "0"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			select {
			case progressNotifs <- req.Params:
			default:
			}
		},
	})
	session, err := cli.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer session.Close()

	params := &mcp.CallToolParams{
		Name: "exec",
		Arguments: map[string]any{
			"host":    sshd.Addr(),
			"cmd":     "alembic upgrade head",
			"project": "internal-platform-x",
			"timeout": "5s",
		},
	}
	params.SetProgressToken("ssh-exec-progress-tok-1")

	res, err := session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("CallTool exec: %v", err)
	}
	if res.IsError {
		t.Fatalf("exec returned IsError=true: %v", res.Content)
	}

	deadline := time.After(500 * time.Millisecond)
	got := 0
collect:
	for {
		select {
		case np := <-progressNotifs:
			got++
			if np.Message == "" {
				t.Errorf("progress notif Message empty (want stdout/stderr label)")
			}
		case <-deadline:
			break collect
		}
	}
	if got == 0 {
		t.Errorf("no progress notifications received; want >=1 (progressSink wiring broken)")
	}
}

func TestInMemExecToolNoProgressTokenDiscards(t *testing.T) {
	t.Setenv("ZEN_SSH_INSECURE_TEST", "1")
	sshd, _ := testharness.NewFakeSSHD(testharness.HandlerFunc(func(req string) testharness.HandlerScript {
		return testharness.HandlerScript{Stdout: "ok\n", ExitCode: 0}
	}))
	defer sshd.Close()
	srv := NewServer(ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: stubAllowlistInternal([]string{"alembic *"}, []string{sshd.Addr()}),
		Auth:              AgentAuthForTest(),
		Emitter:           NopAuditEmitter{},
	})
	ctx, session := serveOverInMemory(t, srv)
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "exec",
		Arguments: map[string]any{
			"host":    sshd.Addr(),
			"cmd":     "alembic upgrade head",
			"project": "internal-platform-x",
			"timeout": "5s",
		},
	})
	if err != nil {
		t.Fatalf("CallTool exec: %v", err)
	}
	if res.IsError {
		t.Errorf("exec returned IsError=true: %v", res.Content)
	}
}

func TestInMemExecToolErrorPath(t *testing.T) {
	srv := NewServer(ServerConfig{
		AllowlistResolver: func(string) (*Allowlist, error) {
			return nil, errStubResolver
		},
		Auth:    AgentAuthForTest(),
		Emitter: NopAuditEmitter{},
	})
	ctx, session := serveOverInMemory(t, srv)
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "exec",
		Arguments: map[string]any{
			"host":    "vps",
			"cmd":     "alembic upgrade",
			"project": "internal-platform-x",
		},
	})
	if err == nil && !res.IsError {
		t.Error("expected error from resolver")
	}
}

type stubResolverErr struct{}

func (stubResolverErr) Error() string { return "stub-resolver-error" }

var errStubResolver = stubResolverErr{}
