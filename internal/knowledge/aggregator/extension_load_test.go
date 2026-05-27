// go:build cgo
//go:build cgo
// +build cgo

// Package aggregator owns aggregator.db (knowledge_pin_index + FTS5 +
// sqlite-vec + wikilinks) spec §"Knowledge aggregator".
//
// invariant NOTE: this package and its tests do NOT make web calls;
// sqlite-vec is a local C extension. The compliance test in
// tests/compliance/inv_zen_129_aggregator_no_web_test.go enforces the
// package's web-egress prohibition by static import inspection.
package aggregator

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestLoadVecExtensionRunsTinyQuery(t *testing.T) {
	t.Helper()
	if err := LoadVecExtension(); err != nil {
		t.Fatalf("LoadVecExtension: %v", err)
	}

	t.Cleanup(func() {
		// We do NOT call sqlite_vec.Cancel() because other tests in the
		// package may rely on the auto-extension state.
	})

	db, err := sql.Open(DefaultDriver, ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	var distance float64
	row := db.QueryRowContext(ctx, `
		SELECT vec_distance_cosine('[1,0,0,0]', '[0.6,0.8,0,0]')
	`)
	if err := row.Scan(&distance); err != nil {
		t.Fatalf("vec_distance_cosine query: %v", err)
	}
	const want = 0.4
	const tolerance = 1e-6
	if math.Abs(distance-want) > tolerance {
		t.Errorf("vec_distance_cosine = %f; want %f (±%e)", distance, want, tolerance)
	}
}

func TestLoadVecExtensionIdempotent(t *testing.T) {
	if err := LoadVecExtension(); err != nil {
		t.Fatalf("LoadVecExtension first call: %v", err)
	}
	if err := LoadVecExtension(); err != nil {
		t.Fatalf("LoadVecExtension second call: %v", err)
	}

	db, err := sql.Open(DefaultDriver, ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	var d float64
	if err := db.QueryRowContext(context.Background(),
		`SELECT vec_distance_cosine('[1,0]', '[0,1]')`,
	).Scan(&d); err != nil {
		t.Fatalf("post-idempotent query: %v", err)
	}
	if math.Abs(d-1.0) > 1e-6 {
		t.Errorf("orthogonal cosine distance = %f; want 1.0", d)
	}
}

func TestVecVersionFunctionAvailable(t *testing.T) {
	if err := LoadVecExtension(); err != nil {
		t.Fatalf("LoadVecExtension: %v", err)
	}
	db, err := sql.Open(DefaultDriver, ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	var version string
	if err := db.QueryRowContext(context.Background(),
		`SELECT vec_version()`,
	).Scan(&version); err != nil {
		t.Fatalf("vec_version query: %v", err)
	}
	if !strings.HasPrefix(version, "v0.1.6") {
		t.Errorf("vec_version = %q; want prefix \"v0.1.6\" (sqlite-vec dep pinned at v0.1.6)", version)
	}
}

func TestDefaultDriverConstant(t *testing.T) {
	if DefaultDriver != "sqlite3" {
		t.Errorf("DefaultDriver = %q; want \"sqlite3\"", DefaultDriver)
	}
}

func TestErrCGODisabledIsExported(_ *testing.T) {
	var err error = ErrCGODisabled
	_ = errors.Is(err, ErrCGODisabled)
}
