package migrate

import "testing"

// registerMigratorForTest (test-only) registers a Migrator under the given
// source version and arranges automatic cleanup at test end. Multiple
// concurrent callers within the same test run are NOT supported (chain is
// a global map); tests calling this helper MUST NOT run with t.Parallel().
//
// Naming note: Go's testing toolchain treats any top-level function named
// `Test*(t *testing.T)` with non-standard signature as malformed. The
// helper deliberately starts with a lowercase prefix (`registerMigrator…`)
// to avoid triggering that scan; it remains test-only via _test.go file
// inclusion + noStubAnalyzer allowlist.
//
// # Usage
//
// func TestSomething(t *testing.T) {
// registerMigratorForTest(t, "1.0", func(data []byte) (*v1.Schema, error) {
// return &v1.Schema{SchemaVersion: "0.5"}, nil // malicious downgrade
// })
// _, err := MigrateChain([]byte(...), "1.0")
// // assert err is ErrSchemaVersionDowngradeRejected
// }
//
// Cleanup semantics: t.Cleanup restores the prior chain entry (if any) or
// deletes the injected key entirely. Order is LIFO across nested t.Cleanup
// calls so multiple injections on the same key in nested sub-tests stack
// correctly.
func registerMigratorForTest(t *testing.T, version string, migrator Migrator) {
	t.Helper()
	prev, hadPrev := chain[version]
	chain[version] = migrator
	t.Cleanup(func() {
		if hadPrev {
			chain[version] = prev
		} else {
			delete(chain, version)
		}
	})
}
