// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// internal/citation/envelope_test.go — Plan 11 Phase D Task D-2.
//
// 10000-trial round-trip property test + Hash() determinism + Hash()
// collision-distribution + EnvelopeFromAuditEvent constructor + strict
// validation tests covering inv-zen-166.
package citation_test

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"testing/quick"
	"time"
	"unicode/utf8"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

func envelopeGenerator() func() citation.Envelope {
	types := []citation.CitationType{
		citation.CitationTypeKGNode,
		citation.CitationTypeKGEdge,
		citation.CitationTypeFileSlice,
		citation.CitationTypeCommitRef,
		citation.CitationTypeCommunitySummary,
		citation.CitationTypeAuditEvent,
		citation.CitationTypeCustom,
	}
	sources := []citation.CitationSource{
		citation.SourceCaronteQuery,
		citation.SourceCaronteContext,
		citation.SourceAggregatorFTS,
		citation.SourceAggregatorVec,
		citation.SourceTemporal,
		citation.SourceManualOverride,
	}
	lanes := []citation.RetrievalLane{
		citation.LaneSemantic,
		citation.LaneLexical,
		citation.LaneGraph,
		citation.LaneRerank,
		citation.LaneTemporal,
	}

	var counter uint64
	return func() citation.Envelope {
		counter++

		var idBuf [10]byte
		for i := range idBuf {
			idBuf[i] = "abcdefghijklmnop"[(int(counter)+i*7)%16]
		}
		id := citation.CitationID("c-" + string(idBuf[:]))

		conf := math.Mod(float64(counter)*0.61803398875, 1.0)
		rrfScore := math.Mod(float64(counter)*0.000234, 0.0164)
		rrfRank := int(counter%1000) - 1

		var exp time.Time
		if counter%2 == 0 {
			exp = time.Date(2026, 5, 11, 12, 0, 0, int(counter%1e9), time.UTC)
		}

		return citation.Envelope{
			ID:           id,
			Type:         types[counter%uint64(len(types))],
			Source:       sources[counter%uint64(len(sources))],
			Lane:         lanes[counter%uint64(len(lanes))],
			AuditEventID: "evt-" + string(idBuf[:]),
			Confidence:   conf,
			RRFScore:     rrfScore,
			RRFRank:      rrfRank,
			Expiration:   exp,
			ProjectID:    "proj-" + string(idBuf[:4]),
			Payload:      "payload-" + string(idBuf[:]),
		}
	}
}

func TestEnvelopeRoundtripPreserves(t *testing.T) {
	gen := envelopeGenerator()
	for trial := 0; trial < 10000; trial++ {
		env := gen()
		if err := env.Validate(); err != nil {
			t.Fatalf("trial %d: generated invalid envelope: %v", trial, err)
		}

		raw, err := json.Marshal(&env)
		if err != nil {
			t.Fatalf("trial %d: marshal: %v", trial, err)
		}

		var got citation.Envelope
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("trial %d: unmarshal: %v", trial, err)
		}

		if got.ID != env.ID {
			t.Errorf("trial %d ID drift: want %s got %s", trial, env.ID, got.ID)
		}
		if got.Type != env.Type {
			t.Errorf("trial %d Type drift: want %s got %s", trial, env.Type, got.Type)
		}
		if got.Source != env.Source {
			t.Errorf("trial %d Source drift", trial)
		}
		if got.Lane != env.Lane {
			t.Errorf("trial %d Lane drift", trial)
		}
		if got.Confidence != env.Confidence {
			t.Errorf("trial %d Confidence drift: want %v got %v", trial, env.Confidence, got.Confidence)
		}
		if got.RRFScore != env.RRFScore {
			t.Errorf("trial %d RRFScore drift", trial)
		}
		if got.RRFRank != env.RRFRank {
			t.Errorf("trial %d RRFRank drift", trial)
		}
		if !got.Expiration.Equal(env.Expiration) {
			t.Errorf("trial %d Expiration drift: want %v got %v", trial, env.Expiration, got.Expiration)
		}
		if got.ProjectID != env.ProjectID {
			t.Errorf("trial %d ProjectID drift", trial)
		}
		if got.Payload != env.Payload {
			t.Errorf("trial %d Payload drift", trial)
		}
	}
}

