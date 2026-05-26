// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/release/verifier"
)

func main() {
	root := newRootCmd()
	if err := root.ExecuteContext(context.Background()); err != nil {
		slog.Error("verify-release-artifacts failed", "err", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var (
		dir         string
		owner       string
		repo        string
		mode        string
		checkSBOM   bool
		checkAttest bool
		checkCosign bool
		checkCGO    bool
		checkOCI    bool
		ociImageRef string
	)
	cmd := &cobra.Command{
		Use:   "verify-release-artifacts",
		Short: "Verify dist/ release artifacts (checksums + SBOMs + attestations + cosign + OCI)",
		Long: `Phase D-10 + Phase E-5 release self-check.

Verifies dist/ artifacts produced by goreleaser release:
- checksums (Phase D-10)
- multi-arch coverage 3-platform (Phase D-10)
- ad-hoc codesign macOS (Phase D-10)
- SBOM dual-emit CycloneDX + SPDX presence + valid JSON (Phase E-5)
- SLSA L2 attestations via gh attestation verify (Phase E-5)
- cosign-keyless signatures via cosign verify-blob (Phase E-5)
- CGO supplement entries merged (Phase E-5)
- OCI image signature via cosign verify ghcr.io/... (Phase E-5 delegates Phase D-9)

Modes:
  full (default)  — all checks including gh + cosign network calls
  fast            — skip gh + cosign network calls (offline-friendly)
`,
		RunE: func(cmd *cobra.Command, args []string) error {

			var vMode verifier.VerifyMode
			switch mode {
			case "full":
				vMode = verifier.ModeFull
			case "fast":
				vMode = verifier.ModeFast
			default:
				return fmt.Errorf("invalid --mode %q; want 'full' or 'fast'", mode)
			}
			v := verifier.New(nil)
			v.Dir = dir
			v.Owner = owner
			v.Repo = repo
			v.Mode = vMode

			artifacts, err := v.VerifyAllArtifacts(dir)
			if err != nil {
				return fmt.Errorf("VerifyAllArtifacts: %w", err)
			}

			if err := v.VerifyMultiArch(dir); err != nil {
				return fmt.Errorf("VerifyMultiArch: %w", err)
			}

			for _, art := range artifacts {
				if art.Type == "binary" && vMode == verifier.ModeFull {

					if err := v.VerifySignatures(art); err != nil {
						return fmt.Errorf("VerifySignatures %s: %w", art.Path, err)
					}
				}

				if checkSBOM && (art.Type == "binary" || art.Type == "deb" || art.Type == "rpm") {
					if err := v.VerifySBOMPresent(art); err != nil {
						return fmt.Errorf("VerifySBOMPresent %s: %w", art.Path, err)
					}
				}
				if checkCGO && art.Type == "binary" {
					if err := v.VerifyCGOSupplementEntries(art); err != nil {
						return fmt.Errorf("VerifyCGOSupplementEntries %s: %w", art.Path, err)
					}
				}
				if vMode == verifier.ModeFull {
					if checkAttest && (art.Type == "binary" || art.Type == "deb" || art.Type == "rpm" ||
						art.Type == "sbom-cyclonedx" || art.Type == "sbom-spdx") {
						if err := v.VerifyAttestation(art); err != nil {
							return fmt.Errorf("VerifyAttestation %s: %w", art.Path, err)
						}
					}
					if checkCosign && (art.Type == "binary" ||
						art.Type == "sbom-cyclonedx" || art.Type == "sbom-spdx" ||
						art.Type == "checksum") {
						if err := v.VerifyCosignSignature(art); err != nil {
							return fmt.Errorf("VerifyCosignSignature %s: %w", art.Path, err)
						}
					}
				}
			}
			if checkOCI && ociImageRef != "" && vMode == verifier.ModeFull {
				if err := v.VerifyOCIImageSignature(ociImageRef); err != nil {
					return fmt.Errorf("VerifyOCIImageSignature %s: %w", ociImageRef, err)
				}
			}
			fmt.Printf("verified %d artifacts across %v\n", len(artifacts), v.Platforms)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "dist", "GoReleaser output directory")
	cmd.Flags().StringVar(&owner, "owner", "hades-system", "GitHub owner for attestation+cosign verification (public org per decisión 11)")
	cmd.Flags().StringVar(&repo, "repo", "hades-system", "GitHub repo for attestation+cosign verification (public repo per decisión 11)")
	cmd.Flags().StringVar(&mode, "mode", "full", "Verification mode: full (default; network calls) | fast (offline; skip gh+cosign)")
	cmd.Flags().BoolVar(&checkSBOM, "check-sbom", true, "Verify SBOM dual-emit presence (Phase E-5)")
	cmd.Flags().BoolVar(&checkAttest, "check-attestation", true, "Verify SLSA L2 attestations via gh (Phase E-5)")
	cmd.Flags().BoolVar(&checkCosign, "check-cosign", true, "Verify cosign-keyless signatures via cosign verify-blob (Phase E-5)")
	cmd.Flags().BoolVar(&checkCGO, "check-cgo-supplement", true, "Verify CGO supplement entries merged into per-artifact CycloneDX (Phase E-5)")
	cmd.Flags().BoolVar(&checkOCI, "check-oci", false, "Verify OCI image signature (Phase E-5 delegates Phase D-9); enable + provide --oci-image-ref")
	cmd.Flags().StringVar(&ociImageRef, "oci-image-ref", "", "OCI image ref to verify (e.g., ghcr.io/cbip-solutions/hades-system@sha256:abcdef...)")
	return cmd
}
