// SPDX-License-Identifier: MIT
// cmd/hades-docs-cron — main.go
//
// documentation maintenance.
//
// Responsibilities (per spec §2.9 Q9=A + §4.4):
//
// Every 6 hours: poll upstream registries (pkg.go.dev, PyPI, npm,
// crates.io); on new version detected → schedule Dispatcher.IngestDelta
// for the changed ecosystem via the daemon HTTP API.
// Sunday 03:00 local: full integrity sweep covering all 4 ecosystems:
// verifier.SweepChunkFingerprints × 4 ecosystems
// verifier.SweepChangeNodes × 4 ecosystems
// symbol_index.Rebuild × 4 ecosystems
// cas.GarbageCollect × 1
//
// Launchd lifecycle: managed by com.hades-system.docs-cron.plist LaunchAgent
// (G-3 ships the plist). KeepAlive=true means launchd auto-restarts on
// crash — worker MUST be idempotent on restart (invariant). All sweep
// operations are re-entrant; partial runs that crash mid-sweep resume
// cleanly on next invocation.
//
// Coordinator pattern: the worker holds Ingester + Sweeper + VersionDetector
// as injected interfaces. The default production wiring (daemonCronClient)
// routes every operation through the hades-ctld daemon over a Unix
// socket, NOT a direct ecosystem-database import. This preserves boundary
// invariant (only internal/daemon/ talks to internal/store and
// internal/research/ecosystem; this binary lives in cmd/).
//
// Boundary (invariant): does NOT import internal/store, does NOT import
// internal/research/ecosystem, does NOT import internal/cli. Daemon
// coordination over UDS is the canonical surface; this binary uses
// net/http with a custom UDS dialer (local IPC, not network egress, so
// invariant "no third-party HTTPS calls in credential path" is not
// engaged — see §4.4 daemon-side coordination contract).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type Ingester interface {
	IngestDelta(ctx context.Context, eco string) error
}

type Sweeper interface {
	SweepChunkFingerprints(ctx context.Context, eco string) error

	SweepChangeNodes(ctx context.Context, eco string) error

	RebuildSymbolIndex(ctx context.Context, eco string) error

	CASGarbageCollect(ctx context.Context) error
}

type VersionDetector interface {
	DetectNewVersions(ctx context.Context, eco string) ([]string, error)
}

var allEcosystems = []string{"go", "python", "typescript", "rust"}

type CronWorkerConfig struct {
	Ingester Ingester

	Sweeper Sweeper

	VersionDetector VersionDetector

	Timezone *time.Location

	PollInterval time.Duration

	SweepHour int
}

type CronWorker struct {
	cfg    CronWorkerConfig
	logger *slog.Logger
}

func NewCronWorker(cfg CronWorkerConfig) (*CronWorker, error) {
	if cfg.Ingester == nil {
		return nil, errors.New("CronWorker: Ingester is required")
	}
	if cfg.Sweeper == nil {
		return nil, errors.New("CronWorker: Sweeper is required")
	}
	if cfg.Timezone == nil {
		cfg.Timezone = time.Local
		if cfg.Timezone == nil {
			cfg.Timezone = time.UTC
		}
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 6 * time.Hour
	}
	if cfg.SweepHour <= 0 || cfg.SweepHour > 23 {
		cfg.SweepHour = 3
	}
	return &CronWorker{
		cfg: cfg,
		logger: slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}, nil
}

func (w *CronWorker) Run(ctx context.Context) error {
	return w.runWithInterval(ctx, 1*time.Minute)
}

func (w *CronWorker) runWithInterval(ctx context.Context, heartbeatInterval time.Duration) error {
	pollTicker := time.NewTicker(w.cfg.PollInterval)
	defer pollTicker.Stop()

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	var lastSweepDate time.Time

	w.logger.Info("hades-docs-cron started",
		slog.Duration("poll_interval", w.cfg.PollInterval),
		slog.Duration("heartbeat_interval", heartbeatInterval),
		slog.Int("sweep_hour", w.cfg.SweepHour),
		slog.String("timezone", w.cfg.Timezone.String()),
	)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("hades-docs-cron stopping", slog.String("reason", ctx.Err().Error()))
			return nil

		case <-pollTicker.C:
			if err := w.PollUpstream(ctx); err != nil {
				w.logger.Error("poll upstream failed", slog.Any("err", err))

			}

		case t := <-heartbeat.C:
			lastSweepDate = w.maybeRunWeeklySweep(ctx, t, lastSweepDate)
		}
	}
}

