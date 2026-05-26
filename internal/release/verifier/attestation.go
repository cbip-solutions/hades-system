// SPDX-License-Identifier: MIT

package verifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrNoAttestation = errors.New("verifier: no attestation file (.intoto.jsonl) found alongside artifact")

func ParseAttestation(artifactPath string) (*AttestationBundle, error) {
	attPath := artifactPath + ".intoto.jsonl"
	data, err := os.ReadFile(attPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoAttestation
		}
		return nil, fmt.Errorf("read attestation %s: %w", attPath, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("attestation %s is empty", attPath)
	}

	firstLine := data
	if idx := strings.IndexByte(string(data), '\n'); idx >= 0 {
		firstLine = data[:idx]
	}
	var probe map[string]interface{}
	if err := json.Unmarshal(firstLine, &probe); err != nil {
		return nil, fmt.Errorf("attestation %s: invalid JSONL first line: %w", attPath, err)
	}
	if t, ok := probe["_type"].(string); !ok || !strings.HasPrefix(t, "https://in-toto.io/Statement/") {
		return nil, fmt.Errorf("attestation %s: missing in-toto _type", attPath)
	}
	return &AttestationBundle{
		Path:       attPath,
		InToto:     string(data),
		OIDCIssuer: "https://token.actions.githubusercontent.com",
		Verified:   false,
	}, nil
}

func (v *Verifier) VerifyAttestation(art ReleaseArtifact) error {
	if v.Mode == ModeFast {
		return nil
	}
	if v.Owner == "" || v.Repo == "" {
		return errors.New("verifier: Owner + Repo required for VerifyAttestation")
	}
	runner := v.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stdout, stderr, err := runner.Run(ctx, "gh",
		"attestation", "verify",
		art.Path,
		"--owner", v.Owner,
		"--repo", v.Repo,
		"--format", "json",
	)
	if err != nil {
		return fmt.Errorf("gh attestation verify %s: %w (stderr=%s)",
			filepath.Base(art.Path), err, strings.TrimSpace(string(stderr)))
	}
	if !strings.Contains(string(stdout), "Verification successful") &&
		!strings.Contains(string(stdout), "verified") {
		return fmt.Errorf("gh attestation verify %s: unexpected stdout %q",
			filepath.Base(art.Path), strings.TrimSpace(string(stdout)))
	}
	return nil
}
