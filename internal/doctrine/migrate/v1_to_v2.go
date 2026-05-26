// SPDX-License-Identifier: MIT
package migrate

import (
	"fmt"

	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func MigrateV1ToV2(data []byte) (*v1.Schema, error) {
	return nil, fmt.Errorf(
		"migrate.MigrateV1ToV2: placeholder; V2 schema not yet defined (Plan 8 Phase C-4): %w",
		doctrineerrors.ErrMigrationNotImplemented,
	)
}
