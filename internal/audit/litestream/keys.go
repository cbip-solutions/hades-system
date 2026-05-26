// SPDX-License-Identifier: MIT
package litestream

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/cbip-solutions/hades-system/internal/redact"
)

type S3Credentials struct {
	AccessKeyID     redact.Secret
	SecretAccessKey redact.Secret
	Region          string
	Endpoint        string
}

func (c *S3Credentials) Wipe() {
	if c == nil {
		return
	}
	c.AccessKeyID.Wipe()
	c.SecretAccessKey.Wipe()
}

var ErrKeychainNoSuchEntry = errors.New("litestream: keychain has no S3 credentials for project")

var ErrKeychainUnsupported = errors.New("litestream: keychain unsupported on this platform or disabled by env")

var (
	loadKeychainFn   = defaultLoadKeychainFn
	saveKeychainFn   = defaultSaveKeychainFn
	deleteKeychainFn = defaultDeleteKeychainFn
)

type S3CredentialsStore struct{}

func NewS3CredentialsStore() *S3CredentialsStore {
	return &S3CredentialsStore{}
}

func (s *S3CredentialsStore) Load(ctx context.Context, projectID string) (S3Credentials, error) {
	_ = ctx
	if projectID == "" {
		return S3Credentials{}, errors.New("litestream: empty project_id")
	}
	if os.Getenv("ZEN_AUDIT_DISABLE_KEYCHAIN") == "1" {
		return S3Credentials{}, ErrKeychainUnsupported
	}
	return loadKeychainFn(projectID)
}

func (s *S3CredentialsStore) Save(ctx context.Context, projectID string, creds S3Credentials) error {
	_ = ctx
	if projectID == "" {
		return errors.New("litestream: empty project_id")
	}
	if os.Getenv("ZEN_AUDIT_DISABLE_KEYCHAIN") == "1" {
		return ErrKeychainUnsupported
	}
	return saveKeychainFn(projectID, creds)
}

func (s *S3CredentialsStore) Delete(ctx context.Context, projectID string) error {
	_ = ctx
	if projectID == "" {
		return errors.New("litestream: empty project_id")
	}
	if os.Getenv("ZEN_AUDIT_DISABLE_KEYCHAIN") == "1" {
		return ErrKeychainUnsupported
	}
	return deleteKeychainFn(projectID)
}

func s3CredentialsServiceName(projectID string) string {
	return "zen-swarm-audit-s3-" + projectID
}

func defaultLoadKeychainFn(projectID string) (S3Credentials, error) {
	return loadKeychainImpl(projectID)
}

func defaultSaveKeychainFn(projectID string, creds S3Credentials) error {
	return saveKeychainImpl(projectID, creds)
}

func defaultDeleteKeychainFn(projectID string) error {
	return deleteKeychainImpl(projectID)
}

func formatKeychainPayload(creds S3Credentials) ([]byte, error) {
	if len(creds.AccessKeyID.Reveal()) == 0 {
		return nil, fmt.Errorf("litestream: access_key_id required")
	}
	if len(creds.SecretAccessKey.Reveal()) == 0 {
		return nil, fmt.Errorf("litestream: secret_access_key required")
	}
	region := creds.Region
	if region == "" {
		region = "us-east-1"
	}

	body := `{"accessKeyId":` + jsonEscape(string(creds.AccessKeyID.Reveal())) +
		`,"secretAccessKey":` + jsonEscape(string(creds.SecretAccessKey.Reveal())) +
		`,"region":` + jsonEscape(region)
	if creds.Endpoint != "" {
		body += `,"endpoint":` + jsonEscape(creds.Endpoint)
	}
	body += `}`
	return []byte(body), nil
}

func jsonEscape(s string) string {

	b, _ := jsonMarshalString(s)
	return string(b)
}
