package scheduler_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

// TestGenerateBearerToken_FormatAndHash asserts that GenerateBearerToken
// returns a base64url-encoded 32-byte raw token (43 chars, no padding,
// url-safe alphabet) paired with its sha256 hex digest (64 chars).
//
// Why this matters: only the hash is persisted in daemon.db; the raw
// token is surfaced once to the operator. A drift in either length or
// alphabet would either truncate the random space (security) or break
// the URL-safety guarantee (transport).
func TestGenerateBearerToken_FormatAndHash(t *testing.T) {
	raw, hash, err := scheduler.GenerateBearerToken()
	if err != nil {
		t.Fatalf("GenerateBearerToken: %v", err)
	}

	if len(raw) != 43 {
		t.Errorf("raw token len = %d, want 43", len(raw))
	}

	if strings.ContainsAny(raw, "+/=") {
		t.Errorf("raw token contains non-url-safe chars: %q", raw)
	}

	if len(hash) != 64 {
		t.Errorf("hash len = %d, want 64 (sha256 hex)", len(hash))
	}
}

func TestGenerateBearerToken_Uniqueness(t *testing.T) {
	raw1, hash1, err := scheduler.GenerateBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	raw2, hash2, err := scheduler.GenerateBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if raw1 == raw2 {
		t.Error("two consecutive raw tokens collided; rand source not random?")
	}
	if hash1 == hash2 {
		t.Error("two consecutive hashes collided")
	}
}

func TestVerifyHttpTrigger_Match(t *testing.T) {
	raw, hash, err := scheduler.GenerateBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	s := &scheduler.Schedule{
		TriggerType:   scheduler.TriggerHTTP,
		TriggerConfig: scheduler.TriggerConfig{BearerTokenHash: hash},
	}
	if err := scheduler.VerifyHttpTrigger(s, raw); err != nil {
		t.Errorf("VerifyHttpTrigger(matching token) = %v, want nil", err)
	}
}

func TestVerifyHttpTrigger_Mismatch(t *testing.T) {
	_, hash, err := scheduler.GenerateBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	s := &scheduler.Schedule{
		TriggerType:   scheduler.TriggerHTTP,
		TriggerConfig: scheduler.TriggerConfig{BearerTokenHash: hash},
	}
	err = scheduler.VerifyHttpTrigger(s, "wrong-token-here-which-doesnt-match-original")
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("VerifyHttpTrigger(mismatch) = %v, want ErrInvalidSchedule", err)
	}
}

func TestVerifyHttpTrigger_LengthMismatch(t *testing.T) {
	_, hash, err := scheduler.GenerateBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	s := &scheduler.Schedule{
		TriggerType:   scheduler.TriggerHTTP,
		TriggerConfig: scheduler.TriggerConfig{BearerTokenHash: hash},
	}
	err = scheduler.VerifyHttpTrigger(s, "short")
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("VerifyHttpTrigger(short token) = %v, want ErrInvalidSchedule", err)
	}
}

func TestVerifyHttpTrigger_NonHttpTriggerRejected(t *testing.T) {
	s := &scheduler.Schedule{
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "* * * * *"},
	}
	err := scheduler.VerifyHttpTrigger(s, "any")
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("VerifyHttpTrigger(non-HTTP) = %v, want ErrInvalidSchedule", err)
	}
}

func TestVerifyHttpTrigger_NilSchedule(t *testing.T) {
	err := scheduler.VerifyHttpTrigger(nil, "any")
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("VerifyHttpTrigger(nil) = %v, want ErrInvalidSchedule", err)
	}
}

func TestVerifyHttpTrigger_EmptyHash(t *testing.T) {
	s := &scheduler.Schedule{
		TriggerType:   scheduler.TriggerHTTP,
		TriggerConfig: scheduler.TriggerConfig{BearerTokenHash: ""},
	}
	err := scheduler.VerifyHttpTrigger(s, "any")
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("VerifyHttpTrigger(empty hash) = %v, want ErrInvalidSchedule", err)
	}
}

func TestVerifyHttpTrigger_MalformedHashHex(t *testing.T) {
	s := &scheduler.Schedule{
		TriggerType:   scheduler.TriggerHTTP,
		TriggerConfig: scheduler.TriggerConfig{BearerTokenHash: "not-hex-zzzz"},
	}
	err := scheduler.VerifyHttpTrigger(s, "any")
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("VerifyHttpTrigger(malformed hash) = %v, want ErrInvalidSchedule", err)
	}
}
