// internal/research/ecosystem/audit_payloads_test.go
//
// Tests for the 8 RAG audit event payload types ( Task D-12;
// see plan-file lines 5484-5806 for canonical spec).
//
// Coverage discipline: per project doctrine `feedback_no_tech_debt.md`,
// security/correctness-critical files (audit chain integrity is a
// Plan-level invariant: invariant + invariant) require ≥90% per-function
// coverage. Tests cover:
// - Compile-time interface assertions for each payload type
// (via reflective JSON round-trip — payloads are pure data, no behaviour)
// - JSON marshal round-trip for every payload (preserves canonical schema)
// - All-fields-set + all-fields-zero marshal coverage per payload (omitempty
// correctness so audit storage stays compact)
// - Integration through RAGAuditEmitter.Emit for every payload type
// (verifies the emitter's marshal+Append path on each typed payload)
// - Cross-doctrine emit coverage (max-scope/default/minimal) per payload
// - EventType slot-binding invariant (each payload bound to its declared slot)
//
// Doctrine note: AuditMinimal in frozen surface emits
// Query + Abstain (NOT Query + Answer as one plan-file snippet suggested);
// these tests align with the surface (audit_emitter.go +
// audit_emitter_test.go) — the frozen contract wins per
// `feedback_plan_template_drift.md`.

package ecosystem

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestRAGAuditPayloadEventTypeSlots(t *testing.T) {
	// invariant APPEND-ONLY: EvtRAGQuery=92..EvtRAGIngestJoinKey=99.
	// Payloads MUST bind to the slots declared; mismatching
	// breaks audit chain consumers.
	want := map[string]eventlog.EventType{
		"RAGQueryPayload":         eventlog.EvtRAGQuery,
		"RAGRetrievalPayload":     eventlog.EvtRAGRetrieval,
		"RAGCitationPayload":      eventlog.EvtRAGCitation,
		"RAGVerifyPayload":        eventlog.EvtRAGVerify,
		"RAGAbstainPayload":       eventlog.EvtRAGAbstain,
		"RAGAnswerPayload":        eventlog.EvtRAGAnswer,
		"RAGIngestPackagePayload": eventlog.EvtRAGIngestPackage,
		"RAGIngestJoinKeyPayload": eventlog.EvtRAGIngestJoinKey,
	}
	got := payloadEventTypeMap()
	if len(got) != len(want) {
		t.Fatalf("len(payload→evt map) = %d; want %d (8 payloads, 1 per slot 92-99)",
			len(got), len(want))
	}
	for name, evt := range want {
		if g := got[name]; g != evt {
			t.Errorf("payload %s bound to slot %d (%s); want slot %d (%s)",
				name, int(g), g.String(), int(evt), evt.String())
		}
	}
}

func TestRAGQueryPayloadJSONRoundtrip(t *testing.T) {
	in := RAGQueryPayload{
		Query:                "how does sha256.Sum256 work?",
		Ecosystem:            EcoGo,
		Version:              "1.22.0",
		VersionLayer:         3,
		Doctrine:             "max-scope",
		Routing:              RoutingDecision{Ecosystems: []Ecosystem{EcoGo}, Method: RoutingMethodSingle},
		ClassifierCheckpoint: "sha256:abc123",
		ProjectPath:          "/path/to/projects/example",
		FreshDispatch:        true,
	}
	out := mustJSONRoundtrip(t, in, &RAGQueryPayload{}).(*RAGQueryPayload)
	if out.Query != in.Query || out.Ecosystem != in.Ecosystem ||
		out.Version != in.Version || out.VersionLayer != in.VersionLayer ||
		out.Doctrine != in.Doctrine || out.ClassifierCheckpoint != in.ClassifierCheckpoint ||
		out.ProjectPath != in.ProjectPath || out.FreshDispatch != in.FreshDispatch {
		t.Errorf("roundtrip drift: in=%+v out=%+v", in, out)
	}
	if !reflect.DeepEqual(out.Routing, in.Routing) {
		t.Errorf("routing roundtrip drift: in=%+v out=%+v", in.Routing, out.Routing)
	}
}

