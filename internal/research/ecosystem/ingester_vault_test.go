package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var _ VaultWriter = (*recordingVault)(nil)

type recordingVault struct {
	mu     sync.Mutex
	writes map[int64][]string
	err    error
}

func (r *recordingVault) UpdateEcosystemJoinKeys(_ context.Context, noteID int64, joinKeys []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.writes == nil {
		r.writes = make(map[int64][]string)
	}

	cp := make([]string, len(joinKeys))
	copy(cp, joinKeys)
	r.writes[noteID] = cp
	return nil
}

func makeNoteWithContent(id int64, content string) Note {
	return Note{
		ID:        id,
		ProjectID: "test-project",
		Path:      "test/note.md",
		Content:   content,
	}
}

func TestIngester_ProcessVaultNote_DetectsCryptoSha256(t *testing.T) {
	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
	}}
	vault := &recordingVault{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	note := makeNoteWithContent(42, "Use `crypto/sha256.Sum256` for fixed-size hashing.")
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote: %v", err)
	}
	got, ok := vault.writes[42]
	if !ok {
		t.Fatalf("vault.writes[42] missing; got keys: %v", vault.writes)
	}
	want := "go:1.23:crypto/sha256.Sum256"
	found := false
	for _, k := range got {
		if k == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("join_keys = %v; want to contain %q", got, want)
	}
}

func TestIngester_ProcessVaultNote_CrossEcosystemResolution(t *testing.T) {
	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
		{Ecosystem: EcoPython, SymbolPath: "numpy.linalg.solve", Version: "1.26"},
		{Ecosystem: EcoTypeScript, SymbolPath: "react.useState", Version: "18.2"},
		{Ecosystem: EcoRust, SymbolPath: "serde::Serialize", Version: "1.0"},
	}}
	vault := &recordingVault{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	fixturePath := filepath.Join("ingester_testdata", "vault_note_sample.md")
	body, rerr := os.ReadFile(fixturePath)
	if rerr != nil {
		t.Fatalf("read fixture: %v", rerr)
	}
	note := makeNoteWithContent(43, string(body))
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote: %v", err)
	}
	got, ok := vault.writes[43]
	if !ok {
		t.Fatalf("vault.writes[43] missing")
	}
	if len(got) < 4 {
		t.Errorf("len(join_keys) = %d; want ≥4 (one per ecosystem)", len(got))
	}
	// All 4 ecosystem prefixes MUST be present.
	prefixes := []string{"go:", "python:", "typescript:", "rust:"}
	for _, prefix := range prefixes {
		found := false
		for _, k := range got {
			if strings.HasPrefix(k, prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing prefix %q in join_keys = %v", prefix, got)
		}
	}
}

func TestIngester_ProcessVaultNote_IdempotentAppend(t *testing.T) {
	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
	}}
	vault := &recordingVault{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	note := makeNoteWithContent(50, "Use `crypto/sha256.Sum256` for hashing.")
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote (1st): %v", err)
	}
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote (2nd): %v", err)
	}
	got, ok := vault.writes[50]
	if !ok {
		t.Fatalf("vault.writes[50] missing")
	}
	if len(got) != 1 {
		t.Errorf("len(writes[50]) = %d; want 1 (recordingVault overwrites + dedup within run)", len(got))
	}
}

func TestIngester_ProcessVaultNote_AuditEmitsJoinKey(t *testing.T) {
	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
	}}
	vault := &recordingVault{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	note := makeNoteWithContent(60, "Use `crypto/sha256.Sum256` for hashing.")
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote: %v", err)
	}
	events := chain.snapshot()
	if len(events) == 0 {
		t.Fatalf("audit events = 0; want ≥1 (one per resolved join_key)")
	}
	// Every event MUST be EvtRAGIngestJoinKey.
	for i, ev := range events {
		if ev.EventType != uint32(eventlog.EvtRAGIngestJoinKey) {
			t.Errorf("event %d EventType = %d; want %d (EvtRAGIngestJoinKey)", i, ev.EventType, uint32(eventlog.EvtRAGIngestJoinKey))
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Fatalf("event %d payload unmarshal: %v (raw=%s)", i, err, string(ev.Payload))
		}
		if _, ok := payload["note_id"]; !ok {
			t.Errorf("event %d payload missing note_id (got keys: %v)", i, mapKeys(payload))
		}
	}
}

