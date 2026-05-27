// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// internal/citation/property_test.go — Task D-7.
//
// Property-based 10000-trial round-trip + hash determinism + hash
// collision-distribution tests. Custom quick.Generator covers BMP +
// SMP UTF-8 payloads, all 7 CitationType + 6 CitationSource + 5
// RetrievalLane combinations, RRFRank sentinel -1 + ranks [0, 1000),
// nanosecond-precision Expiration, varied ProjectID + AuditEventID.
//
// invariant: load-bearing round-trip preservation anchor (10000 trials).
package citation_test

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
	"time"
	"unicode/utf8"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

func generateUTF8Payload(rnd *rand.Rand) string {
	n := rnd.Intn(200) + 1
	out := make([]rune, 0, n)
	for i := 0; i < n; i++ {
		switch rnd.Intn(10) {
		case 0:

			out = append(out, rune(rnd.Intn(126-32)+32))
		case 1, 2:

			out = append(out, rune(rnd.Intn(255-128)+128))
		case 3, 4, 5:

			r := rune(rnd.Intn(0xFFFE-0x100) + 0x100)

			if r >= 0xD800 && r <= 0xDFFF {
				r = 'a'
			}
			out = append(out, r)
		case 6:

			out = append(out, rune(rnd.Intn(0x1F9FF-0x1F300)+0x1F300))
		default:
			out = append(out, 'a')
		}
	}
	s := string(out)
	if !utf8.ValidString(s) {

		return "fallback-ascii-payload"
	}
	return s
}

type envelopeQuickGen struct{}

func (envelopeQuickGen) Generate(rnd *rand.Rand, _ int) reflect.Value {
	idChars := "abcdefghijklmnop0123456789"
	idLen := rnd.Intn(15) + 2
	idBytes := make([]byte, idLen)
	for i := range idBytes {
		idBytes[i] = idChars[rnd.Intn(len(idChars))]
	}

	types := []citation.CitationType{
		citation.CitationTypeKGNode, citation.CitationTypeKGEdge,
		citation.CitationTypeFileSlice, citation.CitationTypeCommitRef,
		citation.CitationTypeCommunitySummary, citation.CitationTypeAuditEvent,
		citation.CitationTypeCustom,
	}
	sources := []citation.CitationSource{
		citation.SourceCaronteQuery, citation.SourceCaronteContext,
		citation.SourceAggregatorFTS, citation.SourceAggregatorVec,
		citation.SourceTemporal, citation.SourceManualOverride,
	}
	lanes := []citation.RetrievalLane{
		citation.LaneSemantic, citation.LaneLexical, citation.LaneGraph,
		citation.LaneRerank, citation.LaneTemporal,
	}

	var exp time.Time
	if rnd.Intn(2) == 0 {
		exp = time.Date(2026, 5, 11, rnd.Intn(24), rnd.Intn(60), rnd.Intn(60), rnd.Intn(1e9), time.UTC)
	}

	rrfRank := -1
	if rnd.Intn(4) > 0 {
		rrfRank = rnd.Intn(1000)
	}

	projTrim := minInt(len(idBytes), 8)
	env := citation.Envelope{
		ID:           citation.CitationID("c-" + string(idBytes)),
		Type:         types[rnd.Intn(len(types))],
		Source:       sources[rnd.Intn(len(sources))],
		Lane:         lanes[rnd.Intn(len(lanes))],
		AuditEventID: "evt-" + string(idBytes),
		Confidence:   rnd.Float64(),
		RRFScore:     rnd.Float64() * 0.0164,
		RRFRank:      rrfRank,
		Expiration:   exp,
		ProjectID:    "proj-" + string(idBytes[:projTrim]),
		Payload:      generateUTF8Payload(rnd),
	}
	return reflect.ValueOf(env)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestEnvelopeRoundtripPropertyBased10000(t *testing.T) {
	roundtripCheck := func(env citation.Envelope) bool {
		if err := env.Validate(); err != nil {
			return true
		}
		raw, err := json.Marshal(&env)
		if err != nil {
			return false
		}
		var got citation.Envelope
		if err := json.Unmarshal(raw, &got); err != nil {
			return false
		}

		if !env.Expiration.Equal(got.Expiration) {
			return false
		}
		envCopy := env
		gotCopy := got
		envCopy.Expiration = time.Time{}
		gotCopy.Expiration = time.Time{}
		return reflect.DeepEqual(envCopy, gotCopy)
	}

	cfg := &quick.Config{MaxCount: 10000, Values: func(args []reflect.Value, rnd *rand.Rand) {
		args[0] = envelopeQuickGen{}.Generate(rnd, 0)
	}}
	if err := quick.Check(roundtripCheck, cfg); err != nil {
		t.Errorf("property roundtrip failed: %v", err)
	}
}

func TestEnvelopeHashDeterminism10000(t *testing.T) {
	env := citation.Envelope{
		ID: "c-test1234567890", Type: citation.CitationTypeKGNode,
		Source: citation.SourceCaronteQuery, Lane: citation.LaneSemantic,
		AuditEventID: "evt-test", Confidence: 0.94, RRFScore: 0.0162, RRFRank: 0,
		ProjectID: "internal-platform-x", Payload: "MergeEngine.Score()",
	}
	expected := env.Hash()
	for i := 0; i < 10000; i++ {
		got := env.Hash()
		if got != expected {
			t.Fatalf("trial %d: Hash() not deterministic: want %x got %x", i, expected, got)
		}
	}
}

func TestEnvelopeHashCollisionDistribution(t *testing.T) {

	seen := make(map[uint64]string)
	gen := envelopeQuickGen{}
	rnd := rand.New(rand.NewSource(42))
	collisions := 0
	for i := 0; i < 1000; i++ {
		env := gen.Generate(rnd, 0).Interface().(citation.Envelope)
		if err := env.Validate(); err != nil {
			continue
		}
		h := env.Hash()
		if prev, ok := seen[h]; ok {

			if prev == string(env.ID) {
				continue
			}
			t.Errorf("hash collision: %s vs %s → %x", prev, env.ID, h)
			collisions++
			if collisions > 10 {
				t.Fatal("too many collisions; aborting")
			}
		}
		seen[h] = string(env.ID)
	}
}
