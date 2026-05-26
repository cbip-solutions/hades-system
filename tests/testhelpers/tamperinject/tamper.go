// SPDX-License-Identifier: MIT
//
// Provides four corruption operations:
//   - ModifyRecordHashRaw — bypass SQLite REFUSE triggers via raw connection.
//   - CorruptTesseraTile — flip a byte in a tile file.
//   - SwapWitnessSig — replace daemon witness signature in checkpoint JSON.
//   - CorruptTilePartial — truncate a tile file (partial-upload simulation).
//
// Used by tests/adversarial/audit_chain_adversarial_test.go (K-8) to
// drive the contract: even after corruption bypasses primary defense,
// secondary defenses (verify-chain walker + Merkle proof + co-sig
// verify) detect the attack. Defense in depth.
//
// Sub-package isolation: this file imports `mattn/go-sqlite3` directly.
// `tests/testhelpers/store.go` already imports `ncruces/go-sqlite3` (WASM
// driver) — both register the `sqlite3` driver name, causing
// `sql: Register called twice for driver sqlite3` panic at test-binary
// init if both live in the same package. Sub-package keeps mattn out of
// any test binary that doesn't explicitly import this package. Same
// boundary pattern as `tests/testhelpers/researchmcpmock/` and
// `tests/testhelpers/tesseramock/`.
package tamperinject

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

var auditEventsRawTriggers = []string{
	"audit_events_raw_no_update_immutable",
	"audit_events_raw_no_update_chain_hashes",
	"audit_events_raw_no_update_partition",
	"audit_events_raw_no_update_tessera_leaf",
	"audit_events_raw_no_delete",
}

func ModifyRecordHashRaw(dbPath string, eventID string, newHash []byte) error {
	db, _ := sql.Open("sqlite3", dbPath+"?_foreign_keys=off")
	defer db.Close()

	for _, name := range auditEventsRawTriggers {
		if _, err := db.Exec("DROP TRIGGER IF EXISTS " + name); err != nil {
			return fmt.Errorf("drop trigger %s: %w", name, err)
		}
	}

	if _, err := db.Exec("UPDATE audit_events_raw SET record_hash = ? WHERE id = ?", newHash, eventID); err != nil {
		return fmt.Errorf("update record_hash: %w", err)
	}
	return nil
}

func CorruptTesseraTile(path string, offset int64) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, _ := f.Stat()
	if offset < 0 || offset >= stat.Size() {
		return errors.New("offset out of range")
	}

	buf := make([]byte, 1)
	_, _ = f.ReadAt(buf, offset)
	buf[0] ^= 0xff
	_, _ = f.WriteAt(buf, offset)
	return nil
}

type checkpointJSON struct {
	Size     int    `json:"size"`
	RootHash string `json:"root_hash"`
	Sig      string `json:"sig"`
	SigB64   string `json:"sig_b64,omitempty"`
}

func SwapWitnessSig(path string, fakeSig []byte) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cp checkpointJSON
	if err := json.Unmarshal(body, &cp); err != nil {
		return err
	}
	cp.Sig = "TAMPERED:" + string(fakeSig)
	cp.SigB64 = "TAMPERED"
	out, _ := json.Marshal(cp)
	return os.WriteFile(path, out, 0644)
}

func CorruptTilePartial(path string, keepBytes int64) error {
	return os.Truncate(path, keepBytes)
}