func TestIngester_ProcessVaultNote_NoSymbolsNoOp(t *testing.T) {
	sym := &recordingSymbolIndex{}
	vault := &recordingVault{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	note := makeNoteWithContent(70, "This note has only prose. No code references whatsoever.")
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote: %v", err)
	}
	if _, ok := vault.writes[70]; ok {
		t.Errorf("vault.writes[70] present; want absent (no-op)")
	}
	if got := len(chain.snapshot()); got != 0 {
		t.Errorf("audit events = %d; want 0 (no-symbols → no emit)", got)
	}
}

func TestIngester_ProcessVaultNote_NilSymbolIndex_Error(t *testing.T) {
	vault := &recordingVault{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  nil,
		VaultWriter:  vault,
		WorkerCount:  1,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	note := makeNoteWithContent(80, "Use `crypto/sha256.Sum256` here.")
	err = ing.ProcessVaultNote(context.Background(), note)
	if err == nil {
		t.Errorf("ProcessVaultNote(nil SymbolIndex) = nil error; want non-nil")
	}
}

func TestIngester_ProcessVaultNote_NilVaultWriter_SilentSkip(t *testing.T) {
	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
	}}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  nil,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	note := makeNoteWithContent(81, "Use `crypto/sha256.Sum256` here.")
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote (nil VaultWriter): %v", err)
	}

}

type errorOnLookupSymbolIndex struct {
	recordingSymbolIndex
	errOnPath string
}

func (e *errorOnLookupSymbolIndex) Lookup(ctx context.Context, symPath string) (SymbolRef, bool, error) {
	if symPath == e.errOnPath {
		return SymbolRef{}, false, errors.New("synthetic lookup err")
	}
	return e.recordingSymbolIndex.Lookup(ctx, symPath)
}

func TestIngester_ProcessVaultNote_LookupError_SkipsSymbol(t *testing.T) {
	sym := &errorOnLookupSymbolIndex{
		recordingSymbolIndex: recordingSymbolIndex{symbols: []SymbolRef{
			{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
			{Ecosystem: EcoPython, SymbolPath: "numpy.linalg.solve", Version: "1.26"},
		}},
		errOnPath: "crypto/sha256.Sum256",
	}
	vault := &recordingVault{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	note := makeNoteWithContent(82, "Use `crypto/sha256.Sum256` and `numpy.linalg.solve`.")
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote: %v", err)
	}
	got, ok := vault.writes[82]
	if !ok {
		t.Fatalf("vault.writes[82] missing")
	}

	if len(got) != 1 {
		t.Errorf("len(join_keys) = %d; want 1 (errored candidate skipped)", len(got))
	}
	if len(got) == 1 && !strings.HasPrefix(got[0], "python:") {
		t.Errorf("join_keys[0] = %s; want python: prefix (Go errored)", got[0])
	}
}

func TestIngester_ProcessVaultNote_VaultWriteError_PropagatesUp(t *testing.T) {
	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
	}}
	vault := &recordingVault{err: errors.New("sqlite locked")}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	note := makeNoteWithContent(83, "Use `crypto/sha256.Sum256`.")
	err = ing.ProcessVaultNote(context.Background(), note)
	if err == nil {
		t.Fatalf("ProcessVaultNote (vault err) = nil error; want non-nil")
	}
	if !strings.Contains(err.Error(), "sqlite locked") {
		t.Errorf("err = %v; want wrap of 'sqlite locked'", err)
	}
}

func TestIngester_ProcessVaultNote_AuditFieldsAllPresent(t *testing.T) {
	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
	}}
	vault := &recordingVault{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	note := makeNoteWithContent(84, "Use `crypto/sha256.Sum256`.")
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote: %v", err)
	}
	events := chain.snapshot()
	if len(events) != 1 {
		t.Fatalf("audit events = %d; want 1", len(events))
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	for _, key := range []string{"note_id", "join_key", "symbols"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("audit payload missing %q (keys: %v)", key, mapKeys(payload))
		}
	}
	// note_id MUST equal 84.
	if v, _ := payload["note_id"].(float64); int64(v) != 84 {
		t.Errorf("note_id = %v; want 84", payload["note_id"])
	}
	// join_key MUST start with "go:".
	if jk, _ := payload["join_key"].(string); !strings.HasPrefix(jk, "go:") {
		t.Errorf("join_key = %v; want go: prefix", payload["join_key"])
	}
}

