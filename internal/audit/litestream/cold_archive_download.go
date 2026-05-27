// SPDX-License-Identifier: MIT
package litestream

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

type ColdArchiveDownloader struct {
	starter    ExecStarter
	credsStore *S3CredentialsStore
}

func NewColdArchiveDownloader(starter ExecStarter, credsStore *S3CredentialsStore) *ColdArchiveDownloader {
	if starter == nil {
		starter = exec.CommandContext
	}
	if credsStore == nil {
		credsStore = NewS3CredentialsStore()
	}
	return &ColdArchiveDownloader{starter: starter, credsStore: credsStore}
}

func (d *ColdArchiveDownloader) DownloadColdArchive(ctx context.Context, projectID, partitionID, dst string) error {
	if projectID == "" || partitionID == "" || dst == "" {
		return errors.New("cold-archive: project_id, partition_id, and dst required")
	}
	creds, err := d.credsStore.Load(ctx, projectID)
	if err != nil {
		return err
	}
	defer creds.Wipe()
	region := creds.Region
	if region == "" {
		region = "us-east-1"
	}
	args := []string{"s3", "cp", S3KeyForColdArchive(projectID, partitionID), dst, "--quiet"}
	if creds.Endpoint != "" {
		args = append(args, "--endpoint-url", creds.Endpoint)
	}
	cmd := d.starter(ctx, "aws", args...)
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = append(env,
		"AWS_ACCESS_KEY_ID="+string(creds.AccessKeyID.Reveal()),
		"AWS_SECRET_ACCESS_KEY="+string(creds.SecretAccessKey.Reveal()),
		"AWS_DEFAULT_REGION="+region,
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cold-archive: aws s3 cp restore: %w", err)
	}
	return nil
}