func TestEnvelopeQuickRoundtrip(t *testing.T) {
	f := func(idHi [10]byte, payload [50]byte, projectID [8]byte) bool {

		id := make([]byte, len(idHi))
		for i := range idHi {
			id[i] = "abcdefghijklmnop"[int(idHi[i])%16]
		}
		pj := string(projectID[:])
		pl := string(payload[:])
		// Skip non-UTF-8 inputs: encoding/json rejects/escapes invalid
		// UTF-8 (replaces with �) so they do not round-trip exactly.
		// The envelope schema is a UTF-8 contract (spec §1 Q9 example uses
		// UTF-8 strings throughout); non-UTF-8 callers must convert before
		// emit.
		if !utf8.ValidString(pj) || !utf8.ValidString(pl) {
			return true
		}
		if pj == "" {
			pj = "p"
		}
		if pl == "" {
			pl = "x"
		}
		env := citation.Envelope{
			ID:           citation.CitationID("c-" + string(id)),
			Type:         citation.CitationTypeKGNode,
			Source:       citation.SourceCaronteQuery,
			Lane:         citation.LaneSemantic,
			AuditEventID: "evt-test",
			Confidence:   0.5,
			RRFScore:     0.01,
			RRFRank:      0,
			ProjectID:    pj,
			Payload:      pl,
		}
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
		return reflect.DeepEqual(env, got)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 1000}); err != nil {
		t.Errorf("quick.Check: %v", err)
	}
}

func TestEnvelopeHashStable(t *testing.T) {
	env := citation.Envelope{
		ID:           "c-abcdef0123456789",
		Type:         citation.CitationTypeKGNode,
		Source:       citation.SourceCaronteQuery,
		Lane:         citation.LaneSemantic,
		AuditEventID: "evt-0001",
		Confidence:   0.94,
		RRFScore:     0.0162,
		RRFRank:      0,
		ProjectID:    "internal-platform-x",
		Payload:      "MergeEngine.Score()",
	}
	h1 := env.Hash()
	h2 := env.Hash()
	if h1 != h2 {
		t.Errorf("Hash() not stable: %x vs %x", h1, h2)
	}
}

func TestEnvelopeHashDifferentForDifferentEnvelopes(t *testing.T) {
	a := citation.Envelope{
		ID: "c-aaaa", Type: citation.CitationTypeKGNode, Source: citation.SourceCaronteQuery,
		Lane: citation.LaneSemantic, AuditEventID: "evt-a", Confidence: 0.5,
		ProjectID: "p", Payload: "x",
	}
	b := a
	b.Payload = "y"
	if a.Hash() == b.Hash() {
		t.Error("Hash() collision for different payloads")
	}
}