func TestIngester_ProcessVaultNote_DedupCandidates(t *testing.T) {
	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
	}}
	vault := &recordingVault{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	note := makeNoteWithContent(85, "First mention: `crypto/sha256.Sum256`. Second: `crypto/sha256.Sum256`.")
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote: %v", err)
	}
	got, ok := vault.writes[85]
	if !ok {
		t.Fatalf("vault.writes[85] missing")
	}
	if len(got) != 1 {
		t.Errorf("len(join_keys) = %d; want 1 (dedup)", len(got))
	}
}

func TestIngester_ProcessVaultNote_UnresolvableSymbol_NoAudit(t *testing.T) {

	sym := &recordingSymbolIndex{}
	vault := &recordingVault{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	note := makeNoteWithContent(86, "Reference: `crypto/sha256.Sum256` (unresolvable).")
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote: %v", err)
	}
	if _, ok := vault.writes[86]; ok {
		t.Errorf("vault.writes[86] present; want absent (no resolved symbols)")
	}
	if got := len(chain.snapshot()); got != 0 {
		t.Errorf("audit events = %d; want 0 (no resolved symbols)", got)
	}
}

func TestDetectSymbolCandidates_GoSymbol(t *testing.T) {
	content := "Reference: `crypto/sha256.Sum256` in code."
	got := detectSymbolCandidates(content)
	want := "crypto/sha256.Sum256"
	found := false
	for _, c := range got {
		if c == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("detectSymbolCandidates(%q) = %v; want to contain %q", content, got, want)
	}
}

func TestDetectSymbolCandidates_PythonSymbol(t *testing.T) {
	content := "Use `numpy.linalg.solve` for matrix solves."
	got := detectSymbolCandidates(content)
	want := "numpy.linalg.solve"
	found := false
	for _, c := range got {
		if c == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("detectSymbolCandidates(%q) = %v; want to contain %q", content, got, want)
	}
}

func TestDetectSymbolCandidates_TypeScriptSymbol(t *testing.T) {
	content := "Use `react.useState` for component state."
	got := detectSymbolCandidates(content)
	want := "react.useState"
	found := false
	for _, c := range got {
		if c == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("detectSymbolCandidates(%q) = %v; want to contain %q", content, got, want)
	}
}

func TestDetectSymbolCandidates_RustSymbol(t *testing.T) {
	content := "Use `serde::Serialize` for deriving serialization."
	got := detectSymbolCandidates(content)
	want := "serde::Serialize"
	found := false
	for _, c := range got {
		if c == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("detectSymbolCandidates(%q) = %v; want to contain %q", content, got, want)
	}
}

func TestDetectSymbolCandidates_NoSymbols_Empty(t *testing.T) {
	content := "This is purely prose with no code references."
	got := detectSymbolCandidates(content)
	if len(got) != 0 {
		t.Errorf("detectSymbolCandidates(prose) = %v; want empty", got)
	}
}

func TestDedupStrings_Order_Preserving(t *testing.T) {
	in := []string{"a", "b", "a", "c", "b"}
	got := dedupStrings(in)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len = %d; want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %s; want %s", i, got[i], want[i])
		}
	}
}

func TestDedupStrings_Empty(t *testing.T) {
	got := dedupStrings(nil)
	if len(got) != 0 {
		t.Errorf("dedupStrings(nil) = %v; want empty", got)
	}
	got = dedupStrings([]string{})
	if len(got) != 0 {
		t.Errorf("dedupStrings([]) = %v; want empty", got)
	}
}

type cancellingSymbolIndex struct {
	recordingSymbolIndex
	cancelAfter int
	n           int
	cancel      context.CancelFunc
}

