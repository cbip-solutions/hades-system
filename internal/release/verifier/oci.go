// SPDX-License-Identifier: MIT

package verifier

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// VerifyOCIImageSignature invokes `cosign verify <imageRef>` for the
// Docker GHCR image. imageRef MUST be the immutable form
// `ghcr.io/<owner>/<repo>@sha256:<digest>`. A mutable tag (`:v1.0.0`)
// rejected because the tag could be republished after verification.
//
// Requires Verifier.Owner + Verifier.Repo. Uses the same identity regex
// shape as VerifyCosignSignature.
//
// Mode gating (K-8 review M1 fix): when v.Mode == ModeFast, this method is
// a no-op (returns nil) — the cosign subprocess + Rekor network call is
// skipped for offline-friendly invocation.
//
// invariant OCI image attestation delegation ( owns
// implementation; verifier consumes).
func (v *Verifier) VerifyOCIImageSignature(imageRef string) error {
	if v.Mode == ModeFast {
		return nil
	}
	if v.Owner == "" || v.Repo == "" {
		return errors.New("verifier: Owner + Repo required for VerifyOCIImageSignature")
	}
	if !strings.HasPrefix(imageRef, "ghcr.io/") {
		return fmt.Errorf("verifier: imageRef must be ghcr.io/... (got %q)", imageRef)
	}
	if !strings.Contains(imageRef, "@sha256:") {
		return fmt.Errorf("verifier: imageRef must include @sha256:<digest> for immutable verification (got %q)", imageRef)
	}
	runner := v.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	idRegex := fmt.Sprintf(`^https://github\.com/%s/%s/\.github/workflows/release\.yml@.*$`,
		regexpEscape(v.Owner), regexpEscape(v.Repo))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stdout, stderr, err := runner.Run(ctx, "cosign",
		"verify", imageRef,
		"--certificate-identity-regexp", idRegex,
		"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
	)
	if err != nil {
		return fmt.Errorf("cosign verify %s: %w (stderr=%s)",
			imageRef, err, strings.TrimSpace(string(stderr)))
	}
	if !strings.Contains(string(stdout), "Verified OK") &&
		!strings.Contains(string(stdout), "verified") {
		return fmt.Errorf("cosign verify %s: unexpected stdout %q", imageRef, strings.TrimSpace(string(stdout)))
	}
	return nil
}