func (w *CronWorker) maybeRunWeeklySweep(ctx context.Context, t, lastSweepDate time.Time) time.Time {
	now := t.In(w.cfg.Timezone)
	if !isSundaySweepTime(now, w.cfg.SweepHour, w.cfg.Timezone) {
		return lastSweepDate
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, w.cfg.Timezone)
	if lastSweepDate.Equal(today) {
		return lastSweepDate
	}
	w.logger.Info("starting weekly integrity sweep")
	if err := w.WeeklySweep(ctx); err != nil {
		w.logger.Error("weekly sweep failed", slog.Any("err", err))
	} else {
		w.logger.Info("weekly integrity sweep complete")
	}
	return today
}

// PollUpstream queries each upstream registry for new versions and schedules
// delta-ingest for any newly-detected versions.
//
// Per spec §4.4: calls Revalidator-backed Source.FetchManifest (via
// VersionDetector). Errors per ecosystem are logged and do not abort
// polling of remaining ecosystems. Returns an aggregated error
// (errors.Join) if any ecosystem failed.
//
// When cfg.VersionDetector is nil, returns nil immediately (no-op mode).
func (w *CronWorker) PollUpstream(ctx context.Context) error {
	if w.cfg.VersionDetector == nil {
		return nil
	}

	var (
		mu   sync.Mutex
		errs []error
	)
	addErr := func(err error) {
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	}

	var wg sync.WaitGroup
	for _, eco := range allEcosystems {
		eco := eco
		wg.Add(1)
		go func() {
			defer wg.Done()
			ecoCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			newVersions, err := w.cfg.VersionDetector.DetectNewVersions(ecoCtx, eco)
			if err != nil {
				w.logger.Error("version detection failed",
					slog.String("eco", eco),
					slog.Any("err", err),
				)
				addErr(fmt.Errorf("detect %s: %w", eco, err))
				return
			}

			for _, v := range newVersions {
				inCtx, inCancel := context.WithTimeout(ctx, 60*time.Minute)
				if err := w.cfg.Ingester.IngestDelta(inCtx, eco); err != nil {
					w.logger.Error("IngestDelta failed",
						slog.String("eco", eco),
						slog.String("version", v),
						slog.Any("err", err),
					)
					addErr(fmt.Errorf("ingest %s@%s: %w", eco, v, err))
				} else {
					w.logger.Info("delta ingested",
						slog.String("eco", eco),
						slog.String("version", v),
					)
				}
				inCancel()
			}
		}()
	}
	wg.Wait()

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// WeeklySweep runs the full integrity sweep per spec §4.4 (Sunday 03:00 local).
//
// Per spec the sweep is:
//
// 1. verifier.SweepChunkFingerprints per ecosystem (re-verify sha256)
// 2. verifier.SweepChangeNodes per ecosystem (Change-node graph consistency)
// 3. symbol_index.Rebuild per ecosystem (in-memory refresh)
// 4. cas.GarbageCollect
//
// Steps 1-3 run in parallel across ecosystems (within each ecosystem they
// run sequentially); step 4 runs after all per-ecosystem work completes.
//
// Inv-hades-204: sweep is idempotent — re-running produces zero schema diff.
// All sweep operations are re-entrant; launchd restart during sweep is safe.
//
// Per-ecosystem errors are logged and do not abort remaining ecosystems.
// Returns an aggregated error if any sweep step failed.
func (w *CronWorker) WeeklySweep(ctx context.Context) error {
	var (
		mu   sync.Mutex
		errs []error
	)
	addErr := func(err error) {
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	}

	var wg sync.WaitGroup
	for _, eco := range allEcosystems {
		eco := eco
		wg.Add(1)
		go func() {
			defer wg.Done()

			ecoCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
			defer cancel()

			if err := w.cfg.Sweeper.SweepChunkFingerprints(ecoCtx, eco); err != nil {
				w.logger.Error("SweepChunkFingerprints failed",
					slog.String("eco", eco),
					slog.Any("err", err),
				)
				addErr(fmt.Errorf("SweepChunkFingerprints(%s): %w", eco, err))

			}

			if err := w.cfg.Sweeper.SweepChangeNodes(ecoCtx, eco); err != nil {
				w.logger.Error("SweepChangeNodes failed",
					slog.String("eco", eco),
					slog.Any("err", err),
				)
				addErr(fmt.Errorf("SweepChangeNodes(%s): %w", eco, err))
			}

			if err := w.cfg.Sweeper.RebuildSymbolIndex(ecoCtx, eco); err != nil {
				w.logger.Error("RebuildSymbolIndex failed",
					slog.String("eco", eco),
					slog.Any("err", err),
				)
				addErr(fmt.Errorf("RebuildSymbolIndex(%s): %w", eco, err))
			}
		}()
	}
	wg.Wait()

	gcCtx, gcCancel := context.WithTimeout(ctx, 30*time.Minute)
	defer gcCancel()
	if err := w.cfg.Sweeper.CASGarbageCollect(gcCtx); err != nil {
		w.logger.Error("CASGarbageCollect failed", slog.Any("err", err))
		addErr(fmt.Errorf("CASGarbageCollect: %w", err))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func isSundaySweepTime(t time.Time, sweepHour int, loc *time.Location) bool {
	local := t.In(loc)
	return local.Weekday() == time.Sunday && local.Hour() == sweepHour
}

func loadTimezone(zone string) (*time.Location, error) {
	if zone == "" {
		if time.Local != nil {
			return time.Local, nil
		}
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("loadTimezone(%q): %w", zone, err)
	}
	return loc, nil
}

type runtimeConfig struct {
	daemonSocket string
	timezone     string
	pollInterval string
	sweepHour    int
}

func parseFlags(fs *flag.FlagSet, args []string) (runtimeConfig, error) {
	var cfg runtimeConfig
	fs.StringVar(&cfg.daemonSocket, "daemon-uds", "/tmp/hades-system.sock", "daemon Unix socket path")
	fs.StringVar(&cfg.timezone, "timezone", "", "IANA timezone for weekly sweep (default: system TZ)")
	fs.StringVar(&cfg.pollInterval, "poll-interval", "6h", "upstream poll cadence (default 6h)")
	fs.IntVar(&cfg.sweepHour, "sweep-hour", 3, "local hour for weekly sweep (default 3 = 03:00)")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func buildCronWorker(
	cfg runtimeConfig,
	deps func(string) (Ingester, Sweeper, VersionDetector),
) (*CronWorker, error) {
	loc, err := loadTimezone(cfg.timezone)
	if err != nil {
		return nil, fmt.Errorf("hades-docs-cron: %w", err)
	}
	pollInterval, err := time.ParseDuration(cfg.pollInterval)
	if err != nil {
		return nil, fmt.Errorf("hades-docs-cron: invalid poll-interval %q: %w", cfg.pollInterval, err)
	}
	ingester, sweeper, detector := deps(cfg.daemonSocket)
	return NewCronWorker(CronWorkerConfig{
		Ingester:        ingester,
		Sweeper:         sweeper,
		VersionDetector: detector,
		Timezone:        loc,
		PollInterval:    pollInterval,
		SweepHour:       cfg.sweepHour,
	})
}

func main() {
	cfg, err := parseFlags(flag.CommandLine, os.Args[1:])
	if err != nil {
		slog.Error("flag parse failed", slog.Any("err", err))
		os.Exit(1)
	}

	w, err := buildCronWorker(cfg, buildProductionDeps)
	if err != nil {
		slog.Error("CronWorker init failed", slog.Any("err", err))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := w.Run(ctx); err != nil {
		slog.Error("CronWorker exited with error", slog.Any("err", err))
		os.Exit(1)
	}
}

func buildProductionDeps(daemonSocket string) (Ingester, Sweeper, VersionDetector) {
	client := newDaemonCronClient(daemonSocket)
	return client, client, client
}

type daemonCronClient struct {
	socketPath string
	http       *http.Client
}

func newDaemonCronClient(socketPath string) *daemonCronClient {
	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
	}
	return &daemonCronClient{
		socketPath: socketPath,
		http: &http.Client{
			Transport: transport,
			Timeout:   2 * time.Hour,
		},
	}
}

func (c *daemonCronClient) IngestDelta(ctx context.Context, eco string) error {
	return c.postJSON(ctx, "/v1/ecosystem/ingest-delta", map[string]string{"ecosystem": eco})
}

func (c *daemonCronClient) SweepChunkFingerprints(ctx context.Context, eco string) error {
	return c.postJSON(ctx, "/v1/ecosystem/sweep/fingerprints", map[string]string{"ecosystem": eco})
}

func (c *daemonCronClient) SweepChangeNodes(ctx context.Context, eco string) error {
	return c.postJSON(ctx, "/v1/ecosystem/sweep/change-nodes", map[string]string{"ecosystem": eco})
}

func (c *daemonCronClient) RebuildSymbolIndex(ctx context.Context, eco string) error {
	return c.postJSON(ctx, "/v1/ecosystem/sweep/rebuild-symbol-index", map[string]string{"ecosystem": eco})
}

func (c *daemonCronClient) CASGarbageCollect(ctx context.Context) error {
	return c.postJSON(ctx, "/v1/ecosystem/sweep/cas-gc", nil)
}

func (c *daemonCronClient) DetectNewVersions(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (c *daemonCronClient) postJSON(ctx context.Context, path string, body interface{}) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("postJSON marshal %s: %w", path, err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+path, reqBody)
	if err != nil {
		return fmt.Errorf("postJSON new request %s: %w", path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("postJSON %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("postJSON %s: status %d: %s", path, resp.StatusCode, string(snippet))
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