func (c *cancellingSymbolIndex) Lookup(ctx context.Context, symPath string) (SymbolRef, bool, error) {
	c.n++
	if c.n >= c.cancelAfter {
		c.cancel()
	}
	return c.recordingSymbolIndex.Lookup(ctx, symPath)
}

func TestIngester_ProcessVaultNote_CtxCancelledMidLoop_ReturnsErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sym := &cancellingSymbolIndex{
		recordingSymbolIndex: recordingSymbolIndex{symbols: []SymbolRef{
			{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
		}},
		cancelAfter: 1,
		cancel:      cancel,
	}
	vault := &recordingVault{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	note := makeNoteWithContent(89, "Use `crypto/sha256.Sum256` and `numpy.linalg.solve`.")
	err = ing.ProcessVaultNote(ctx, note)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("ProcessVaultNote(mid-loop cancel) = %v; want context.Canceled", err)
	}
}

func TestIngester_ProcessVaultNote_CtxCancelled_ReturnsErr(t *testing.T) {
	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
	}}
	vault := &recordingVault{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	note := makeNoteWithContent(87, "Use `crypto/sha256.Sum256`.")
	err = ing.ProcessVaultNote(ctx, note)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("ProcessVaultNote(cancelled ctx) = %v; want context.Canceled", err)
	}
}

func TestIngester_ProcessVaultNote_PreCanceledCtxEmptyContent_ReturnsCanceled(t *testing.T) {
	sym := &recordingSymbolIndex{}
	vault := &recordingVault{}
	chain := &recordingAuditChain{}
	ing, _ := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{}}},
		SymbolIndex:  sym,
		AuditChain:   chain,
		DoctrineName: "default",
	})
	ing.opts.VaultWriter = vault

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	note := Note{ID: 80, Content: "just prose; no symbols here at all"}
	err := ing.ProcessVaultNote(ctx, note)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled on pre-cancelled ctx with empty content; got %v", err)
	}
	if len(vault.writes) != 0 {
		t.Errorf("expected no vault writes on cancelled ctx; got %d", len(vault.writes))
	}
}

func TestIngester_ProcessVaultNote_NilAuditChain_Silent(t *testing.T) {
	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
	}}
	vault := &recordingVault{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		AuditChain:   nil,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	note := makeNoteWithContent(88, "Use `crypto/sha256.Sum256`.")
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote (nil AuditChain): %v", err)
	}
	if _, ok := vault.writes[88]; !ok {
		t.Errorf("vault.writes[88] missing; vault MUST still be written even with nil audit")
	}
}

func TestProcessVaultNote_JoinKeyFormat_StrictTripartite(t *testing.T) {

	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
		{Ecosystem: EcoPython, SymbolPath: "numpy.linalg.solve", Version: "1.26.0"},
		{Ecosystem: EcoTypeScript, SymbolPath: "react.useState", Version: "18.2.0"},
		{Ecosystem: EcoRust, SymbolPath: "serde::Serialize", Version: "1.0.219"},
	}}
	vault := &recordingVault{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		AuditChain:   chain,
		WorkerCount:  1,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}

	content := "# Cross-eco note\n" +
		"Go: `crypto/sha256.Sum256`. " +
		"Python: `numpy.linalg.solve`. " +
		"TS: `react.useState`. " +
		"Rust: `serde::Serialize`."
	note := makeNoteWithContent(100, content)
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote: %v", err)
	}
	got, ok := vault.writes[100]
	if !ok {
		t.Fatalf("vault.writes[100] missing")
	}
	if len(got) != 4 {
		t.Fatalf("len(join_keys) = %d; want 4 (one per eco); got=%v", len(got), got)
	}
	// Strict format invariant: every key MUST split into exactly 3
	// colon-separated parts via SplitN(":", 3). The plan-file canonical
	// assertion (lines 1819-1825) — pinned here against drift.
	//
	// Note SplitN with limit=3 stops splitting after the 2nd colon, so
	// trailing colons inside symbol_path (e.g., Rust's `crate::Item`) are
	// preserved verbatim in parts[2]. The lang token + version are strictly
	// colon-free; symbol_path may legitimately contain colons (Rust).
	for _, key := range got {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) != 3 {
			t.Errorf("malformed join_key %q: SplitN(:, 3) yields %d parts; want 3", key, len(parts))
			continue
		}
		// parts[0] = lang token; MUST be non-empty.
		if parts[0] == "" {
			t.Errorf("join_key %q has empty lang token", key)
		}
		// parts[1] = version; MUST be non-empty.
		if parts[1] == "" {
			t.Errorf("join_key %q has empty version", key)
		}
		// parts[2] = symbol_path; MUST be non-empty.
		if parts[2] == "" {
			t.Errorf("join_key %q has empty symbol_path", key)
		}
	}
}

