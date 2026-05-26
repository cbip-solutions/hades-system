// SPDX-License-Identifier: MIT
// Package fix — schema_version_fix.go ships the Fix impl for the
// store.schema-version check.
//
// Non-destructive transactional: `zen migrate up` is reversible (down
// migrations exist) and idempotent (no-op when already current); safe
// under --auto-safe.
package fix

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/cbip-solutions/hades-system/internal/doctor/check"
)

type SchemaVersionFix struct{}

func (s *SchemaVersionFix) Name() string { return "store.schema-version" }

func (s *SchemaVersionFix) IsDestructive() bool { return false }

func (s *SchemaVersionFix) Apply(ctx context.Context, mode check.FixMode) error {
	if mode == check.FixModeReadOnly {
		return errors.New("fix: read-only mode; run `zen migrate up` to apply migrations (transactional + reversible)")
	}
	if _, err := exec.LookPath("zen"); err != nil {
		return fmt.Errorf("fix: `zen` binary not on PATH: %w", err)
	}
	cmd := exec.CommandContext(ctx, "zen", "migrate", "up")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fix: zen migrate up failed: %w; output:\n%s", err, string(out))
	}
	return nil
}

var (
	_ Destructive = (*SchemaVersionFix)(nil)
	_ Applier     = (*SchemaVersionFix)(nil)
)
