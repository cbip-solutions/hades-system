// SPDX-License-Identifier: MIT
// internal/mcp/sshexec/interactive.go
//
// Task L-7 (front-loaded into L-5 to avoid stub) — interactive
// prompt detector. Sealed type; constructor is the unexported
// newDetector() called by Run only.
//
// This is the compile-check anchor for invariant: callers cannot
// instantiate a Detector outside this package, so every exec.Run path
// goes through Run's wired-up SIGKILL handling.
//
// Runtime contract:
// - inspects the first 1024 bytes per stream;
// - patterns: [sudo], password:, Are you sure (yes/no), (yes/no),
// TIOCSTI bytes 0xFD 0x18, leading "> " continuation;
// - latches on first detection: subsequent Feed calls are no-ops;
// - trigger latency < 100ms (locked by TestDetectorTriggerLatency).

package sshexec

import (
	"bytes"
	"strings"
	"sync"
	"sync/atomic"
)

const detectorWindow = 1024

type Detector struct {
	mu        sync.Mutex
	buf       [detectorWindow]byte
	off       int
	triggered atomic.Bool
	ch        chan string
	snippet   []byte
}

func newDetector() *Detector {
	return &Detector{ch: make(chan string, 1)}
}

func (d *Detector) Feed(data []byte, stream StreamLabel) {
	if d.triggered.Load() {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.triggered.Load() {
		return
	}
	rem := detectorWindow - d.off
	if rem <= 0 {
		return
	}
	n := len(data)
	if n > rem {
		n = rem
	}
	copy(d.buf[d.off:], data[:n])
	d.off += n
	view := d.buf[:d.off]
	if reason := matchPrompts(view); reason != "" {
		d.triggered.Store(true)

		snip := make([]byte, d.off)
		copy(snip, view)
		d.snippet = snip
		select {
		case d.ch <- reason:
		default:
		}
	}

	_ = stream
}

func (d *Detector) Triggered() <-chan string { return d.ch }

func (d *Detector) Snippet() []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.snippet == nil {
		return nil
	}
	out := make([]byte, len(d.snippet))
	copy(out, d.snippet)
	return out
}

func matchPrompts(view []byte) string {
	low := bytes.ToLower(view)
	if bytes.Contains(low, []byte("[sudo]")) {
		return "sudo prompt"
	}

	if idx := bytes.Index(low, []byte("password")); idx >= 0 {
		end := idx + 30
		if end > len(view) {
			end = len(view)
		}
		if bytes.IndexByte(view[idx:end], ':') >= 0 {
			return "password prompt"
		}
		return "password keyword"
	}
	if bytes.Contains(low, []byte("are you sure")) {
		return "are you sure prompt"
	}
	if bytes.Contains(view, []byte("(yes/no)")) {
		return "yes/no prompt"
	}
	for i := 0; i < len(view)-1; i++ {
		if view[i] == 0xfd && view[i+1] == 0x18 {
			return "TIOCSTI marker"
		}
	}

	if strings.HasPrefix(string(view), "> ") {
		return "continuation prompt"
	}
	for i := 0; i < len(view)-2; i++ {
		if view[i] == '\n' && view[i+1] == '>' && view[i+2] == ' ' {
			return "continuation prompt"
		}
	}
	return ""
}
