// SPDX-License-Identifier: MIT
// Package auth — bearer.go.
//
// Daemon-bearer + per-routine bearer validators using
// crypto/subtle.ConstantTimeCompare for timing-safe comparison
// (invariant). Per-routine mismatch emits a
// ScheduleHttpTriggerAuthFailed audit event for the release audit
// pipeline to escalate to action-needed (5+ in 1h per spec §4.3).
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var ErrBearerMismatch = errors.New("auth: bearer token mismatch")

var ErrScheduleNotFound = errors.New("auth: schedule not found")

var ErrPeerCredUnsupported = errors.New("auth: peer-cred unsupported on this GOOS")

type AuditEmitter interface {
	Emit(ctx context.Context, event map[string]any) error
}

type HTTPTokenStore interface {
	Get(ctx context.Context, scheduleID string) (sha256HexHash string, err error)
}

type DaemonBearer struct {
	expectedHash [32]byte
}

func NewDaemonBearer(token string) *DaemonBearer {
	return &DaemonBearer{
		expectedHash: sha256.Sum256([]byte(token)),
	}
}

// Validate compares the presented cleartext bearer against the stored
// hash using subtle.ConstantTimeCompare. Returns (true, nil) on match,
// (false, ErrBearerMismatch) on mismatch.
//
// Empty presented short-circuits to mismatch (do not hash empty string
// and accept it as a match against an empty configured token — the
// daemon-bearer token is always 64 hex chars; an empty token is a
// configuration bug, but defensively reject anyway).
func (b *DaemonBearer) Validate(_ context.Context, presented string) (bool, error) {
	if presented == "" {
		return false, ErrBearerMismatch
	}
	got := sha256.Sum256([]byte(presented))
	if subtle.ConstantTimeCompare(b.expectedHash[:], got[:]) == 1 {
		return true, nil
	}
	return false, ErrBearerMismatch
}

type PerRoutineBearer struct {
	store   HTTPTokenStore
	emitter AuditEmitter
}

func NewPerRoutineBearer(store HTTPTokenStore, emitter AuditEmitter) *PerRoutineBearer {
	return &PerRoutineBearer{store: store, emitter: emitter}
}

func (b *PerRoutineBearer) Validate(ctx context.Context, scheduleID, presented, remoteAddr string) (bool, error) {
	storedHash, err := b.store.Get(ctx, scheduleID)
	if err != nil {
		if errors.Is(err, ErrScheduleNotFound) {
			return false, ErrScheduleNotFound
		}
		return false, fmt.Errorf("auth: token store: %w", err)
	}
	presentedHash := sha256.Sum256([]byte(presented))
	storedRaw, decodeErr := hex.DecodeString(storedHash)
	if decodeErr != nil || len(storedRaw) != 32 {

		_ = b.emitMismatch(ctx, scheduleID, presented, remoteAddr, "stored_hash_malformed")
		return false, ErrBearerMismatch
	}
	if subtle.ConstantTimeCompare(storedRaw, presentedHash[:]) == 1 {
		return true, nil
	}
	_ = b.emitMismatch(ctx, scheduleID, presented, remoteAddr, "")
	return false, ErrBearerMismatch
}

func (b *PerRoutineBearer) emitMismatch(ctx context.Context, scheduleID, presented, remoteAddr, reason string) error {
	prefixLen := 10
	if len(presented) < prefixLen {
		prefixLen = len(presented)
	}
	return b.emitter.Emit(ctx, map[string]any{
		"type":                   "ScheduleHttpTriggerAuthFailed",
		"schedule_id":            scheduleID,
		"remote_addr":            remoteAddr,
		"attempted_token_prefix": presented[:prefixLen],
		"reason":                 reason,
	})
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}

	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func RequireDaemonBearer(b *DaemonBearer, emitter AuditEmitter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			presented := extractBearer(r)
			ok, _ := b.Validate(r.Context(), presented)
			if !ok {
				_ = emitter.Emit(r.Context(), map[string]any{
					"type":        "DaemonBearerAuthFailed",
					"remote_addr": r.RemoteAddr,
					"path":        r.URL.Path,
				})
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RequirePerRoutineBearer(b *PerRoutineBearer, scheduleIDFromPath func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheduleID := scheduleIDFromPath(r)
			if scheduleID == "" {
				http.Error(w, "missing schedule id", http.StatusBadRequest)
				return
			}
			presented := extractBearer(r)
			ok, _ := b.Validate(r.Context(), scheduleID, presented, r.RemoteAddr)
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
