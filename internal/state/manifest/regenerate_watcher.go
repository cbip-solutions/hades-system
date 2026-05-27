// SPDX-License-Identifier: MIT
package manifest

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const DefaultDebounce = 5 * time.Second

type WatcherConfig struct {
	ManifestPath string

	AuthSources []string

	Walker *Walker

	Regenerator *Regenerator

	Appender EventAppender

	Debounce time.Duration

	OnRegenerated func()
}

type RegenerateWatcher struct {
	cfg   WatcherConfig
	w     *fsnotify.Watcher
	stopC chan struct{}
	wg    sync.WaitGroup
	once  sync.Once
}

func NewRegenerateWatcher(cfg WatcherConfig) *RegenerateWatcher {
	if cfg.Debounce == 0 {
		cfg.Debounce = DefaultDebounce
	}
	return &RegenerateWatcher{
		cfg:   cfg,
		stopC: make(chan struct{}),
	}
}

// Start validates the configuration, creates an fsnotify.Watcher, adds each
// AuthSources path (fail-soft on individual add failures), and spawns the
// debounce-loop goroutine.
//
// Returns an error when:
// - cfg.Walker is nil.
// - cfg.Regenerator is nil.
// - fsnotify.NewWatcher fails (OS-level file-descriptor exhaustion etc.).
//
// Once Start returns nil, Stop MUST eventually be called to release the
// fsnotify file descriptor and drain the goroutine.
func (rw *RegenerateWatcher) Start(ctx context.Context) error {
	if rw.cfg.Walker == nil || rw.cfg.Regenerator == nil {
		return errors.New("manifest: RegenerateWatcher requires non-nil Walker and Regenerator")
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	rw.w = fw

	for _, src := range rw.cfg.AuthSources {
		if addErr := fw.Add(src); addErr != nil {

			_ = addErr
		}
	}

	rw.wg.Add(1)
	go rw.loop(ctx)
	return nil
}

func (rw *RegenerateWatcher) Stop() {
	rw.once.Do(func() {
		close(rw.stopC)
		if rw.w != nil {
			_ = rw.w.Close()
		}
	})
	rw.wg.Wait()
}

func (rw *RegenerateWatcher) loop(ctx context.Context) {
	defer rw.wg.Done()

	var pending bool

	timer := time.NewTimer(rw.cfg.Debounce)
	if !timer.Stop() {
		<-timer.C
	}

	for {
		select {
		case <-ctx.Done():
			return

		case <-rw.stopC:
			return

		case _, ok := <-rw.eventsCh():
			if !ok {

				return
			}
			pending = true

			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(rw.cfg.Debounce)

		case <-timer.C:
			if !pending {
				continue
			}
			pending = false
			rw.runRegenerate(ctx)
		}
	}
}

func (rw *RegenerateWatcher) eventsCh() <-chan fsnotify.Event {
	if rw.w == nil {
		closed := make(chan fsnotify.Event)
		close(closed)
		return closed
	}
	return rw.w.Events
}

func (rw *RegenerateWatcher) runRegenerate(ctx context.Context) {
	res, err := rw.cfg.Walker.Walk(ctx)
	if err != nil {

		return
	}

	if err := rw.cfg.Regenerator.RegenerateAndWrite(ctx, res.Manifest, rw.cfg.ManifestPath); err != nil {
		return
	}

	now := time.Now().UTC()
	if rw.cfg.Appender != nil {
		if len(res.MissingSources) > 0 {

			_ = rw.cfg.Appender.AppendEvent(ctx, EventPayload{
				Type:           TypeStateRegeneratePartial,
				MissingSources: res.MissingSources,
				Timestamp:      now,
			})
		} else {

			_ = rw.cfg.Appender.AppendEvent(ctx, EventPayload{
				Type:      TypeStateRegenerated,
				Timestamp: now,
			})
		}
	}

	if rw.cfg.OnRegenerated != nil {
		rw.cfg.OnRegenerated()
	}
}

func DefaultAuthSources(repoRoot string) []string {
	return []string{
		filepath.Join(repoRoot, "docs", "decisions"),
		filepath.Join(repoRoot, "go.mod"),
		filepath.Join(repoRoot, "internal", "doctrine"),
	}
}