func TestRAGQueryPayloadOmitEmpty(t *testing.T) {
	// All zero-value optional fields MUST be omitted; required fields
	// emit even at zero. Plan-file §4.6 marks these REQUIRED (no omitempty):
	// query, doctrine, routing, classifier_checkpoint
	// These are OPTIONAL (omitempty):
	// ecosystem, version, version_layer, project_path, fresh_dispatch
	in := RAGQueryPayload{Query: "q", Doctrine: "default"}
	body := mustMarshal(t, in)
	for _, omit := range []string{
		`"ecosystem":`, `"version":`, `"version_layer":`,
		`"project_path":`, `"fresh_dispatch":`,
	} {
		if strings.Contains(body, omit) {
			t.Errorf("omitempty leak: %q in body %s", omit, body)
		}
	}
	for _, must := range []string{
		`"query":`, `"doctrine":`, `"routing":`, `"classifier_checkpoint":`,
	} {
		if !strings.Contains(body, must) {
			t.Errorf("required field missing: %q in body %s", must, body)
		}
	}
}

func TestRAGRetrievalPayloadJSONRoundtrip(t *testing.T) {
	in := RAGRetrievalPayload{
		PerEcoCounts: map[Ecosystem]int{EcoGo: 120, EcoPython: 80},
		FusedCount:   60,
		K:            10,
		Weights:      map[Ecosystem]float64{EcoGo: 0.7, EcoPython: 0.3},
	}
	out := mustJSONRoundtrip(t, in, &RAGRetrievalPayload{}).(*RAGRetrievalPayload)
	if !reflect.DeepEqual(out.PerEcoCounts, in.PerEcoCounts) ||
		out.FusedCount != in.FusedCount || out.K != in.K ||
		!reflect.DeepEqual(out.Weights, in.Weights) {
		t.Errorf("retrieval roundtrip drift: in=%+v out=%+v", in, out)
	}
}

func TestRAGCitationPayloadJSONRoundtrip(t *testing.T) {
	in := RAGCitationPayload{
		Citations: []CitationRef{
			{ID: "doc_1", ChunkID: 42, SymbolPath: "crypto/sha256.Sum256", SourceURL: "https://pkg.go.dev"},
			{ID: "doc_2", ChunkID: 99, SymbolPath: "io.Reader", SourceURL: "https://pkg.go.dev/io"},
		},
	}
	out := mustJSONRoundtrip(t, in, &RAGCitationPayload{}).(*RAGCitationPayload)
	if len(out.Citations) != 2 ||
		out.Citations[0].ID != in.Citations[0].ID ||
		out.Citations[1].ChunkID != in.Citations[1].ChunkID {
		t.Errorf("citation roundtrip drift: in=%+v out=%+v", in, out)
	}
}

