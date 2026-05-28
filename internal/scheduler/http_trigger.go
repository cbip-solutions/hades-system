// SPDX-License-Identifier: MIT
package scheduler

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const bearerTokenRandBytes = 32

var randRead = rand.Read

// GenerateBearerToken returns a freshly-generated raw bearer token and
// its sha256 hex digest.
//
// The raw token is 32 bytes from crypto/rand encoded with
// base64.RawURLEncoding (alphabet [A-Za-z0-9_-], no padding) which
// produces exactly 43 chars. The hash is the sha256 of the raw token
// (NOT the underlying bytes) rendered as 64 lowercase hex chars.
//
// Security invariant: only the hash is persisted in
// daemon.db.bearer_token_hash. The raw token is surfaced to the
// operator EXACTLY ONCE in the `hades schedule routine create` CLI
// response and never again — never logged, never persisted, never
// echoed to disk by the daemon. Loss of the raw token requires a
// rotation (delete + recreate the routine) per design contract
//
// The separate raw -> hash sequence (rather than hashing the random
// bytes directly) keeps the verification path symmetric: the operator
// sends back the same encoded string they received, and the verifier
// hashes that string to compare.
func GenerateBearerToken() (raw, hashHex string, err error) {
	var buf [bearerTokenRandBytes]byte
	if _, err := randRead(buf[:]); err != nil {
		return "", "", fmt.Errorf("scheduler: bearer token rand: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(buf[:])
	sum := sha256.Sum256([]byte(raw))
	hashHex = hex.EncodeToString(sum[:])
	return raw, hashHex, nil
}

func VerifyHttpTrigger(s *Schedule, providedRaw string) error {
	if s == nil {
		return fmt.Errorf("%w: nil Schedule", ErrInvalidSchedule)
	}
	if s.TriggerType != TriggerHTTP {
		return fmt.Errorf("%w: trigger type %v != HTTP", ErrInvalidSchedule, s.TriggerType)
	}
	if s.TriggerConfig.BearerTokenHash == "" {
		return fmt.Errorf("%w: empty BearerTokenHash", ErrInvalidSchedule)
	}
	stored, err := hex.DecodeString(s.TriggerConfig.BearerTokenHash)
	if err != nil {
		return fmt.Errorf("%w: invalid BearerTokenHash hex: %v", ErrInvalidSchedule, err)
	}
	provided := sha256.Sum256([]byte(providedRaw))

	if subtle.ConstantTimeCompare(stored, provided[:]) != 1 {
		return fmt.Errorf("%w: bearer token mismatch", ErrInvalidSchedule)
	}
	return nil
}
