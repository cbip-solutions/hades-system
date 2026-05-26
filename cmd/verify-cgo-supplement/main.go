// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/sbom/cgosupplement"
)

func main() {
	root := newRootCmd()
	if err := root.ExecuteContext(context.Background()); err != nil {
		slog.Error("verify-cgo-supplement failed", "err", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var (
		rootDir            string
		supplementPath     string
		goModPath          string
		vendorDir          string
		mergeMode          bool
		sbomTarget         string
		allowMissingVendor bool
	)
	cmd := &cobra.Command{
		Use:   "verify-cgo-supplement",
		Short: "Verify CGO supplement against go.mod + vendor/ (Phase E-6; pattern Plan 10 verify-vendor-tree)",
		Long: `Verifies configs/cgo-supplement.cdx.json entries match:
  - go.mod actual require versions (for entries with hades-system:go-binding)
  - vendor/ directory state (for entries with hades-system:vendor-path)
  - LICENSE file presence per vendored entry (Apache §4(d) attribution)

Exit codes:
  0 — supplement valid (no drift)
  1 — drift detected; stderr lists each drift entry

Flags (full list via --help):
  --supplement <path>          Path to cgo-supplement.cdx.json
  --go-mod <path>              Path to go.mod for require-version cross-check
  --vendor <path>              Path to vendor/ dir for vendored-entry check
  --root <dir>                 Root dir for resolving relative paths above
  --allow-missing-vendor       Tolerate missing vendor/ dir (transitional;
                               pre-Plan-10-merge mode for environments where
                               vendoring has not yet completed)
  --sbom <path>                Target SBOM for --merge mode
  --merge                      Merge supplement into --sbom in-place (atomic
                               tempfile + rename); used by GoReleaser
                               post-hook per Phase E-1

--merge mode: writes the supplement components into a target CycloneDX SBOM
in-place (atomic tempfile + rename). Used by GoReleaser post-hook (Phase E-1).
`,
		RunE: func(cmd *cobra.Command, args []string) error {

			if rootDir != "" {
				if !filepath.IsAbs(supplementPath) {
					supplementPath = filepath.Join(rootDir, supplementPath)
				}
				if !filepath.IsAbs(goModPath) {
					goModPath = filepath.Join(rootDir, goModPath)
				}
				if !filepath.IsAbs(vendorDir) {
					vendorDir = filepath.Join(rootDir, vendorDir)
				}
			}

			s, err := cgosupplement.Load(supplementPath)
			if err != nil {
				return fmt.Errorf("load supplement: %w", err)
			}

			if mergeMode {
				if sbomTarget == "" {
					return errors.New("--merge requires --sbom <path>")
				}
				if err := s.MergeIntoSBOM(sbomTarget); err != nil {
					return fmt.Errorf("merge into %s: %w", sbomTarget, err)
				}
				fmt.Printf("merged %d supplement entries into %s\n", len(s.Entries), sbomTarget)
				return nil
			}

			var verifyErrs []error
			if err := s.ValidateAgainstGoMod(goModPath); err != nil {
				verifyErrs = append(verifyErrs, fmt.Errorf("go.mod cross-check failed:\n%w", err))
			}
			if vendorDir != "" {
				if _, statErr := os.Stat(vendorDir); statErr != nil && os.IsNotExist(statErr) && allowMissingVendor {
					slog.Warn("vendor dir missing (--allow-missing-vendor mode)", "path", vendorDir)
				} else if err := s.ValidateAgainstVendorDir(vendorDir); err != nil {
					verifyErrs = append(verifyErrs, fmt.Errorf("vendor/ cross-check failed:\n%w", err))
				}
			}
			if len(verifyErrs) > 0 {
				return errors.Join(verifyErrs...)
			}
			fmt.Printf("supplement %s validated against go.mod (%s) + vendor/ (%s) — %d entries; no drift\n",
				supplementPath, goModPath, vendorDir, len(s.Entries))
			return nil
		},
	}
	cmd.Flags().StringVar(&rootDir, "root", ".", "Repo root directory (resolves other paths relative)")
	cmd.Flags().StringVar(&supplementPath, "supplement", "configs/cgo-supplement.cdx.json", "CGO supplement CycloneDX 1.6 JSON file")
	cmd.Flags().StringVar(&goModPath, "go-mod", "go.mod", "go.mod file for require-version cross-check")
	cmd.Flags().StringVar(&vendorDir, "vendor-dir", "vendor", "vendor/ directory for vendored-path presence check")
	cmd.Flags().BoolVar(&mergeMode, "merge", false, "Merge supplement into target SBOM (requires --sbom)")
	cmd.Flags().StringVar(&sbomTarget, "sbom", "", "Target SBOM .cdx.json file (only valid with --merge)")
	cmd.Flags().BoolVar(&allowMissingVendor, "allow-missing-vendor", false, "Tolerate missing vendor/ dir (pre-Plan-10-merge transitional mode)")
	return cmd
}