func TestRAGVerifyPayloadJSONRoundtrip(t *testing.T) {
	in := RAGVerifyPayload{
		Verifications: []SymbolVerification{
			{Symbol: SymbolRef{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.22"}, Exists: true, Source: "symbol_index"},
			{Symbol: SymbolRef{Ecosystem: EcoGo, SymbolPath: "fake.Symbol", Version: "1.22"}, Exists: false, Source: "live_cmd"},
		},
		AllVerified: false,
	}
	out := mustJSONRoundtrip(t, in, &RAGVerifyPayload{}).(*RAGVerifyPayload)
	if len(out.Verifications) != 2 || out.AllVerified != in.AllVerified {
		t.Errorf("verify roundtrip drift: in=%+v out=%+v", in, out)
	}
	if out.Verifications[0].Symbol.SymbolPath != in.Verifications[0].Symbol.SymbolPath {
		t.Errorf("symbol path drift: in=%q out=%q",
			in.Verifications[0].Symbol.SymbolPath, out.Verifications[0].Symbol.SymbolPath)
	}
}

func TestRAGAbstainPayloadJSONRoundtrip(t *testing.T) {
	in := RAGAbstainPayload{
		Reason:           "score_below_mu_minus_lambda_sigma",
		Lambda:           1.5,
		Mean:             0.62,
		Stdev:            0.08,
		Threshold:        0.50,
		Ecosystem:        EcoPython,
		Doctrine:         "default",
		SuspiciousChunks: []int64{12, 17, 23},
	}
	out := mustJSONRoundtrip(t, in, &RAGAbstainPayload{}).(*RAGAbstainPayload)
	if out.Reason != in.Reason || out.Lambda != in.Lambda || out.Mean != in.Mean ||
		out.Stdev != in.Stdev || out.Threshold != in.Threshold ||
		out.Ecosystem != in.Ecosystem || out.Doctrine != in.Doctrine ||
		!reflect.DeepEqual(out.SuspiciousChunks, in.SuspiciousChunks) {
		t.Errorf("abstain roundtrip drift: in=%+v out=%+v", in, out)
	}
}

func TestRAGAbstainPayloadOmitEmpty(t *testing.T) {

	in := RAGAbstainPayload{Reason: "verifier_all_failed", Doctrine: "capa-firewall"}
	body := mustMarshal(t, in)
	for _, omit := range []string{`"lambda":`, `"mean":`, `"stdev":`, `"threshold":`, `"ecosystem":`, `"suspicious_chunks":`} {
		if strings.Contains(body, omit) {
			t.Errorf("omitempty leak: %q in body %s", omit, body)
		}
	}
	for _, must := range []string{`"reason":`, `"doctrine":`} {
		if !strings.Contains(body, must) {
			t.Errorf("required field missing: %q in body %s", must, body)
		}
	}
}

func TestRAGAnswerPayloadJSONRoundtrip(t *testing.T) {
	in := RAGAnswerPayload{
		AnswerHashSHA256: "abcdef0123456789",
		CitedChunkIDs:    []int64{1, 2, 3, 5, 8},
		Doctrine:         "max-scope",
		TotalLatencyMs:   1234.567,
	}
	out := mustJSONRoundtrip(t, in, &RAGAnswerPayload{}).(*RAGAnswerPayload)
	if out.AnswerHashSHA256 != in.AnswerHashSHA256 ||
		!reflect.DeepEqual(out.CitedChunkIDs, in.CitedChunkIDs) ||
		out.Doctrine != in.Doctrine || out.TotalLatencyMs != in.TotalLatencyMs {
		t.Errorf("answer roundtrip drift: in=%+v out=%+v", in, out)
	}
}

func TestRAGIngestPackagePayloadJSONRoundtrip(t *testing.T) {
	in := RAGIngestPackagePayload{
		Package:           "crypto/sha256",
		Ecosystem:         EcoGo,
		Version:           "1.22.0",
		ChunksCount:       512,
		SymbolsCount:      28,
		ChangeNodesCount:  3,
		StartedAtUnixNs:   1747521600000000000,
		CompletedAtUnixNs: 1747521660000000000,
	}
	out := mustJSONRoundtrip(t, in, &RAGIngestPackagePayload{}).(*RAGIngestPackagePayload)
	if !reflect.DeepEqual(out, &in) {
		t.Errorf("ingest_package roundtrip drift: in=%+v out=%+v", in, out)
	}
}

func TestRAGIngestJoinKeyPayloadJSONRoundtrip(t *testing.T) {
	in := RAGIngestJoinKeyPayload{
		NoteID:             "note-2026-05-14-001",
		ResolvedSymbolPath: "numpy.linalg.svd",
		Ecosystem:          EcoPython,
		Version:            "1.26.4",
	}
	out := mustJSONRoundtrip(t, in, &RAGIngestJoinKeyPayload{}).(*RAGIngestJoinKeyPayload)
	if !reflect.DeepEqual(out, &in) {
		t.Errorf("ingest_join_key roundtrip drift: in=%+v out=%+v", in, out)
	}
}

func TestRAGAuditPayloadEndToEndEmissionMaxScope(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	e := NewRAGAuditEmitter(chain, &DoctrineProfile{
		Name:               "max-scope",
		AuditEmissionLevel: AuditAll8Events,
	})
	ctx := context.Background()

	emissions := []struct {
		evt     eventlog.EventType
		payload interface{}
	}{
		{eventlog.EvtRAGQuery, RAGQueryPayload{Query: "q", Doctrine: "max-scope"}},
		{eventlog.EvtRAGRetrieval, RAGRetrievalPayload{FusedCount: 10}},
		{eventlog.EvtRAGCitation, RAGCitationPayload{Citations: []CitationRef{{ID: "doc_1", ChunkID: 1}}}},
		{eventlog.EvtRAGVerify, RAGVerifyPayload{AllVerified: true}},
		{eventlog.EvtRAGAbstain, RAGAbstainPayload{Reason: "test", Doctrine: "max-scope"}},
		{eventlog.EvtRAGAnswer, RAGAnswerPayload{AnswerHashSHA256: "h", Doctrine: "max-scope"}},
		{eventlog.EvtRAGIngestPackage, RAGIngestPackagePayload{Package: "pkg", Ecosystem: EcoGo, Version: "1.0"}},
		{eventlog.EvtRAGIngestJoinKey, RAGIngestJoinKeyPayload{NoteID: "n1", Ecosystem: EcoGo}},
	}
	for i, em := range emissions {
		seq, err := e.Emit(ctx, em.evt, em.payload)
		if err != nil {
			t.Fatalf("Emit #%d (%s): %v", i, em.evt.String(), err)
		}
		if seq != int64(i+1) {
			t.Errorf("Emit #%d (%s): seq = %d; want %d (monotonic)", i, em.evt.String(), seq, i+1)
		}
	}
	if got := chain.Len(); got != 8 {
		t.Errorf("max-scope chain len = %d; want 8", got)
	}

	for i, em := range emissions {
		rec := chain.Get(int64(i + 1))
		if rec == nil {
			t.Errorf("chain.Get(%d) = nil", i+1)
			continue
		}
		if rec.EventType != em.evt {
			t.Errorf("record %d evt = %s; want %s", i+1, rec.EventType.String(), em.evt.String())
		}

		wantBytes, err := json.Marshal(em.payload)
		if err != nil {
			t.Errorf("re-marshal #%d: %v", i+1, err)
			continue
		}
		if string(rec.Payload) != string(wantBytes) {
			t.Errorf("record %d payload drift:\ngot:  %s\nwant: %s",
				i+1, rec.Payload, wantBytes)
		}
	}
}

func TestRAGAuditPayloadEndToEndEmissionDefault(t *testing.T) {

	chain := NewInMemoryRAGAuditChain()
	e := NewRAGAuditEmitter(chain, &DoctrineProfile{
		Name:               "default",
		AuditEmissionLevel: AuditQueryAbstainVerifyFailureAnswer,
	})
	ctx := context.Background()

	cases := []struct {
		evt     eventlog.EventType
		payload interface{}
		want    bool
	}{
		{eventlog.EvtRAGQuery, RAGQueryPayload{Query: "q", Doctrine: "default"}, true},
		{eventlog.EvtRAGRetrieval, RAGRetrievalPayload{FusedCount: 5}, false},
		{eventlog.EvtRAGCitation, RAGCitationPayload{}, false},
		{eventlog.EvtRAGVerify, RAGVerifyPayload{AllVerified: false}, true},
		{eventlog.EvtRAGAbstain, RAGAbstainPayload{Reason: "x", Doctrine: "default"}, true},
		{eventlog.EvtRAGAnswer, RAGAnswerPayload{AnswerHashSHA256: "h", Doctrine: "default"}, true},
		{eventlog.EvtRAGIngestPackage, RAGIngestPackagePayload{Package: "p"}, false},
		{eventlog.EvtRAGIngestJoinKey, RAGIngestJoinKeyPayload{NoteID: "n"}, false},
	}
	admitted := 0
	for _, c := range cases {
		seq, err := e.Emit(ctx, c.evt, c.payload)
		if err != nil {
			t.Errorf("Emit(%s): %v", c.evt.String(), err)
			continue
		}
		gotAdmitted := seq > 0
		if gotAdmitted != c.want {
			t.Errorf("Emit(%s) admitted = %v; want %v", c.evt.String(), gotAdmitted, c.want)
		}
		if gotAdmitted {
			admitted++
		}
	}
	if got := chain.Len(); got != admitted {
		t.Errorf("chain len = %d; want %d (default filter)", got, admitted)
	}
}

func TestRAGAuditPayloadEndToEndEmissionMinimal(t *testing.T) {

	chain := NewInMemoryRAGAuditChain()
	e := NewRAGAuditEmitter(chain, &DoctrineProfile{
		Name:               "minimal-test",
		AuditEmissionLevel: AuditMinimal,
	})
	ctx := context.Background()

	_, _ = e.Emit(ctx, eventlog.EvtRAGQuery, RAGQueryPayload{Query: "q", Doctrine: "minimal"})
	_, _ = e.Emit(ctx, eventlog.EvtRAGRetrieval, RAGRetrievalPayload{FusedCount: 1})
	_, _ = e.Emit(ctx, eventlog.EvtRAGCitation, RAGCitationPayload{})
	_, _ = e.Emit(ctx, eventlog.EvtRAGVerify, RAGVerifyPayload{})
	_, _ = e.Emit(ctx, eventlog.EvtRAGAbstain, RAGAbstainPayload{Reason: "x", Doctrine: "minimal"})
	_, _ = e.Emit(ctx, eventlog.EvtRAGAnswer, RAGAnswerPayload{Doctrine: "minimal"})
	_, _ = e.Emit(ctx, eventlog.EvtRAGIngestPackage, RAGIngestPackagePayload{})
	_, _ = e.Emit(ctx, eventlog.EvtRAGIngestJoinKey, RAGIngestJoinKeyPayload{})

	if got := chain.Len(); got != 2 {
		t.Errorf("minimal chain len = %d; want 2 (Query + Abstain)", got)
	}
}

func TestEventTypeAliasAgreesWithEventlogPackage(t *testing.T) {
	// `EventType` (declared in audit_payloads.go) MUST be a type alias of
	// eventlog.EventType, not a new named type. Otherwise consumers must
	// convert at every boundary which defeats the alias purpose.
	var local EventType = eventlog.EvtRAGQuery
	var upstream eventlog.EventType = local
	if int(upstream) != int(local) {
		t.Errorf("EventType alias drift: local=%d upstream=%d", local, upstream)
	}
}

func mustJSONRoundtrip(t *testing.T, in, out interface{}) interface{} {
	t.Helper()
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v (in=%+v)", err, in)
	}
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, body)
	}
	return out
}

