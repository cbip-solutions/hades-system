// SPDX-License-Identifier: MIT
// Package testhelpers — TTY simulator for bubbletea programs.
//
// documented at internal/onboard/qna/wizard.go::runCustomizePath
// docstring (commit de5def64 "fix(onboard): wire install_hermes +
// enable_audit_chain answers + polish"):
//
//	Coverage gap (intentional, deferred to A-7): the happy path through
//	tea.NewProgram().Run() + final model assertion + answer extraction
//	requires a TTY simulator; A-7 integration scaffolding ships the
//	harness (tests/testhelpers/onboard_testdaemon.go + buffered terminal
//	stub) and is the obligated owner of that coverage.
//
// The simulator wires a *bytes.Buffer*-backed `io.Reader` (input) +
// `io.Writer` (output) into a `tea.Program` via the public bubbletea
// program options (`tea.WithInput`, `tea.WithOutput`). bubbletea
// v1.3.10's Program.Run honours these options:
//
//   - `customInput` branch in `tea.go` line 624 skips the os.Stdin
//     TTY probe and reads directly from the injected reader.
//   - `tea.WithOutput` swaps the renderer's writer; no terminal
//     control codes are emitted to the real stdout during tests.
//
// Usage:
//
//	sim := testhelpers.NewTTYSimulator(t)
//	sim.WriteKey(testhelpers.KeyDown)
//	sim.WriteKey(testhelpers.KeyEnter)
//	prog := tea.NewProgram(model, sim.ProgramOptions()...)
//	final, err := prog.Run()
//
// The simulator is safe for sequential use within a single test (no
// goroutine fan-out internally); callers that need t.Parallel() should
// instantiate a fresh simulator per test.
package testhelpers

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type TTYSimulator struct {
	t         *testing.T
	mu        sync.Mutex
	inR       *io.PipeReader
	inW       *io.PipeWriter
	out       *safeBuffer
	ctx       context.Context
	closeOnce sync.Once
	closeErr  error
}

func NewTTYSimulator(t *testing.T) *TTYSimulator {
	t.Helper()
	pr, pw := io.Pipe()
	sim := &TTYSimulator{
		t:   t,
		inR: pr,
		inW: pw,
		out: newSafeBuffer(),
		ctx: context.Background(),
	}
	t.Cleanup(sim.Close)
	return sim
}

func NewTTYSimulatorWithContext(t *testing.T, ctx context.Context) *TTYSimulator {
	t.Helper()
	s := NewTTYSimulator(t)
	s.ctx = ctx
	return s
}

func (s *TTYSimulator) Close() {
	s.closeOnce.Do(func() {
		s.closeErr = s.inW.Close()
	})
}

func (s *TTYSimulator) CloseErr() error { return s.closeErr }

func (s *TTYSimulator) ProgramOptions() []tea.ProgramOption {
	return []tea.ProgramOption{
		tea.WithContext(s.ctx),
		tea.WithInput(s.inR),
		tea.WithOutput(s.out),
		tea.WithoutSignalHandler(),
	}
}

type Key string

const (
	KeyEnter Key = "\r"

	KeyEsc Key = "\x1b"

	KeyCtrlC Key = "\x03"

	KeyUp Key = "\x1b[A"

	KeyDown Key = "\x1b[B"
)

func (s *TTYSimulator) WriteKey(k Key) {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.inW.Write([]byte(k)); err != nil {
		s.t.Fatalf("TTYSimulator.WriteKey: %v", err)
	}
}

func (s *TTYSimulator) WriteKeys(keys ...Key) {
	s.t.Helper()
	for _, k := range keys {
		s.WriteKey(k)
	}
}

func (s *TTYSimulator) WriteString(text string) {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.inW.Write([]byte(text)); err != nil {
		s.t.Fatalf("TTYSimulator.WriteString: %v", err)
	}
}

func (s *TTYSimulator) Output() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

func (s *TTYSimulator) InputReader() io.Reader { return s.inR }

func (s *TTYSimulator) OutputWriter() io.Writer { return s.out }

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func newSafeBuffer() *safeBuffer { return &safeBuffer{} }

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Read(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}
