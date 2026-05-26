// SPDX-License-Identifier: MIT
package aggregator

import (
	"context"
	"database/sql"
)

func goodQuery(ctx context.Context, db *sql.DB) (*sql.Rows, error) {

	return db.QueryContext(ctx, "SELECT * FROM knowledge_pin_index WHERE rank > ?", 0)
}
