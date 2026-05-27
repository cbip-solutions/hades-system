// SPDX-License-Identifier: MIT
// Package ecosystem — verifier.go
//
// 3-stage symbol existence verifier per spec §2.7 Q7=A Layer 2 (release
// task D-5).
//
// Stages (sequential; first hit wins):
//
// A — SymbolIndex.Contains: O(1) in-memory hash-set lookup (sub-μs).
// Populated by at daemon startup + incremental Register on
// ingest. ~99% of canonical-symbol queries hit here.
//
// B — 24h LRU cache: in-process LRU keyed by (ecosystem, version,
// symbol_path). Records live-cmd outputs for symbols that miss
// the index (typically third-party packages not yet indexed).
// TTL aligns with Q9=A 24h revalidation cadence; TTL exceeded →
// eviction-on-read, fall to C. Negative results are also cached
// so we don't re-shell-out on every hallucinated symbol.
//
// C — live cmd: per-ecosystem shellout via LiveCmdRunner abstraction.
// Production runner (execLiveCmdRunner in verifier_live_cmd.go)
// calls `go doc`, `python -c`, `npm view`, `cargo search`. Result
// written into the LRU before returning, so the next call for
// the same symbol within TTL hits stage B.
//
// Verify-at-answer-time is the deterministic anchor against package-
// hallucination. USENIX Sec 2025 (arXiv 2406.10279) finds 5.2%
// commercial / 21.7% open-source LLM-recommended packages don't exist;
// verify catches them. Order matters: symbol_index is the hot path
// (sub-μs); LRU absorbs repeated cold-misses; live cmd is the cold-start
// path with strict 24h TTL.
//
// invariant partial: Verify(symbols) hot path on stage-A is O(1) per
// ref; goroutine-safe; bench in covers the symbol_index leaf.
//
// invariant (D-13): doctrine-knob can downgrade verify (default
// doctrine skips stage C on cold misses) — see VerifierConfig.SkipStageC.
//
// Concurrency Verifier.Verify is goroutine-safe. A single mutex
// serializes LRU access AND live-cmd invocations. Live-cmd serialization
// is intentional — it avoids per-package-manager concurrency bugs and
// rate limits. Dispatcher.Query has at most one verifier call per query
// (after fusion), so the serialization point is in-budget.
package ecosystem

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"
)

type VerifySource string

const (
	VerifySourceSymbolIndex VerifySource = "symbol_index"

	VerifySourceLiveCache VerifySource = "live_cache"

	VerifySourceLiveCmd VerifySource = "live_cmd"

	VerifySourceSkipped VerifySource = "skipped"
)

type SymbolIndexLookup interface {
	Contains(eco Ecosystem, symbolPath, version string) (string, bool)
}

type LiveCmdRunner interface {
	Run(ctx context.Context, eco Ecosystem, ref SymbolRef) (liveCmdResult, error)
}

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type liveCmdResult struct {
	Exists    bool
	Signature string
	Truncated bool
	Err       error
}

type VerifierConfig struct {
	SymbolIndex SymbolIndexLookup

	LiveCmdRunner LiveCmdRunner

	LRUSize int

	LRUTTL time.Duration

	Clock Clock

	SkipStageC bool
}

const (
	defaultVerifierLRUSize = 1024
	defaultVerifierLRUTTL  = 24 * time.Hour
)

type Verifier struct {
	cfg   VerifierConfig
	mu    sync.Mutex
	lru   *list.List
	index map[string]*list.Element
}

type lruEntry struct {
	key      string
	value    SymbolVerification
	storedAt time.Time
}