// TestProcessVaultNote_CrossLink_LangTokenIsEcosystem asserts the lang token
// in each join_key equals the canonical Ecosystem string ("go", "python",
// "typescript", "rust") of the registering SymbolRef.
//
// This is the cross-link invariant: the lang token MUST be derivable from
// SymbolRef.Ecosystem via direct cast (Ecosystem is a string type).
func TestProcessVaultNote_CrossLink_LangTokenIsEcosystem(t *testing.T) {

	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
		{Ecosystem: EcoPython, SymbolPath: "numpy.linalg.solve", Version: "1.26.0"},
		{Ecosystem: EcoTypeScript, SymbolPath: "react.useState", Version: "18.2.0"},
		{Ecosystem: EcoRust, SymbolPath: "serde::Serialize", Version: "1.0"},
	}}
	vault := &recordingVault{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	content := "Go: `crypto/sha256.Sum256`. Python: `numpy.linalg.solve`. " +
		"TS: `react.useState`. Rust: `serde::Serialize`."
	note := makeNoteWithContent(101, content)
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote: %v", err)
	}
	got := vault.writes[101]
	if len(got) != 4 {
		t.Fatalf("len(join_keys) = %d; want 4; got=%v", len(got), got)
	}

	wantLangs := map[string]bool{
		string(EcoGo):         true,
		string(EcoPython):     true,
		string(EcoTypeScript): true,
		string(EcoRust):       true,
	}
	// Every join_key's lang token MUST be one of the 4 Ecosystem strings.
	seen := make(map[string]bool, 4)
	for _, key := range got {
		parts := strings.SplitN(key, ":", 3)
		if len(parts) != 3 {
			t.Fatalf("malformed join_key %q: SplitN(:, 3) yields %d parts", key, len(parts))
		}
		lang := parts[0]
		if !wantLangs[lang] {
			t.Errorf("join_key %q has lang %q; want one of {go, python, typescript, rust}", key, lang)
		}
		seen[lang] = true
	}
	// All 4 ecosystems MUST be represented (one symbol per eco was registered).
	for lang := range wantLangs {
		if !seen[lang] {
			t.Errorf("lang %q missing from join_keys = %v", lang, got)
		}
	}
}

func TestProcessVaultNote_CrossLink_VersionPreservedVerbatim(t *testing.T) {

	const wantVersion = "1.0.219-beta+commit.abc123"
	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoRust, SymbolPath: "serde::Serialize", Version: wantVersion},
	}}
	vault := &recordingVault{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	note := makeNoteWithContent(102, "Use `serde::Serialize` for serde.")
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote: %v", err)
	}
	got := vault.writes[102]
	if len(got) != 1 {
		t.Fatalf("len(join_keys) = %d; want 1; got=%v", len(got), got)
	}
	parts := strings.SplitN(got[0], ":", 3)
	if len(parts) != 3 {
		t.Fatalf("malformed join_key %q: SplitN(:, 3) yields %d parts", got[0], len(parts))
	}
	if parts[1] != wantVersion {
		t.Errorf("join_key %q version = %q; want %q (verbatim from SymbolRef.Version)", got[0], parts[1], wantVersion)
	}
}

func TestProcessVaultNote_CrossLink_SymbolPathPreservedVerbatim(t *testing.T) {
	// Rust symbol with `::` separator — the `::` MUST survive.
	const wantSym = "tokio::runtime::Runtime"
	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoRust, SymbolPath: wantSym, Version: "1.35"},
	}}
	vault := &recordingVault{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		WorkerCount:  1,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	note := makeNoteWithContent(103, "Use `tokio::runtime::Runtime` for the runtime.")
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote: %v", err)
	}
	got := vault.writes[103]
	if len(got) != 1 {
		t.Fatalf("len(join_keys) = %d; want 1; got=%v", len(got), got)
	}
	parts := strings.SplitN(got[0], ":", 3)
	if len(parts) != 3 {
		t.Fatalf("malformed join_key %q: SplitN(:, 3) yields %d parts", got[0], len(parts))
	}
	if parts[2] != wantSym {
		t.Errorf("join_key %q symbol_path = %q; want %q (verbatim from SymbolRef.SymbolPath, including ::)", got[0], parts[2], wantSym)
	}
}

