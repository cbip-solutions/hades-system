// SPDX-License-Identifier: MIT
// tests/testharness/sshd_fake.go
//
// Phase L Task L-1 — embedded crypto/ssh server harness.
//
// FakeSSHD listens on a random localhost port, accepts any agent key,
// and runs a scripted handler returning bytes + exit code. Supports:
//   - normal stdout/stderr scripted output
//   - interactive prompt simulation (Stdout content emitted before
//     any benign output by the test handler)
//   - raw scripted bytes (RawStdout / RawStderr) for adversarial tests
//   - delayed responses (handler returns Delay)
//   - max active connections guard
//
// Reused by Phase L unit/integration/adversarial/compliance tests AND by
// future Plan 5+ phases that test ssh-exec paths.
//
// Why a fake instead of real OpenSSH?
//   - Deterministic timing (no jitter from the OS scheduler).
//   - Adversarial scripting: real OpenSSH cannot be coerced into emitting
//     raw TIOCSTI bytes; we need byte-level control.
//   - CI portability: no need to install OpenSSH on every runner.
//
// Realworld test (tests/realworld/sshexec_actual_test.go) covers actual
// OpenSSH path under build tag `realworld`.

package testharness

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

type HandlerScript struct {
	Stdout      string
	Stderr      string
	RawStdout   []byte
	RawStderr   []byte
	ExitCode    uint32
	Delay       time.Duration
	Interactive bool
}

type Handler interface {
	Respond(req string) HandlerScript
}

type HandlerFunc func(req string) HandlerScript

func (f HandlerFunc) Respond(req string) HandlerScript { return f(req) }

type FakeSSHD struct {
	listener   net.Listener
	cfg        *ssh.ServerConfig
	handler    Handler
	maxConns   int
	active     int64
	closed     atomic.Bool
	wg         sync.WaitGroup
	hostSigner ssh.Signer
}

func NewFakeSSHD(h Handler) (*FakeSSHD, error) {
	return NewFakeSSHDWithLimit(64, h)
}

func NewFakeSSHDWithLimit(maxConns int, h Handler) (*FakeSSHD, error) {
	if h == nil {
		return nil, errors.New("nil Handler")
	}
	signer, err := generateHostKey()
	if err != nil {
		return nil, fmt.Errorf("host key: %w", err)
	}
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return &ssh.Permissions{}, nil
		},
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			return &ssh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	srv := &FakeSSHD{
		listener:   ln,
		cfg:        cfg,
		handler:    h,
		maxConns:   maxConns,
		hostSigner: signer,
	}
	srv.wg.Add(1)
	go srv.acceptLoop()
	return srv, nil
}

func (s *FakeSSHD) Addr() string { return s.listener.Addr().String() }

func (s *FakeSSHD) ActiveConns() int64 { return atomic.LoadInt64(&s.active) }

func (s *FakeSSHD) Close() error {
	s.closed.Store(true)
	err := s.listener.Close()
	s.wg.Wait()
	return err
}

func (s *FakeSSHD) acceptLoop() {
	defer s.wg.Done()
	for {
		c, err := s.listener.Accept()
		if err != nil {
			if s.closed.Load() {
				return
			}
			return
		}
		if atomic.LoadInt64(&s.active) >= int64(s.maxConns) {
			c.Close()
			continue
		}
		atomic.AddInt64(&s.active, 1)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer atomic.AddInt64(&s.active, -1)
			defer c.Close()
			s.serveConn(c)
		}()
	}
}

func (s *FakeSSHD) serveConn(c net.Conn) {
	conn, chans, reqs, err := ssh.NewServerConn(c, s.cfg)
	if err != nil {
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			newCh.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, requests, err := newCh.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(ch, requests)
	}
}

func (s *FakeSSHD) handleSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "exec":
			cmd := parseExecPayload(req.Payload)
			req.Reply(true, nil)
			script := s.handler.Respond(cmd)
			s.runScript(ch, script)
			return
		case "shell", "pty-req", "env":

			if req.Type == "env" {
				req.Reply(true, nil)
			} else {
				req.Reply(false, nil)
			}
		default:
			req.Reply(false, nil)
		}
	}
}

func (s *FakeSSHD) runScript(ch ssh.Channel, script HandlerScript) {
	if script.Delay > 0 {
		time.Sleep(script.Delay)
	}
	if script.RawStdout != nil {
		ch.Write(script.RawStdout)
	} else if script.Stdout != "" {
		io.WriteString(ch, script.Stdout)
	}
	if script.RawStderr != nil {
		ch.Stderr().Write(script.RawStderr)
	} else if script.Stderr != "" {
		io.WriteString(ch.Stderr(), script.Stderr)
	}

	type exitMsg struct{ Code uint32 }
	ch.SendRequest("exit-status", false, ssh.Marshal(exitMsg{Code: script.ExitCode}))
}

func parseExecPayload(p []byte) string {
	if len(p) < 4 {
		return ""
	}

	n := int(p[0])<<24 | int(p[1])<<16 | int(p[2])<<8 | int(p[3])
	if 4+n > len(p) {
		return ""
	}
	return string(p[4 : 4+n])
}

func generateHostKey() (ssh.Signer, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}
