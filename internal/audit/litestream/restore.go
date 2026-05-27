// SPDX-License-Identifier: MIT
package litestream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type DBRestorer struct {
	starter    ExecStarter
	credsStore *S3CredentialsStore
}

func NewDBRestorer(starter ExecStarter, credsStore *S3CredentialsStore) *DBRestorer {
	if starter == nil {
		starter = exec.CommandContext
	}
	if credsStore == nil {
		credsStore = NewS3CredentialsStore()
	}
	return &DBRestorer{starter: starter, credsStore: credsStore}
}

func (r *DBRestorer) RestoreFromS3(ctx context.Context, projectID string, fromTS time.Time, stagingDir string) error {
	if projectID == "" || fromTS.IsZero() || stagingDir == "" {
		return errors.New("litestream restore: project_id, timestamp, and stagingDir required")
	}
	creds, err := r.credsStore.Load(ctx, projectID)
	if err != nil {
		return err
	}
	defer creds.Wipe()
	region := creds.Region
	if region == "" {
		region = "us-east-1"
	}
	auditDir := filepath.Join(stagingDir, "projects", projectID, "audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		return fmt.Errorf("litestream restore: mkdir audit staging: %w", err)
	}
	dbPath := filepath.Join(auditDir, "audit.db")
	cfg := BuildConfig(projectID, "max-scope", dbPath)
	if len(cfg.DBs) > 0 && len(cfg.DBs[0].Replicas) > 0 {
		cfg.DBs[0].Replicas[0].Region = region
		cfg.DBs[0].Replicas[0].Endpoint = creds.Endpoint
	}
	cfgPath := filepath.Join(stagingDir, "litestream-"+projectID+".yml")
	if err := WriteConfig(cfg, cfgPath); err != nil {
		return fmt.Errorf("litestream restore: write config: %w", err)
	}
	cmd := r.starter(ctx, "litestream",
		"restore",
		"-config", cfgPath,
		"-timestamp", fromTS.UTC().Format(time.RFC3339),
		"-o", dbPath,
		dbPath,
	)
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = append(env,
		"LITESTREAM_ACCESS_KEY_ID="+string(creds.AccessKeyID.Reveal()),
		"LITESTREAM_SECRET_ACCESS_KEY="+string(creds.SecretAccessKey.Reveal()),
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("litestream restore: command: %w", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("litestream restore: restored audit.db missing: %w", err)
	}
	return nil
}