// TestProcessVaultNote_CrossLink_AuditPayloadJoinKeyMatchesVault asserts the
// cross-link audit emit contract: every EvtRAGIngestJoinKey event's payload
// `join_key` field MUST appear in the vault.db row's join_keys slice. This
// closes the cross-link loop (audit chain ↔ vault.db).
func TestProcessVaultNote_CrossLink_AuditPayloadJoinKeyMatchesVault(t *testing.T) {
	sym := &recordingSymbolIndex{symbols: []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.23"},
		{Ecosystem: EcoPython, SymbolPath: "numpy.linalg.solve", Version: "1.26"},
	}}
	vault := &recordingVault{}
	chain := &recordingAuditChain{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{eco: EcoGo, kind: SrcPackageDoc}}},
		SymbolIndex:  sym,
		VaultWriter:  vault,
		AuditChain:   chain,
		WorkerCount:  1,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	content := "Two symbols: `crypto/sha256.Sum256` and `numpy.linalg.solve`."
	note := makeNoteWithContent(104, content)
	if err := ing.ProcessVaultNote(context.Background(), note); err != nil {
		t.Fatalf("ProcessVaultNote: %v", err)
	}
	vaultKeys := vault.writes[104]
	if len(vaultKeys) != 2 {
		t.Fatalf("len(vault.writes[104]) = %d; want 2; got=%v", len(vaultKeys), vaultKeys)
	}
	vaultSet := make(map[string]bool, len(vaultKeys))
	for _, k := range vaultKeys {
		vaultSet[k] = true
	}

	events := chain.snapshot()
	if len(events) != 2 {
		t.Fatalf("audit events = %d; want 2 (one per resolved join_key)", len(events))
	}
	// Every audit payload's join_key MUST appear in the vault row's join_keys.
	for i, ev := range events {
		if ev.EventType != uint32(eventlog.EvtRAGIngestJoinKey) {
			t.Errorf("event[%d] EventType = %d; want %d (EvtRAGIngestJoinKey)", i, ev.EventType, uint32(eventlog.EvtRAGIngestJoinKey))
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Fatalf("event[%d] payload unmarshal: %v", i, err)
		}
		key, _ := payload["join_key"].(string)
		if key == "" {
			t.Errorf("event[%d] payload missing join_key string field", i)
			continue
		}
		if !vaultSet[key] {
			t.Errorf("event[%d] payload join_key %q not present in vault row join_keys %v", i, key, vaultKeys)
		}
		// note_id MUST match the input note's ID.
		if v, _ := payload["note_id"].(float64); int64(v) != 104 {
			t.Errorf("event[%d] payload note_id = %v; want 104", i, payload["note_id"])
		}
	}
}

func TestProcessVaultNote_Wiring_PublicEntryPoint(t *testing.T) {
	sym := &recordingSymbolIndex{}
	ing, err := NewIngester(IngesterOptions{
		Sources:      map[Ecosystem]map[SourceType]Source{EcoGo: {SrcPackageDoc: &fakeSource{}}},
		SymbolIndex:  sym,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("NewIngester: %v", err)
	}
	// Compile-time signature pin: ProcessVaultNote MUST be a method on
	// *Ingester accepting (context.Context, Note) and returning error.
	// Method value extraction would fail at compile time on signature drift.
	var processFn func(context.Context, Note) error = ing.ProcessVaultNote
	if processFn == nil {
		t.Fatal("ProcessVaultNote method value is nil; expected non-nil")
	}

	err = processFn(context.Background(), Note{ID: 99, Content: "no symbols here"})
	if err != nil {
		t.Errorf("ProcessVaultNote (empty content) = %v; want nil", err)
	}
}
