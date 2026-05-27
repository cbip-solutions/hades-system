// SPDX-License-Identifier: MIT
// Package daemon — notifications.go.
//
// The Notifier dispatches bypass-module events to two destinations:
// 1. SQLite ledger (notifications table, schema v9).
// 2. macOS osascript banner (darwin only; non-darwin → log-only).
//
// Severity ∈ {INFO, WARN, CRITICAL}. CRITICAL events not acknowledged
// within 1h are re-dispatched (osascript fires again, last_repeated
// stamped). The repeat loop exits on Close().
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/store"
)

type Notifier struct {
	store        *store.Store
	osascriptCmd string
	repeatEvery  time.Duration
	stop         chan struct{}
	once         sync.Once
}

func NewNotifier(s *store.Store) *Notifier {
	n := &Notifier{
		store:        s,
		osascriptCmd: "osascript",
		repeatEvery:  1 * time.Hour,
		stop:         make(chan struct{}),
	}
	go n.runRepeatLoop()
	return n
}

func (n *Notifier) Dispatch(ctx context.Context, severity, title, body, source string) (int64, error) {
	id, err := n.store.InsertBypassNotification(ctx, store.Notification{
		Severity: severity,
		Title:    title,
		Body:     body,
		Source:   source,
		TS:       time.Now().UTC(),
	})
	if err != nil {
		return 0, err
	}
	n.fireOSNotification(severity, title, body)
	return id, nil
}

func (n *Notifier) fireOSNotification(severity, title, body string) {
	if runtime.GOOS != "darwin" {
		return
	}
	esc := func(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }
	subtitle := "hades-system — " + severity
	script := fmt.Sprintf(
		`display notification "%s" with title "%s" subtitle "%s"`,
		esc(body), esc(title), esc(subtitle))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, n.osascriptCmd, "-e", script).Run()
	}()
}

func (n *Notifier) runRepeatLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-n.stop:
			return
		case <-ticker.C:
			n.repeatTick()
		}
	}
}

func (n *Notifier) repeatTick() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	due, err := n.store.UnackedCriticalsDueForRepeat(ctx, n.repeatEvery)
	if err != nil {
		slog.Error("notifier repeat: list", "err", err)
		return
	}
	for _, nt := range due {
		n.fireOSNotification(nt.Severity, "[REPEAT] "+nt.Title, nt.Body)
		ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
		if err := n.store.MarkRepeated(ctx2, nt.ID); err != nil {
			slog.Error("notifier repeat: mark", "id", nt.ID, "err", err)
		}
		cancel2()
	}
}

func (n *Notifier) Close() error {
	n.once.Do(func() { close(n.stop) })
	return nil
}

func (n *Notifier) OnTierSwitch(from, _ any, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = n.Dispatch(ctx, "WARN",
		"HADES: bypass tier unavailable",
		fmt.Sprintf("Bypass tier (%v) unavailable: %s. Orchestrator cascading to the next configured provider.", from, reason),
		"bypass.tier-switch")
}

func (n *Notifier) OnRefreshPermanentFail(reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = n.Dispatch(ctx, "CRITICAL",
		"HADES: bypass OAuth refresh failing",
		"Reason: "+reason+"\nThe refresher retries automatically; if it persists, run `hades bypass refresh-now` or re-login Claude Code.",
		"bypass.refresh")
}

func (n *Notifier) OnCertPinFailure(detail string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = n.Dispatch(ctx, "CRITICAL",
		"Bypass cert pin mismatch",
		"Possible MITM or Anthropic rotated CA. Detail: "+detail,
		"bypass.cert")
}

func (n *Notifier) OnAnomalyThreshold(field string, pct float64) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = n.Dispatch(ctx, "INFO",
		fmt.Sprintf("Bypass anomaly threshold: %s", field),
		fmt.Sprintf("Field appeared in %.1f%% of responses in 24h", pct*100),
		"bypass.anomaly")
}
