// SPDX-License-Identifier: MIT
package embed

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

var ErrMPSUnavailable = errors.New("embed: MPS embedder unavailable")

type MPSOptions struct {
	PythonPath string

	ScriptPath string

	Dimensions int

	Model string
}

type MPSEmbedder struct {
	opts   MPSOptions
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	mu     sync.Mutex
	closed bool
}

func NewMPSEmbedder(opts MPSOptions) (*MPSEmbedder, error) {
	if opts.Dimensions <= 0 {
		return nil, errors.New("embed: MPSEmbedder Dimensions must be > 0")
	}
	if opts.PythonPath == "" {
		opts.PythonPath = "python3"
	}
	if opts.ScriptPath == "" {
		return nil, fmt.Errorf("%w: empty ScriptPath", ErrMPSUnavailable)
	}
	if _, err := os.Stat(opts.ScriptPath); err != nil {
		return nil, fmt.Errorf("%w: script %s: %v", ErrMPSUnavailable, opts.ScriptPath, err)
	}
	cmd := exec.Command(opts.PythonPath, opts.ScriptPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("embed: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("embed: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("%w: start subprocess: %v", ErrMPSUnavailable, err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024*4)
	return &MPSEmbedder{
		opts:   opts,
		cmd:    cmd,
		stdin:  stdin,
		stdout: scanner,
	}, nil
}

func (m *MPSEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("embed: MPSEmbedder closed")
	}
	req := map[string]string{"text": text}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("embed: marshal req: %w", err)
	}
	type respT struct {
		Embedding  []float32 `json:"embedding"`
		Dimensions int       `json:"dimensions"`
		Error      string    `json:"error,omitempty"`
	}

	if _, err := m.stdin.Write(append(reqBytes, '\n')); err != nil {
		return nil, fmt.Errorf("embed: write req: %w", err)
	}

	type readResult struct {
		resp respT
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		if !m.stdout.Scan() {
			scanErr := m.stdout.Err()
			if scanErr == nil {
				scanErr = io.EOF
			}
			ch <- readResult{err: fmt.Errorf("embed: read resp: %w", scanErr)}
			return
		}
		var resp respT
		if err := json.Unmarshal(m.stdout.Bytes(), &resp); err != nil {
			ch <- readResult{err: fmt.Errorf("embed: unmarshal resp: %w", err)}
			return
		}
		ch <- readResult{resp: resp}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		if r.resp.Error != "" {
			return nil, fmt.Errorf("embed: subprocess: %s", r.resp.Error)
		}
		if len(r.resp.Embedding) != m.opts.Dimensions {
			return nil, fmt.Errorf("embed: dim mismatch: got %d want %d",
				len(r.resp.Embedding), m.opts.Dimensions)
		}
		return r.resp.Embedding, nil
	}
}

func (m *MPSEmbedder) Dimensions() int { return m.opts.Dimensions }

func (m *MPSEmbedder) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	_ = m.stdin.Close()
	if m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
	}
	_ = m.cmd.Wait()
	return nil
}