func TestEnvelopeRejectsInvalidConfidence(t *testing.T) {
	cases := []float64{-0.1, 1.1, math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, c := range cases {
		env := citation.Envelope{
			ID: "c-aaaa", Type: citation.CitationTypeKGNode, Source: citation.SourceCaronteQuery,
			Lane: citation.LaneSemantic, AuditEventID: "evt", Confidence: c, ProjectID: "p", Payload: "x",
		}
		if err := env.Validate(); err == nil {
			t.Errorf("Validate accepted invalid Confidence %v", c)
		}
	}
}

func TestEnvelopeValidateStrictPassesThroughToValidate(t *testing.T) {

	env := citation.Envelope{
		ID: "c-abcdef0123456789", Type: citation.CitationTypeKGNode, Source: citation.SourceCaronteQuery,
		Lane: citation.LaneSemantic, AuditEventID: "evt-0001", Confidence: 0.94, RRFScore: 0.01,
		RRFRank: 0, ProjectID: "p", Payload: "x",
	}
	if err := env.ValidateStrict(); err != nil {
		t.Errorf("ValidateStrict rejected valid envelope: %v", err)
	}
}

func TestEnvelopeValidateStrictRejectsNonFinite(t *testing.T) {

	cases := []citation.Envelope{

		{ID: "c-aa", Type: citation.CitationTypeKGNode, Source: citation.SourceCaronteQuery,
			Lane: citation.LaneSemantic, AuditEventID: "evt", Confidence: math.NaN(), ProjectID: "p", Payload: "x"},

		{ID: "c-aa", Type: citation.CitationTypeKGNode, Source: citation.SourceCaronteQuery,
			Lane: citation.LaneSemantic, AuditEventID: "evt", Confidence: 0.5, RRFScore: math.Inf(1),
			ProjectID: "p", Payload: "x"},

		{ID: "c-aa", Type: citation.CitationTypeKGNode, Source: citation.SourceCaronteQuery,
			Lane: citation.LaneSemantic, AuditEventID: "evt", Confidence: 0.5, RRFScore: math.NaN(),
			ProjectID: "p", Payload: "x"},
	}
	for i, env := range cases {
		if err := env.ValidateStrict(); err == nil {
			t.Errorf("case %d: ValidateStrict accepted non-finite values", i)
		}
	}
}

func TestEnvelopeFromAuditEvent(t *testing.T) {
	row := citation.AuditEventRow{
		ID:        "evt-0001-2026-05-10",
		ProjectID: "internal-platform-x",
		Type:      "AugmentationCompleted",
		Doctrine:  "max-scope",
		Payload:   `{"tokens":1024,"lanes":5}`,
		EmittedAt: 1715299200,
	}
	env, err := citation.EnvelopeFromAuditEvent(row)
	if err != nil {
		t.Fatalf("EnvelopeFromAuditEvent: %v", err)
	}
	if env.AuditEventID != row.ID {
		t.Errorf("AuditEventID: want %s got %s", row.ID, env.AuditEventID)
	}
	if env.ProjectID != row.ProjectID {
		t.Errorf("ProjectID: want %s got %s", row.ProjectID, env.ProjectID)
	}
	if env.Type != citation.CitationTypeAuditEvent {
		t.Errorf("Type: want audit_event got %s", env.Type)
	}
	if env.Confidence != 1.0 {
		t.Errorf("Confidence: want 1.0 got %v", env.Confidence)
	}
	if env.RRFRank != -1 {
		t.Errorf("RRFRank: want -1 got %d", env.RRFRank)
	}
}

func TestEnvelopeFromAuditEventRejectsEmpty(t *testing.T) {

	_, err := citation.EnvelopeFromAuditEvent(citation.AuditEventRow{ProjectID: "p"})
	if err == nil {
		t.Error("EnvelopeFromAuditEvent accepted empty ID")
	}

	_, err = citation.EnvelopeFromAuditEvent(citation.AuditEventRow{ID: "evt-1"})
	if err == nil {
		t.Error("EnvelopeFromAuditEvent accepted empty ProjectID")
	}
}

func TestEnvelopeFromAuditEventLongID(t *testing.T) {

	row := citation.AuditEventRow{
		ID:        "evt-A1B2C3-LONGER-THAN-16-CHARS-WITH-DASHES",
		ProjectID: "p",
		Type:      "Audit",
		Doctrine:  "max-scope",
		Payload:   `{}`,
		EmittedAt: 1715299200,
	}
	env, err := citation.EnvelopeFromAuditEvent(row)
	if err != nil {
		t.Fatalf("EnvelopeFromAuditEvent: %v", err)
	}
	// The citation id MUST validate (sanitised + truncated).
	if err := env.ID.Validate(); err != nil {
		t.Errorf("sanitised ID does not validate: %v (id=%s)", err, env.ID)
	}
	// Full event-id MUST be preserved in AuditEventID.
	if env.AuditEventID != row.ID {
		t.Errorf("AuditEventID truncated: want %s got %s", row.ID, env.AuditEventID)
	}
}

func TestEnvelopeFromAuditEventAllNonAlphanumeric(t *testing.T) {

	row := citation.AuditEventRow{
		ID:        "----",
		ProjectID: "p",
		Type:      "Audit",
		Doctrine:  "max-scope",
		Payload:   `{}`,
		EmittedAt: 1715299200,
	}
	env, err := citation.EnvelopeFromAuditEvent(row)
	if err != nil {
		t.Fatalf("EnvelopeFromAuditEvent: %v", err)
	}
	if err := env.ID.Validate(); err != nil {
		t.Errorf("fallback ID does not validate: %v (id=%s)", err, env.ID)
	}
}

func TestEnvelopeFromAuditEventOneCharSanitised(t *testing.T) {

	row := citation.AuditEventRow{
		ID:        "x-",
		ProjectID: "p",
		Type:      "Audit",
		Doctrine:  "max-scope",
		Payload:   `{}`,
		EmittedAt: 1715299200,
	}
	env, err := citation.EnvelopeFromAuditEvent(row)
	if err != nil {
		t.Fatalf("EnvelopeFromAuditEvent: %v", err)
	}
	if err := env.ID.Validate(); err != nil {
		t.Errorf("padded ID does not validate: %v (id=%s)", err, env.ID)
	}
}

func TestEnvelopeValidateRejectsNonFiniteRRFScore(t *testing.T) {

	cases := []float64{math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, c := range cases {
		env := citation.Envelope{
			ID: "c-aaaa", Type: citation.CitationTypeKGNode, Source: citation.SourceCaronteQuery,
			Lane: citation.LaneSemantic, AuditEventID: "evt", Confidence: 0.5, RRFScore: c,
			ProjectID: "p", Payload: "x",
		}
		if err := env.Validate(); err == nil {
			t.Errorf("Validate accepted non-finite RRFScore %v", c)
		}
	}
}

func TestEnvelopeValidationIndividualMissingFields(t *testing.T) {
	base := func() citation.Envelope {
		return citation.Envelope{
			ID:           "c-abcdef0123456789",
			Type:         citation.CitationTypeKGNode,
			Source:       citation.SourceCaronteQuery,
			Lane:         citation.LaneSemantic,
			AuditEventID: "evt-0001",
			Confidence:   0.94,
			RRFScore:     0.0162,
			RRFRank:      0,
			ProjectID:    "internal-platform-x",
			Payload:      "MergeEngine.Score()",
		}
	}
	cases := []struct {
		name   string
		mutate func(*citation.Envelope)
	}{
		{"missing Type", func(e *citation.Envelope) { e.Type = "" }},
		{"missing Source", func(e *citation.Envelope) { e.Source = "" }},
		{"missing Lane", func(e *citation.Envelope) { e.Lane = "" }},
		{"missing AuditEventID", func(e *citation.Envelope) { e.AuditEventID = "" }},
		{"missing ProjectID", func(e *citation.Envelope) { e.ProjectID = "" }},
		{"missing Payload", func(e *citation.Envelope) { e.Payload = "" }},
		{"negative RRFScore", func(e *citation.Envelope) { e.RRFScore = -0.1 }},
		{"RRFRank below sentinel", func(e *citation.Envelope) { e.RRFRank = -2 }},
		{"invalid CitationID", func(e *citation.Envelope) { e.ID = "bad-id" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := base()
			tc.mutate(&env)
			if err := env.Validate(); err == nil {
				t.Errorf("Validate accepted envelope missing field: %s", tc.name)
			}
		})
	}
}

func TestEnvelopeHashFallsBackOnUnmarshalableValue(t *testing.T) {

	env := citation.Envelope{}

	h1 := env.Hash()
	h2 := env.Hash()
	if h1 != h2 {
		t.Errorf("Hash() not stable on zero-value: %x vs %x", h1, h2)
	}
	if h1 == 0 {

		t.Errorf("Hash() of zero-value is zero (likely fallback path returned uninitialised)")
	}
}

func TestEnvelopeHashFallbackOnNonFiniteFloats(t *testing.T) {

	env := citation.Envelope{
		ID: "c-broken", Type: citation.CitationTypeKGNode, Source: citation.SourceCaronteQuery,
		Lane: citation.LaneSemantic, AuditEventID: "evt", Confidence: 0.5, RRFScore: 0.01,
		ProjectID: "p", Payload: "x",
	}

	env.Confidence = nan()
	h := env.Hash()
	// Even on fallback the hash MUST be stable for the SAME malformed
	// envelope (deterministic of the unmarshalable input).
	if env.Hash() != h {
		t.Errorf("Hash() unstable on non-finite envelope")
	}
}

func nan() float64 {
	var z float64
	return z / z
}

func TestEnvelopeHashFallbackOnPlatformRenderInvalidJSON(t *testing.T) {

	env := citation.Envelope{
		ID: "c-bad", Type: citation.CitationTypeKGNode, Source: citation.SourceCaronteQuery,
		Lane: citation.LaneSemantic, AuditEventID: "evt", Confidence: 0.5, RRFScore: 0.01,
		ProjectID: "p", Payload: "x",
		PlatformRenders: citation.PlatformRenders{
			"ink": citation.PlatformRender{"bad": []byte("{not valid json")},
		},
	}

	h := env.Hash()
	if env.Hash() != h {
		t.Errorf("Hash() unstable on invalid-JSON PlatformRender")
	}
}
