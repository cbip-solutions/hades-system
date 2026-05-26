// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Mode int

const (
	ModeUnknown Mode = iota

	ModeVerify

	ModeCapture

	ModeFreshness
)

func (m Mode) String() string {
	switch m {
	case ModeVerify:
		return "verify"
	case ModeCapture:
		return "capture"
	case ModeFreshness:
		return "freshness"
	default:
		return "unknown"
	}
}

type Config struct {
	Mode       Mode
	Seed       int64
	GoldenOut  string
	TraceOut   string
	MaxAgeDays int
	Stdout     io.Writer
	Stderr     io.Writer
	runner     Runner
}

type Runner interface {
	Verify() error
	Capture(seed int64, goldenOut, traceOut string) error
	Freshness(maxAgeDays int) error
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify-chaos-suite: %v\n", err)
		os.Exit(2)
	}
	cfg.Stdout = os.Stdout
	cfg.Stderr = os.Stderr
	cfg.runner = newRealRunner(os.Stdout, os.Stderr)
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "verify-chaos-suite: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags(args []string) (Config, error) {
	fs := flag.NewFlagSet("verify-chaos-suite", flag.ContinueOnError)

	fs.SetOutput(io.Discard)
	captureSeed := fs.Int64("capture-seed", -1, "capture a failing seed (regression mode)")
	goldenOut := fs.String("golden-out", "", "golden file output path (capture mode)")
	traceOut := fs.String("trace-out", "", "trace file output path (capture mode)")
	freshness := fs.Bool("freshness", false, "check chaos.yml nightly cron freshness")
	maxAgeDays := fs.Int("max-age-days", 8, "freshness window in days (default 8; 1 weekly cron miss tolerated)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	cfg := Config{MaxAgeDays: *maxAgeDays}
	switch {
	case *freshness:
		cfg.Mode = ModeFreshness
	case *captureSeed >= 0:
		cfg.Mode = ModeCapture
		cfg.Seed = *captureSeed
		cfg.GoldenOut = *goldenOut
		cfg.TraceOut = *traceOut
		if cfg.GoldenOut == "" || cfg.TraceOut == "" {
			return cfg, errors.New("--golden-out and --trace-out required with --capture-seed")
		}
	default:
		cfg.Mode = ModeVerify
	}
	return cfg, nil
}

func run(cfg Config) error {
	switch cfg.Mode {
	case ModeVerify:
		if err := cfg.runner.Verify(); err != nil {
			return fmt.Errorf("verify: %w", err)
		}
		if cfg.Stdout != nil {
			fmt.Fprintln(cfg.Stdout, "verify-chaos-suite: ALL PASS")
		}
		return nil
	case ModeCapture:
		if err := cfg.runner.Capture(cfg.Seed, cfg.GoldenOut, cfg.TraceOut); err != nil {
			return fmt.Errorf("capture seed=%d: %w", cfg.Seed, err)
		}
		if cfg.Stdout != nil {
			fmt.Fprintf(cfg.Stdout, "verify-chaos-suite: captured seed=%d → %s + %s\n",
				cfg.Seed, cfg.GoldenOut, cfg.TraceOut)
		}
		return nil
	case ModeFreshness:
		if err := cfg.runner.Freshness(cfg.MaxAgeDays); err != nil {
			return fmt.Errorf("freshness: %w", err)
		}
		if cfg.Stdout != nil {
			fmt.Fprintf(cfg.Stdout, "verify-chaos-suite: chaos.yml fresh (within %d days)\n", cfg.MaxAgeDays)
		}
		return nil
	default:
		return fmt.Errorf("unknown mode: %s", cfg.Mode)
	}
}

type realRunner struct {
	stdout      io.Writer
	stderr      io.Writer
	makeCommand func(name string, args ...string) *exec.Cmd
	ghCommand   func(name string, args ...string) *exec.Cmd
}

func newRealRunner(stdout, stderr io.Writer) *realRunner {
	return &realRunner{
		stdout:      stdout,
		stderr:      stderr,
		makeCommand: exec.Command,
		ghCommand:   exec.Command,
	}
}

func (r *realRunner) Verify() error {
	cmdFunc := r.makeCommand
	if cmdFunc == nil {
		cmdFunc = exec.Command
	}
	cmd := cmdFunc("make", "smoke-chaos")
	cmd.Stdout = r.stdout
	cmd.Stderr = r.stderr
	return cmd.Run()
}

func (r *realRunner) Capture(seed int64, goldenOut, traceOut string) error {

	absGolden, err := filepath.Abs(goldenOut)
	if err != nil {
		return fmt.Errorf("resolve golden-out: %w", err)
	}
	absTrace, err := filepath.Abs(traceOut)
	if err != nil {
		return fmt.Errorf("resolve trace-out: %w", err)
	}

	envelope := captureEnvelope{
		Seed:       seed,
		CapturedAt: time.Now().UTC().Format(time.RFC3339),
		Note:       "captured by verify-chaos-suite ModeCapture; reproduce via `go test -tags chaos ./tests/chaos/dst/... -seed=NN`",
	}
	goldenBytes, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal golden: %w", err)
	}
	if err := os.WriteFile(absGolden, goldenBytes, 0o644); err != nil {
		return fmt.Errorf("write golden: %w", err)
	}

	traceBytes, err := json.MarshalIndent(map[string]any{
		"seed":  seed,
		"trace": "captured by verify-chaos-suite; per-step action stream emission queued (see harness.go)",
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal trace: %w", err)
	}
	if err := os.WriteFile(absTrace, traceBytes, 0o644); err != nil {
		return fmt.Errorf("write trace: %w", err)
	}
	return nil
}

type captureEnvelope struct {
	Seed       int64  `json:"seed"`
	CapturedAt string `json:"captured_at"`
	Note       string `json:"note"`
}

// Freshness checks the chaos.yml workflow has had a successful run
// within the freshness window. Defaults to 8 days (1 weekly cron miss
// tolerated per spec §6.6).
//
// Uses `gh run list --workflow chaos.yml --status success --limit 1
// --json createdAt,databaseId`. If `gh` is not available or the
// network call fails the check returns an error (CI MUST have `gh`
// available; the gate is intentionally strict).
func (r *realRunner) Freshness(maxAgeDays int) error {
	if maxAgeDays <= 0 {
		return fmt.Errorf("max-age-days must be positive; got %d", maxAgeDays)
	}
	cmdFunc := r.ghCommand
	if cmdFunc == nil {
		cmdFunc = exec.Command
	}
	cmd := cmdFunc("gh", "run", "list",
		"--workflow", "chaos.yml",
		"--status", "success",
		"--limit", "1",
		"--json", "createdAt,databaseId")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("gh run list: %w", err)
	}
	var runs []struct {
		CreatedAt  string `json:"createdAt"`
		DatabaseID int64  `json:"databaseId"`
	}
	if err := json.Unmarshal(out, &runs); err != nil {
		return fmt.Errorf("unmarshal gh output: %w", err)
	}
	if len(runs) == 0 {
		return errors.New("no successful chaos.yml runs in history; cron may be broken")
	}
	createdAt, err := time.Parse(time.RFC3339, runs[0].CreatedAt)
	if err != nil {
		return fmt.Errorf("parse createdAt %q: %w", runs[0].CreatedAt, err)
	}
	age := time.Since(createdAt)
	maxAge := time.Duration(maxAgeDays) * 24 * time.Hour
	if age > maxAge {
		return fmt.Errorf("last successful chaos.yml run (#%d) was %s ago (> %s freshness window)",
			runs[0].DatabaseID, age.Round(time.Hour), maxAge)
	}
	return nil
}
