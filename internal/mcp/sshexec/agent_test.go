package sshexec

import (
	"crypto/rand"
	"crypto/rsa"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh/agent"
)

func startFakeAgent(t *testing.T, withKey bool) string {
	t.Helper()
	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "agent.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	a := agent.NewKeyring()
	if withKey {
		priv, err := rsa.GenerateKey(rand.Reader, 1024)
		if err != nil {
			t.Fatalf("rsa: %v", err)
		}
		if err := a.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
			t.Fatalf("agent.Add: %v", err)
		}
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = agent.ServeAgent(a, c)
			}()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		wg.Wait()
	})
	return sockPath
}

func TestAgentAuthHappyPath(t *testing.T) {
	sock := startFakeAgent(t, true)
	t.Setenv("SSH_AUTH_SOCK", sock)
	a, err := AgentAuth()
	if err != nil {
		t.Fatalf("AgentAuth: %v", err)
	}
	if len(a.signers) == 0 {
		t.Errorf("AgentAuth returned no signers; want >=1")
	}

	auths := a.sshAuth()
	if len(auths) != 1 {
		t.Errorf("sshAuth len = %d", len(auths))
	}
}

func TestAgentAuthEmptyAgent(t *testing.T) {
	sock := startFakeAgent(t, false)
	t.Setenv("SSH_AUTH_SOCK", sock)
	_, err := AgentAuth()
	if err == nil {
		t.Fatal("AgentAuth accepted empty agent")
	}
	if !strings.Contains(err.Error(), "no identities") {
		t.Errorf("err = %v, want 'no identities'", err)
	}
}
