//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// Package cache — tier.go
//
// Body storage tier decision: inline (≤InlineThresholdBytes) vs CAS
// (>InlineThresholdBytes). release
//
// # Design
//
// Research findings carry a body blob (raw search result, tool output, etc.).
// The two-tier scheme keeps small bodies in the SQLite row (inline) for fast
// single-query retrieval and pushes large bodies to the filesystem CAS to avoid
// SQLite page bloat.
//
// Boundary rule: bodies of exactly InlineThresholdBytes bytes are stored
// inline (boundary is inclusive on the inline side). Bodies of
// InlineThresholdBytes+1 bytes and above are stored in CAS.
//
// ContentHash (SHA-256 hex) is always computed and set on Finding regardless of
// tier, enabling:
// - Integrity verification without reading the blob.
// - CAS lookup dedup (the CAS key equals ContentHash).
// - Tier migration (a future gc pass can re-tier without re-downloading).
//
// # Invariants
//
// - inv-hades-F4-01: InlineThresholdBytes == 102400 (100 KiB). Do not change
// without a schema migration and ADR, because existing inline blobs in
// research_findings.body_inline_blob were stored under this contract.
// - inv-hades-F4-02: ContentHash is always a 64-char lowercase hex SHA-256.
// An empty ContentHash means StoreBody was never called on this Finding.
// - inv-hades-F4-03: BodyInlineBlob != nil XOR BodyPath != "" (after StoreBody).
// Both empty means no body; both set simultaneously is a data corruption
// indicator and must never be produced by this package.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

const InlineThresholdBytes = 102400

type Tier int

const (
	TierInline Tier = iota

	TierCAS
)

func (t Tier) String() string {
	switch t {
	case TierInline:
		return "inline"
	case TierCAS:
		return "cas"
	default:
		return fmt.Sprintf("tier(%d)", int(t))
	}
}

func BodyTier(body []byte) Tier {
	if len(body) <= InlineThresholdBytes {
		return TierInline
	}
	return TierCAS
}

var ErrCASRequiredForLargeBody = errors.New("research/cache: CAS handle required for body > InlineThresholdBytes")

func StoreBody(finding *Finding, body []byte, cas *CAS, ext string) error {
	if finding == nil {
		return errors.New("research/cache: StoreBody: finding must not be nil")
	}

	sum := sha256.Sum256(body)
	contentHash := hex.EncodeToString(sum[:])

	switch BodyTier(body) {
	case TierInline:

		blob := make([]byte, len(body))
		copy(blob, body)
		finding.ContentHash = contentHash
		finding.BodyInlineBlob = blob
		finding.BodyPath = ""

	case TierCAS:
		if cas == nil {
			return ErrCASRequiredForLargeBody
		}

		casHash, err := cas.Write(body, ext)
		if err != nil {
			return fmt.Errorf("research/cache: StoreBody CAS write: %w", err)
		}
		finding.ContentHash = contentHash
		finding.BodyInlineBlob = nil
		finding.BodyPath = cas.Path(casHash, ext)
	}

	return nil
}

func LoadBody(finding *Finding, cas *CAS, ext string) ([]byte, error) {
	if finding == nil {
		return nil, errors.New("research/cache: LoadBody: finding must not be nil")
	}

	if finding.BodyInlineBlob != nil {
		out := make([]byte, len(finding.BodyInlineBlob))
		copy(out, finding.BodyInlineBlob)
		return out, nil
	}

	if finding.BodyPath == "" {
		return nil, errors.New("research/cache: LoadBody: finding has no stored body (BodyInlineBlob nil and BodyPath empty)")
	}

	if cas == nil {
		return nil, errors.New("research/cache: LoadBody: CAS handle required to read body from BodyPath")
	}

	data, err := cas.Read(finding.ContentHash, ext)
	if err != nil {
		return nil, err
	}
	return data, nil
}
