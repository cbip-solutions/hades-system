// SPDX-License-Identifier: MIT
package litestream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/redact"
)

const (
	minAccessKeyIDLength     = 16
	minSecretAccessKeyLength = 20
)

func ConfigureS3Interactive(ctx context.Context, projectID string, stdin io.Reader, stdout io.Writer) error {
	if projectID == "" {
		return fmt.Errorf("litestream: empty project_id")
	}
	br := bufio.NewReader(stdin)

	fmt.Fprintf(stdout, "Configuring S3 credentials for project %s\n", projectID)
	fmt.Fprintln(stdout, "Bucket name will be: zen-swarm-audit-"+projectID)
	fmt.Fprintln(stdout, "(create the bucket separately via your AWS console / aws-cli)")
	fmt.Fprintln(stdout, "")

	fmt.Fprint(stdout, "AWS Access Key ID: ")
	rawAccess, err := readLine(br)
	if err != nil {
		return fmt.Errorf("litestream: read access-key: %w", err)
	}
	access := strings.TrimSpace(rawAccess)
	if len(access) < minAccessKeyIDLength {
		return fmt.Errorf("litestream: access key id too short (got %d chars, need >=%d)", len(access), minAccessKeyIDLength)
	}

	fmt.Fprint(stdout, "AWS Secret Access Key (input hidden): ")
	rawSecret, err := readLine(br)
	if err != nil {
		return fmt.Errorf("litestream: read secret: %w", err)
	}
	secret := strings.TrimSpace(rawSecret)
	if len(secret) < minSecretAccessKeyLength {
		return fmt.Errorf("litestream: secret access key too short (got %d chars, need >=%d)", len(secret), minSecretAccessKeyLength)
	}

	fmt.Fprintln(stdout, "(secret received)")

	fmt.Fprint(stdout, "Region [us-east-1]: ")
	rawRegion, err := readLine(br)
	if err != nil {
		return fmt.Errorf("litestream: read region: %w", err)
	}
	region := strings.TrimSpace(rawRegion)
	if region == "" {
		region = "us-east-1"
	}

	fmt.Fprint(stdout, "Custom S3 endpoint (leave blank for AWS): ")
	rawEndpoint, err := readLine(br)
	if err != nil {
		return fmt.Errorf("litestream: read endpoint: %w", err)
	}
	endpoint := strings.TrimSpace(rawEndpoint)

	creds := S3Credentials{
		AccessKeyID:     redact.NewSecret(access),
		SecretAccessKey: redact.NewSecret(secret),
		Region:          region,
		Endpoint:        endpoint,
	}
	defer creds.Wipe()

	store := NewS3CredentialsStore()
	if err := store.Save(ctx, projectID, creds); err != nil {
		return fmt.Errorf("litestream: save credentials: %w", err)
	}
	fmt.Fprintf(stdout, "Saved S3 credentials to Keychain for project %s\n", projectID)
	fmt.Fprintln(stdout, "(daemon restart picks up new credentials at next litestream subprocess respawn)")
	return nil
}

func readLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