// symbolPathRegex restricts SymbolPath to ASCII-identifier segments
// separated by '.', '/', ':' or '::'. The first character of every
// segment must be a letter or underscore — this structurally rejects
// leading '-' (argv-flag injection) AND any byte outside the allowed
// set, which covers shell metacharacters, quotes, semicolons, newlines,
// NUL bytes, backticks, and arbitrary Unicode payloads.
//
// Separator alternatives:
//
// `.` — Go, Python, TypeScript member access (e.g., `pkg.Sym`).
// `/` — Go import paths (e.g., `crypto/sha256`).
// `:` — reserved separator (some symbol-table formats use it).
// `::` — Rust path separator (e.g., `tokio::spawn`, `std::sync::Mutex`).
//
// Allowed shapes (D-5 fix-cycle 2):
//
// go "crypto/sha256.Sum256" "encoding/json.Marshal"
// go modules "github.com/zen-swarm/zen" "k8s.io/api/core/v1.Pod"
// python "functools.partial" "asyncio"
// typescript "react.useState"
// rust "tokio::spawn" "std::async::spawn"
//
// Each segment starts with [A-Za-z_] and may contain [A-Za-z0-9_-]
// thereafter. Hyphens are allowed mid-segment (npm + go-module names
// commonly contain them, e.g., "zen-swarm") but never leading, so
// argv-flag injection ("-x", "--registry=evil") is still rejected
// structurally.
//
// Scoped npm packages ("@scope/pkg") are deliberately rejected for now
// — '@' is not in the allowed set. The validator can be relaxed as a
// separate amendment with a tightened npm-name grammar; for the current
// security boundary the conservative shape is correct.
//
// Justification this is the security anchor against attacker-controlled
// SymbolPath flowing into `python3 -c`, `npm view`, `cargo search`. The
// Python branch uses argv-passing (eliminating script interpolation)
// and the npm/cargo branches use '--' end-of-options markers; the
// regex is the cascade-entry gate so even if either layer is bypassed
// or refactored in a future plan, hostile input never reaches the
// subprocess.
//
// USENIX Sec 2025 (arXiv 2406.10279) observes 5-22% LLM-recommended
// packages don't exist; an attacker who hallucinates a package name
// in the prompt is precisely the input shape that flows through here.
var symbolPathRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*((\.|/|::?)[A-Za-z_][A-Za-z0-9_-]*)*$`)

const maxSymbolPathLen = 1024

// validateSymbolRef gates the cascade entry against hostile SymbolPath
// values. Empty path → typed error. Over-length path → typed error.
// Non-matching path → typed error.
//
// All three error modes return Source=skipped, Exists=false at the
// verifyOne layer; the cascade never proceeds to the live-cmd runner.
// This is the load-bearing security property — the runner can assume
// its input has already been validated.
func validateSymbolRef(ref SymbolRef) error {
	if ref.SymbolPath == "" {
		return errors.New("verifier: empty symbol path")
	}
	if len(ref.SymbolPath) > maxSymbolPathLen {
		return fmt.Errorf("verifier: symbol path exceeds %d bytes (got %d)", maxSymbolPathLen, len(ref.SymbolPath))
	}
	if !symbolPathRegex.MatchString(ref.SymbolPath) {
		return fmt.Errorf("verifier: invalid symbol path %q (must match identifier segments separated by '.', '/' or ':')", ref.SymbolPath)
	}
	return nil
}

func NewVerifier(cfg VerifierConfig) (*Verifier, error) {
	if cfg.SymbolIndex == nil {
		return nil, errors.New("verifier: SymbolIndex required")
	}
	if cfg.LRUSize <= 0 {
		cfg.LRUSize = defaultVerifierLRUSize
	}
	if cfg.LRUTTL <= 0 {
		cfg.LRUTTL = defaultVerifierLRUTTL
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	return &Verifier{
		cfg:   cfg,
		lru:   list.New(),
		index: make(map[string]*list.Element, cfg.LRUSize),
	}, nil
}

func (v *Verifier) Verify(ctx context.Context, refs []SymbolRef) (*VerifyResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]SymbolVerification, len(refs))
	allVerified := true

	for i, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sv, err := v.verifyOne(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("verify %s: %w", ref.SymbolPath, err)
		}
		out[i] = sv
		if !sv.Exists {
			allVerified = false
		}
	}
	return &VerifyResult{Verifications: out, AllVerified: allVerified}, nil
}

func (v *Verifier) verifyOne(ctx context.Context, ref SymbolRef) (SymbolVerification, error) {
	startA := v.cfg.Clock.Now()

	// Security gate — reject hostile / malformed SymbolPath before any
	// stage runs. The verifier is the LLM-hallucination anchor; if a
	// malicious upstream component (or a prompt-injected LLM output)
	// emits a SymbolPath outside the allowed identifier shape, we
	// must NOT consult the runner. Returning Exists=false with
	// Source=skipped is correct because a path that cannot be a real
	// symbol is, by definition, hallucinated.
	//
	// Note SymbolIndex.Contains is also skipped because passing a
	// non-identifier through to the index would risk false positives
	// on adversarially-crafted keys; the gate fails closed.
	if err := validateSymbolRef(ref); err != nil {
		_ = err
		return SymbolVerification{
			Symbol:  ref,
			Exists:  false,
			Source:  string(VerifySourceSkipped),
			Latency: v.cfg.Clock.Now().Sub(startA),
		}, nil
	}

	if sig, ok := v.cfg.SymbolIndex.Contains(ref.Ecosystem, ref.SymbolPath, ref.Version); ok {
		return SymbolVerification{
			Symbol:    ref,
			Exists:    true,
			Source:    string(VerifySourceSymbolIndex),
			Signature: sig,
			Latency:   v.cfg.Clock.Now().Sub(startA),
		}, nil
	}

	// Doctrine override — short-circuit before LRU + stage C.
	// Justification capa-firewall / default doctrines do not trust
	// shellouts on the answer-time hot path; LRU values come from
	// stage C, so without C the cache itself is conceptually disabled.
	if v.cfg.SkipStageC {
		return SymbolVerification{
			Symbol:  ref,
			Exists:  false,
			Source:  string(VerifySourceSkipped),
			Latency: v.cfg.Clock.Now().Sub(startA),
		}, nil
	}

	key := lruKey(ref)
	if sv, ok := v.lookupLRU(key, startA); ok {
		return sv, nil
	}

	if v.cfg.LiveCmdRunner == nil {
		return SymbolVerification{
			Symbol:  ref,
			Exists:  false,
			Source:  string(VerifySourceSkipped),
			Latency: v.cfg.Clock.Now().Sub(startA),
		}, nil
	}
	res, err := v.cfg.LiveCmdRunner.Run(ctx, ref.Ecosystem, ref)
	if err != nil {
		return SymbolVerification{}, err
	}
	sv := SymbolVerification{
		Symbol:    ref,
		Exists:    res.Exists,
		Source:    string(VerifySourceLiveCmd),
		Signature: res.Signature,
		Latency:   v.cfg.Clock.Now().Sub(startA),
	}
	v.cacheLRU(key, sv)
	return sv, nil
}

func (v *Verifier) lookupLRU(key string, startA time.Time) (SymbolVerification, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	elem, ok := v.index[key]
	if !ok {
		return SymbolVerification{}, false
	}
	entry := elem.Value.(*lruEntry)
	if v.cfg.Clock.Now().Sub(entry.storedAt) > v.cfg.LRUTTL {

		v.lru.Remove(elem)
		delete(v.index, key)
		return SymbolVerification{}, false
	}
	v.lru.MoveToFront(elem)
	cached := entry.value
	cached.Source = string(VerifySourceLiveCache)
	cached.Latency = v.cfg.Clock.Now().Sub(startA)
	return cached, true
}

func (v *Verifier) cacheLRU(key string, sv SymbolVerification) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.cfg.Clock.Now()
	if elem, ok := v.index[key]; ok {
		v.lru.MoveToFront(elem)
		entry := elem.Value.(*lruEntry)
		entry.value = sv
		entry.storedAt = now
		return
	}
	entry := &lruEntry{key: key, value: sv, storedAt: now}
	elem := v.lru.PushFront(entry)
	v.index[key] = elem
	for v.lru.Len() > v.cfg.LRUSize {
		oldest := v.lru.Back()
		if oldest == nil {
			break
		}
		delete(v.index, oldest.Value.(*lruEntry).key)
		v.lru.Remove(oldest)
	}
}

func lruKey(ref SymbolRef) string {
	return string(ref.Ecosystem) + ":" + ref.Version + ":" + ref.SymbolPath
}
