// SPDX-License-Identifier: MIT
package litestream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

func RsyncCadenceForDoctrine(doctrine string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(doctrine)) {
	case "default":
		return 7 * 24 * time.Hour
	case "capa-firewall", "max-scope", "":
		return 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

type rsyncProject struct {
	cancel      context.CancelFunc
	done        chan struct{}
	mu          sync.Mutex
	lastSuccess time.Time
	lastError   string
	lastErrorAt time.Time
}

type RsyncScheduler struct {
	starter ExecStarter
	mu      sync.Mutex
	per     map[string]*rsyncProject

	cadence time.Duration
}

func NewRsyncScheduler(starter ExecStarter) *RsyncScheduler {
	return &RsyncScheduler{
		starter: starter,
		per:     make(map[string]*rsyncProject),
		cadence: 24 * time.Hour,
	}
}

// StartProject schedules rsync for project_id from the local Tessera
// directory tree to s3://hades-system-audit-<project_id>/tessera/. env is
// the AWS env vars (AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY +
// AWS_DEFAULT_REGION) provided by LifecycleManager.AwsEnvForProject;
// nil/empty env means the subprocess inherits the parent's environment
// unmodified (which the daemon process MUST NOT carry — the daemon's
// own env is AWS-key-free).
//
// Returns ErrProjectAlreadyManaged if project_id is already running
// — the wrapped error includes a "(rsync)" suffix so an operator can
// disambiguate from the litestream-supervisor variant.
func (s *RsyncScheduler) StartProject(ctx context.Context, projectID, tesseraDir string, env []string) error {
	if projectID == "" {
		return errors.New("rsync scheduler: empty project_id")
	}
	if tesseraDir == "" {
		return errors.New("rsync scheduler: empty tessera dir")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.per[projectID]; ok {
		return fmt.Errorf("%w: %s (rsync)", ErrProjectAlreadyManaged, projectID)
	}
	pCtx, cancel := context.WithCancel(ctx)
	p := &rsyncProject{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	s.per[projectID] = p

	go s.run(pCtx, projectID, tesseraDir, env, p)
	return nil
}

func (s *RsyncScheduler) StopProject(ctx context.Context, projectID string) error {
	s.mu.Lock()
	p, ok := s.per[projectID]
	if ok {
		delete(s.per, projectID)
	}
	s.mu.Unlock()
	if !ok {
		return nil
	}
	p.cancel()
	select {
	case <-p.done:
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("rsync scheduler: stop timeout for %s", projectID)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *RsyncScheduler) LastSuccess(projectID string) time.Time {
	s.mu.Lock()
	p, ok := s.per[projectID]
	s.mu.Unlock()
	if !ok {
		return time.Time{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastSuccess
}

func (s *RsyncScheduler) LastError(projectID string) string {
	s.mu.Lock()
	p, ok := s.per[projectID]
	s.mu.Unlock()
	if !ok {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastError
}

func (s *RsyncScheduler) run(ctx context.Context, projectID, tesseraDir string, env []string, p *rsyncProject) {
	defer close(p.done)
	first := true
	for {
		if !first {
			select {
			case <-time.After(s.cadence):
			case <-ctx.Done():
				return
			}
		}
		first = false
		s.runOnce(ctx, projectID, tesseraDir, env, p)
		if ctx.Err() != nil {
			return
		}
	}
}

func (s *RsyncScheduler) runOnce(ctx context.Context, projectID, tesseraDir string, env []string, p *rsyncProject) {
	if s.starter == nil {
		return
	}
	bucket := "s3://hades-system-audit-" + projectID + "/tessera/"
	cmd := s.starter(ctx, "aws", "s3", "sync", tesseraDir, bucket, "--delete", "--quiet")
	if len(env) > 0 {
		cmd.Env = append(append([]string(nil), os.Environ()...), env...)
	}
	err := cmd.Run()
	now := time.Now().UTC()
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.lastError = err.Error()
		p.lastErrorAt = now
		return
	}
	p.lastSuccess = now
	p.lastError = "" // clear on success — stale errors do not pollute doctor
}
