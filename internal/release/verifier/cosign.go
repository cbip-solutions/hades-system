// SPDX-License-Identifier: MIT

package verifier

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var ErrNoCosignSignature = errors.New("verifier: cosign signature missing")

func ParseCosignSignature(artifactPath string) (*CosignSignature, error) {
	sigPath := artifactPath + ".sig"
	pemPath := artifactPath + ".pem"
	for _, p := range []string{sigPath, pemPath} {
		st, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("%w: %s", ErrNoCosignSignature, p)
			}
			return nil, fmt.Errorf("stat %s: %w", p, err)
		}
		if st.Size() == 0 {
			return nil, fmt.Errorf("cosign signature %s is empty", p)
		}
	}
	return &CosignSignature{
		SigPath:  sigPath,
		PemPath:  pemPath,
		Verified: false,
	}, nil
}

func (v *Verifier) VerifyCosignSignature(art ReleaseArtifact) error {
	if v.Mode == ModeFast {
		return nil
	}
	if v.Owner == "" || v.Repo == "" {
		return errors.New("verifier: Owner + Repo required for VerifyCosignSignature")
	}
	runner := v.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	sig := art.Path + ".sig"
	pem := art.Path + ".pem"

	idRegex := fmt.Sprintf(`^https://github\.com/%s/%s/\.github/workflows/release\.yml@.*$`,
		regexpEscape(v.Owner), regexpEscape(v.Repo))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stdout, stderr, err := runner.Run(ctx, "cosign",
		"verify-blob",
		"--certificate", pem,
		"--signature", sig,
		"--certificate-identity-regexp", idRegex,
		"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
		art.Path,
	)
	if err != nil {
		return fmt.Errorf("cosign verify-blob %s: %w (stderr=%s)",
			filepath.Base(art.Path), err, strings.TrimSpace(string(stderr)))
	}
	if !strings.Contains(string(stdout), "Verified OK") {
		return fmt.Errorf("cosign verify-blob %s: stdout %q (no 'Verified OK')",
			filepath.Base(art.Path), strings.TrimSpace(string(stdout)))
	}
	return nil
}

func regexpEscape(s string) string {
	return regexp.QuoteMeta(s)
}