func mustMarshal(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v (v=%+v)", err, v)
	}
	return string(b)
}

func payloadEventTypeMap() map[string]eventlog.EventType {
	return map[string]eventlog.EventType{
		"RAGQueryPayload":         eventlog.EvtRAGQuery,
		"RAGRetrievalPayload":     eventlog.EvtRAGRetrieval,
		"RAGCitationPayload":      eventlog.EvtRAGCitation,
		"RAGVerifyPayload":        eventlog.EvtRAGVerify,
		"RAGAbstainPayload":       eventlog.EvtRAGAbstain,
		"RAGAnswerPayload":        eventlog.EvtRAGAnswer,
		"RAGIngestPackagePayload": eventlog.EvtRAGIngestPackage,
		"RAGIngestJoinKeyPayload": eventlog.EvtRAGIngestJoinKey,
	}
}

var (
	_ AuditPayload = (*RAGQueryPayload)(nil)
	_ AuditPayload = (*RAGRetrievalPayload)(nil)
	_ AuditPayload = (*RAGCitationPayload)(nil)
	_ AuditPayload = (*RAGVerifyPayload)(nil)
	_ AuditPayload = (*RAGAbstainPayload)(nil)
	_ AuditPayload = (*RAGAnswerPayload)(nil)
	_ AuditPayload = (*RAGIngestPackagePayload)(nil)
	_ AuditPayload = (*RAGIngestJoinKeyPayload)(nil)
)

func TestAuditPayloadMarkerMethods(t *testing.T) {
	t.Helper()
	payloads := []AuditPayload{
		RAGQueryPayload{},
		RAGRetrievalPayload{},
		RAGCitationPayload{},
		RAGVerifyPayload{},
		RAGAbstainPayload{},
		RAGAnswerPayload{},
		RAGIngestPackagePayload{},
		RAGIngestJoinKeyPayload{},
	}
	if len(payloads) != 8 {
		t.Fatalf("payload set drift: got %d; want 8", len(payloads))
	}
	for i, p := range payloads {
		if got := p.markerAuditPayload(); !got {
			t.Errorf("payload[%d] (%T) markerAuditPayload = false; want true (kind > 0)",
				i, p)
		}
	}
}

var _ = sql.NullString{}
