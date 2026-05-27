// SPDX-License-Identifier: MIT
package litestream

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// projectManager is the surface area LifecycleManager needs from
// Manager (or a fake). Defined as an interface so the C-4 lifecycle
// tests do not have to launch real subprocesses (those are exercised
// by the C-1 manager tests).
//
// Manager satisfies this interface via its existing StartProject /
// StopProject / StopAll methods plus the SetEnvForProject method below.
type projectManager interface {
	StartProject(ctx context.Context, projectID, cfgPath string) error
	StopProject(ctx context.Context, projectID string) error
	StopAll(ctx context.Context) error
	SetEnvForProject(fn func(projectID string) []string)
}

func (m *Manager) SetEnvForProject(fn func(projectID string) []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.envForProject = fn
}

type LifecycleManager struct {
	mgr         projectManager
	credsStore  *S3CredentialsStore
	configDir   string
	dbPathFor   func(projectID string) string
	doctrineFor func(projectID string) string
	onSkip      func(projectID, reason string)
}

func NewLifecycleManager(
	mgr projectManager,
	credsStore *S3CredentialsStore,
	configDir string,
	dbPathFor func(string) string,
	doctrineFor func(string) string,
	onSkip func(string, string),
) *LifecycleManager {
	if onSkip == nil {
		onSkip = func(string, string) {}
	}
	return &LifecycleManager{
		mgr:         mgr,
		credsStore:  credsStore,
		configDir:   configDir,
		dbPathFor:   dbPathFor,
		doctrineFor: doctrineFor,
		onSkip:      onSkip,
	}
}

func (lc *LifecycleManager) StartAllProjects(ctx context.Context, projectIDs []string) error {
	lc.mgr.SetEnvForProject(lc.envForProject)

	for _, id := range projectIDs {
		creds, err := lc.credsStore.Load(ctx, id)
		if err != nil {
			if errors.Is(err, ErrKeychainNoSuchEntry) {
				lc.onSkip(id, "no S3 credentials in Keychain (run `hades audit configure-s3 --project "+id+"`)")
				continue
			}
			if errors.Is(err, ErrKeychainUnsupported) {
				lc.onSkip(id, "Keychain unsupported on this platform / disabled by env (litestream replication unavailable)")
				continue
			}
			return fmt.Errorf("litestream lifecycle: load creds for %s: %w", id, err)
		}

		creds.Wipe()

		dbPath := lc.dbPathFor(id)
		doctrine := lc.doctrineFor(id)
		cfg := BuildConfig(id, doctrine, dbPath)
		cfgPath := filepath.Join(lc.configDir, id+".yml")
		if err := WriteConfig(cfg, cfgPath); err != nil {
			return fmt.Errorf("litestream lifecycle: write config for %s: %w", id, err)
		}
		if err := lc.mgr.StartProject(ctx, id, cfgPath); err != nil {
			return fmt.Errorf("litestream lifecycle: start project %s: %w", id, err)
		}
	}
	return nil
}

func (lc *LifecycleManager) StopAll(ctx context.Context) error {
	return lc.mgr.StopAll(ctx)
}

func (lc *LifecycleManager) envForProject(projectID string) []string {
	creds, err := lc.credsStore.Load(context.Background(), projectID)
	if err != nil {
		return nil
	}
	defer creds.Wipe()
	return []string{
		"LITESTREAM_ACCESS_KEY_ID=" + string(creds.AccessKeyID.Reveal()),
		"LITESTREAM_SECRET_ACCESS_KEY=" + string(creds.SecretAccessKey.Reveal()),
	}
}

func (lc *LifecycleManager) awsEnvForProject(projectID string) []string {
	creds, err := lc.credsStore.Load(context.Background(), projectID)
	if err != nil {
		return nil
	}
	defer creds.Wipe()
	region := creds.Region
	if region == "" {
		region = "us-east-1"
	}
	return []string{
		"AWS_ACCESS_KEY_ID=" + string(creds.AccessKeyID.Reveal()),
		"AWS_SECRET_ACCESS_KEY=" + string(creds.SecretAccessKey.Reveal()),
		"AWS_DEFAULT_REGION=" + region,
	}
}

func (lc *LifecycleManager) AwsEnvForProject(projectID string) []string {
	return lc.awsEnvForProject(projectID)
}
