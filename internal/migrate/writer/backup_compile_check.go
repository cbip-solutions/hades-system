// SPDX-License-Identifier: MIT
package writer

import "github.com/cbip-solutions/hades-system/internal/migrate/mapping"

type applyFunc func(*mapping.Plan) error

var _ applyFunc = (*Writer)(nil).Apply
