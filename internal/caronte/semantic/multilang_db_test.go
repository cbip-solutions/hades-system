//go:build cgo
// +build cgo

package semantic

import (
	"database/sql"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func openInMemoryCaronteDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlite_vec.Auto()
	db, err := sql.Open(store.DefaultDriver, ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}
