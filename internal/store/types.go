// SPDX-License-Identifier: MIT
package store

import (
	"database/sql"
	"time"
)

type sqlNullString struct{ sql.NullString }

func (s sqlNullString) Get() string {
	if !s.Valid {
		return ""
	}
	return s.String
}

type sqlNullInt64 struct{ sql.NullInt64 }

func (n sqlNullInt64) Get() int64 {
	if !n.Valid {
		return 0
	}
	return n.Int64
}

type sqlNullFloat64 struct{ sql.NullFloat64 }

func (n sqlNullFloat64) Get() float64 {
	if !n.Valid {
		return 0
	}
	return n.Float64
}

func nowUnix() int64 {
	return time.Now().UTC().Unix()
}

func timeFromUnix(ts int64) time.Time {
	return time.Unix(ts, 0).UTC()
}
