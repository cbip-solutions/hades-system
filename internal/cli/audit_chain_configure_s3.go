// SPDX-License-Identifier: MIT
// Package cli — audit_chain_configure_s3.go.
//
// `zen audit-chain configure-s3 --project <id>` walks the operator through
// interactively configuring per-project S3 backup credentials (endpoint,
// bucket, region, access key, secret key). Credentials are transmitted over
// the local UDS socket; the daemon adapter rotates them into macOS Keychain
// (darwin) or a 0600 file (non-darwin) — never stored by the CLI itself.
//
// Privacy-by-default: the success message confirms storage (project, bucket,
// endpoint) but NEVER echoes the secret key or access key back to stdout or
// stderr. If an error message from the daemon inadvertently contains the
// secret, it is redacted before printing.
//
// invariant boundary: the CLI calls the daemon endpoint
// POST /v1/audit-chain/configure-s3 (client.AuditConfigureS3); the CLI
// NEVER touches Keychain directly.
//
// Plan deviation (implementer): plan-file used AuditConfigureS3Req struct and
// AuditChainConfigureS3Resp return type. H-7 actually shipped:
//
// client.AuditConfigureS3(ctx, projectID string, creds AuditS3Credentials) error
//
// The daemon returns 204 No Content on success; the client method returns nil.
// AuditS3Credentials{Endpoint, Bucket, Region, AccessKey, SecretKey, Prefix}.
// Prefix is optional (empty string accepted).
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func auditChainConfigureS3Cmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "configure-s3",
		Short: "Interactive S3 credential setup (Keychain integrated, Plan 2 pattern)",
		Long: `configure-s3 walks the operator through interactively configuring
per-project S3 backup credentials for Tessera cold archiving. Prompts for
endpoint URL, bucket name, region, access key, and secret key.

Credentials are transmitted over the local daemon UDS socket; the daemon
adapter stores them in macOS Keychain (darwin) or
~/.config/zen-swarm/s3-credentials.json mode 0600 (non-darwin).

The secret is NEVER echoed back to stdout or stderr (privacy-by-default;
Plan 2 pattern). The success message confirms project ID, bucket, and
endpoint only.

Required: --project.`,
		Example: `  zen audit-chain configure-s3 --project zen-swarm`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			project, _ := cmd.Flags().GetString("project")
			if strings.TrimSpace(project) == "" {

				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--project required and must be non-empty"))
			}

			out := cmd.OutOrStdout()

			fmt.Fprintf(out, "Configuring S3 backup credentials for project %s.\n", project)
			fmt.Fprintln(out, "Five fields will be requested. The secret is never echoed back.")
			fmt.Fprintln(out)

			br := bufio.NewReader(cmd.InOrStdin())
			readField := func(prompt string) (string, error) {
				if _, err := fmt.Fprintf(out, "%s: ", prompt); err != nil {
					return "", err
				}
				line, err := br.ReadString('\n')
				if err != nil && err != io.EOF {
					return "", err
				}
				return strings.TrimSpace(line), nil
			}

			endpoint, err := readField("S3 endpoint (e.g. s3.us-east-2.amazonaws.com)")
			if err != nil {
				return err
			}
			bucket, err := readField("Bucket name")
			if err != nil {
				return err
			}
			region, err := readField("Region (e.g. us-east-2)")
			if err != nil {
				return err
			}
			accessKey, err := readField("Access key ID")
			if err != nil {
				return err
			}
			secretKey, err := readField("Secret access key")
			if err != nil {
				return err
			}

			if strings.TrimSpace(endpoint) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("endpoint is required"))
			}
			if strings.TrimSpace(bucket) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("bucket is required"))
			}
			if strings.TrimSpace(accessKey) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("access key is required"))
			}
			if strings.TrimSpace(secretKey) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("secret key is required"))
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			creds := client.AuditS3Credentials{
				Endpoint:  endpoint,
				Bucket:    bucket,
				Region:    region,
				AccessKey: accessKey,
				SecretKey: secretKey,
			}
			if err := newClientFromCmd(cmd).AuditConfigureS3(ctx, project, creds); err != nil {

				msg := err.Error()
				if strings.Contains(msg, secretKey) && secretKey != "" {
					return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("daemon error (secret redacted)"))
				}
				return err
			}

			fmt.Fprintf(out, "\nStored S3 credentials.\n")
			fmt.Fprintf(out, "  project:  %s\n", project)
			fmt.Fprintf(out, "  bucket:   %s\n", bucket)
			fmt.Fprintf(out, "  endpoint: %s\n", endpoint)
			if strings.TrimSpace(region) != "" {
				fmt.Fprintf(out, "  region:   %s\n", region)
			}
			fmt.Fprintln(out, "(Secret stored in Keychain on darwin / mode 0600 file otherwise.)")
			return nil
		},
	}
	c.Flags().String("project", "", "Project ID (required)")
	_ = c.MarkFlagRequired("project")
	return c
}
