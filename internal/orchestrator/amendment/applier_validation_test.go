package amendment_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type fakeReloadAwaiter struct {
	mu          sync.Mutex
	calls       int
	gotPath     string
	gotTO       time.Duration
	err         error
	delayBefore time.Duration
}

func (f *fakeReloadAwaiter) NotifyForceAndWait(_ context.Context, path string, timeout time.Duration) error {
	if f.delayBefore > 0 {
		time.Sleep(f.delayBefore)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.gotPath = path
	f.gotTO = timeout
	return f.err
}

type fakeSchemaParser struct {
	schema *v1.Schema
	err    error
}

func (f *fakeSchemaParser) Parse(_ []byte) (*v1.Schema, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.schema, nil
}

func makeBaselineAndCandidate() (baseline, candidate *v1.Schema) {
	baseline = &v1.Schema{}
	baseline.SchemaVersion = "1.0"
	baseline.DoctrineVersion = "1.0.0"
	baseline.AutoUpgrade = "patch"
	baseline.Workforce.MaxDepth = 3

	candidate = &v1.Schema{}
	candidate.SchemaVersion = "1.0"
	candidate.DoctrineVersion = "1.0.0"
	candidate.AutoUpgrade = "patch"
	candidate.Workforce.MaxDepth = 6
	return
}

func makeCleanCandidate(t *testing.T) (baseline, candidate *v1.Schema) {
	t.Helper()
	baseline = builtinMaxScopeForTest(t)
	candidate = builtinMaxScopeForTest(t)
	return
}

func builtinMaxScopeForTest(t *testing.T) *v1.Schema {
	t.Helper()
	s := *builtin.MaxScope()
	return &s
}

func TestApplyWithValidationRejectsLoosenAttempt(t *testing.T) {
	dir := initRepo(t)
	tomlPath := filepath.Join(dir, "zenswarm.toml")
	preContent, _ := os.ReadFile(tomlPath)
	preStat, _ := os.Stat(tomlPath)

	baseline, candidate := makeBaselineAndCandidate()
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
	})

	err := a.ApplyWithValidation(context.Background(), 20, "the-operator",
		func() *v1.Schema { return baseline },
		&fakeSchemaParser{schema: candidate})
	if err == nil {
		t.Fatalf("ApplyWithValidation accepted loosen attempt; want ErrTightenViolation")
	}
	if !errors.Is(err, doctrineerrors.ErrTightenViolation) {
		t.Errorf("err = %v, want ErrTightenViolation", err)
	}

	// invariant + invariant: filesystem MUST be byte-identical.
	postContent, _ := os.ReadFile(tomlPath)
	postStat, _ := os.Stat(tomlPath)
	if string(postContent) != string(preContent) {
		t.Errorf("zenswarm.toml mutated despite tighten-rejection")
	}
	if postStat.Size() != preStat.Size() {
		t.Errorf("zenswarm.toml size changed: %d → %d", preStat.Size(), postStat.Size())
	}

	events := em.snapshot()
	var foundReject, foundSuppressed bool
	for _, ev := range events {
		switch ev.typ {
		case eventlog.EvtDoctrineTightenViolationRejected:
			foundReject = true
			if got, _ := ev.payload["source"].(string); got != "amendment-apply" {
				t.Errorf("DoctrineTightenViolationRejected.source = %q, want amendment-apply", got)
			}
			if got, _ := ev.payload["adr_id"].(string); got != "ADR-0020" {
				t.Errorf("DoctrineTightenViolationRejected.adr_id = %q, want ADR-0020", got)
			}
		case eventlog.EvtDoctrineAmendmentSuppressed:
			foundSuppressed = true
		}
	}
	if !foundReject {
		t.Errorf("expected DoctrineTightenViolationRejected emission; got %d events", len(events))
	}
	if !foundSuppressed {
		t.Errorf("expected DoctrineAmendmentSuppressed emission for audit-trail consistency")
	}

	// Git log MUST show no new commit (only the init commit).
	out, _ := exec.Command("git", "-C", dir, "log", "--oneline").CombinedOutput()
	if strings.Count(string(out), "\n") != 1 {
		t.Errorf("expected exactly 1 commit (init), got log:\n%s", out)
	}
}

func TestApplyWithValidationDelegatesToInnerApplyOnSuccess(t *testing.T) {
	dir := initRepo(t)
	baseline, candidate := makeCleanCandidate(t)
	em := &fakeEmitter{}
	rs := &fakeReloadSignal{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      em,
		ReloadSignal: rs,
	})

	err := a.ApplyWithValidation(context.Background(), 20, "the-operator",
		func() *v1.Schema { return baseline },
		&fakeSchemaParser{schema: candidate})
	if err != nil {
		t.Fatalf("ApplyWithValidation: %v", err)
	}

	events := em.snapshot()
	var found bool
	for _, ev := range events {
		if ev.typ == eventlog.EvtDoctrineAmendmentApplied {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected DoctrineAmendmentApplied from inner Apply")
	}
	if rs.calls != 1 {
		t.Errorf("ReloadSignal.calls = %d, want 1", rs.calls)
	}
}

func TestApplyWithValidationNilBaselineLoaderReturnsError(t *testing.T) {
	dir := initRepo(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
	})
	err := a.ApplyWithValidation(context.Background(), 20, "testuser", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "nil BaselineLoader") {
		t.Errorf("err = %v, want nil BaselineLoader sentinel", err)
	}
}

func TestApplyWithValidationNilBaselineFromLoaderReturnsError(t *testing.T) {
	dir := initRepo(t)
	_, candidate := makeCleanCandidate(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
	})
	err := a.ApplyWithValidation(context.Background(), 20, "testuser",
		func() *v1.Schema { return nil },
		&fakeSchemaParser{schema: candidate})
	if err == nil || !strings.Contains(err.Error(), "BaselineLoader returned nil") {
		t.Errorf("err = %v, want BaselineLoader returned nil sentinel", err)
	}
}

func TestApplyWithValidationParserErrorPropagates(t *testing.T) {
	dir := initRepo(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
	})
	err := a.ApplyWithValidation(context.Background(), 20, "testuser",
		func() *v1.Schema { return &v1.Schema{} },
		&fakeSchemaParser{err: errors.New("parse boom")})
	if err == nil || !strings.Contains(err.Error(), "parse merged schema") {
		t.Errorf("err = %v, want parse merged schema wrapping", err)
	}
}

func TestApplyWithValidationMissingProposedADR(t *testing.T) {
	dir := initRepo(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
	})
	err := a.ApplyWithValidation(context.Background(), 9999, "testuser",
		func() *v1.Schema { return &v1.Schema{} }, nil)
	if err == nil || !strings.Contains(err.Error(), "locate ADR-9999") {
		t.Errorf("err = %v, want locate ADR-9999 wrapping", err)
	}
}

func TestApplyWithValidationDefaultSchemaParserAcceptsValidTOML(t *testing.T) {
	parser := amendment.DefaultSchemaParser()
	if parser == nil {
		t.Fatal("DefaultSchemaParser returned nil")
	}

	s, err := parser.Parse([]byte("schema_version = \"1.0\"\ndoctrine_version = \"1.0.0\"\nauto_upgrade = \"none\"\n"))
	if err != nil {
		t.Fatalf("DefaultSchemaParser parse: %v", err)
	}
	if s == nil {
		t.Fatal("DefaultSchemaParser returned nil schema")
	}
}

func makeBaselineAndSingleRuleCandidate() (baseline, candidate *v1.Schema) {
	a := *builtinMaxScopeForTestNoT()
	baseline = &a
	b := *builtinMaxScopeForTestNoT()
	candidate = &b
	candidate.Workforce.MaxDepth = baseline.Workforce.MaxDepth + 1
	return
}

func builtinMaxScopeForTestNoT() *v1.Schema {
	s := *builtin.MaxScope()
	return &s
}

func TestTightenViolationRejectedSingleRuleEmission(t *testing.T) {
	dir := initRepo(t)
	baseline, candidate := makeBaselineAndSingleRuleCandidate()
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
	})

	err := a.ApplyWithValidation(context.Background(), 20, "the-operator",
		func() *v1.Schema { return baseline },
		&fakeSchemaParser{schema: candidate})
	if err == nil {
		t.Fatalf("ApplyWithValidation accepted single-rule loosen; want ErrTightenViolation")
	}
	if !errors.Is(err, doctrineerrors.ErrTightenViolation) {
		t.Errorf("err = %v, want ErrTightenViolation", err)
	}

	events := em.snapshot()
	var rejectPayload map[string]any
	for _, ev := range events {
		if ev.typ == eventlog.EvtDoctrineTightenViolationRejected {
			rejectPayload = ev.payload
			break
		}
	}
	if rejectPayload == nil {
		t.Fatalf("expected DoctrineTightenViolationRejected emission; got %d events", len(events))
	}

	if got, _ := rejectPayload["rule_path"].(string); got != "Workforce.MaxDepth" {
		t.Errorf("rule_path = %q, want Workforce.MaxDepth", got)
	}
	if got, _ := rejectPayload["direction"].(string); got != "decrease" {
		t.Errorf("direction = %q, want decrease", got)
	}
	if got, _ := rejectPayload["attempted_value"].(string); got == "" {
		t.Errorf("attempted_value missing in payload (got empty); want populated scalar")
	}
	if got, _ := rejectPayload["baseline_value"].(string); got == "" {
		t.Errorf("baseline_value missing in payload (got empty); want populated scalar")
	}
	// Multi-rule rule_violations slice MUST be absent for single-rule case.
	if _, present := rejectPayload["rule_violations"]; present {
		t.Errorf("rule_violations key should be absent for single-rule case; got %v", rejectPayload["rule_violations"])
	}
}

func TestTightenViolationRejectedDefensiveFallbackWhenNoTypedViolations(t *testing.T) {
	dir := initRepo(t)
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
	})

	err := a.ApplyWithValidation(context.Background(), 20, "the-operator",
		func() *v1.Schema { return &v1.Schema{} },
		&fakeSchemaParser{schema: nil})
	if err == nil {
		t.Fatalf("ApplyWithValidation accepted nil candidate; want ErrTightenViolation")
	}
	if !errors.Is(err, doctrineerrors.ErrTightenViolation) {
		t.Errorf("err = %v, want ErrTightenViolation", err)
	}

	events := em.snapshot()
	var rejectPayload map[string]any
	for _, ev := range events {
		if ev.typ == eventlog.EvtDoctrineTightenViolationRejected {
			rejectPayload = ev.payload
			break
		}
	}
	if rejectPayload == nil {
		t.Fatalf("expected DoctrineTightenViolationRejected emission; got %d events", len(events))
	}

	rv, ok := rejectPayload["rule_violations"].([]map[string]any)
	if !ok {
		t.Fatalf("rule_violations key missing or wrong shape (got %T): %#v",
			rejectPayload["rule_violations"], rejectPayload)
	}
	if len(rv) != 1 {
		t.Fatalf("rule_violations len = %d, want 1 (defensive fallback)", len(rv))
	}
	gotDetail, _ := rv[0]["detail"].(string)
	if gotDetail == "" {
		t.Errorf("rule_violations[0].detail empty; want raw vErr.Error() string")
	}
	if !strings.Contains(gotDetail, "nil schema") {
		t.Errorf("rule_violations[0].detail = %q; want substring 'nil schema' (raw err)", gotDetail)
	}
	// Per-rule scalar fields MUST be absent for case 0.
	for _, k := range []string{"rule_path", "attempted_value", "baseline_value", "direction"} {
		if _, present := rejectPayload[k]; present {
			t.Errorf("payload key %q present in case-0 emission; want absent", k)
		}
	}
}

func TestApplyWithValidationMissingZenswarmToml(t *testing.T) {
	dir := initRepo(t)

	if err := os.Remove(filepath.Join(dir, "zenswarm.toml")); err != nil {
		t.Fatalf("remove zenswarm.toml: %v", err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
	})
	_, candidate := makeCleanCandidate(t)
	err := a.ApplyWithValidation(context.Background(), 20, "the-operator",
		func() *v1.Schema { return &v1.Schema{} },
		&fakeSchemaParser{schema: candidate})
	if err == nil {
		t.Fatal("ApplyWithValidation succeeded with missing zenswarm.toml; want read failure")
	}
	if !strings.Contains(err.Error(), "read zenswarm.toml") {
		t.Errorf("err = %v, want substring 'read zenswarm.toml'", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want errors.Is(os.ErrNotExist)", err)
	}
}

func TestApplyWithValidationADRWithNoTomlBlock(t *testing.T) {
	dir := initRepo(t)

	if err := os.WriteFile(filepath.Join(dir, "docs", "decisions", "proposed", "0020-x.md"),
		[]byte("# ADR 0020\nNo TOML fenced block here at all.\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	em := &fakeEmitter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
	})
	_, candidate := makeCleanCandidate(t)
	err := a.ApplyWithValidation(context.Background(), 20, "the-operator",
		func() *v1.Schema { return &v1.Schema{} },
		&fakeSchemaParser{schema: candidate})
	if err == nil {
		t.Fatal("ApplyWithValidation succeeded on no-toml-block ADR; want extract failure")
	}
	if !errors.Is(err, amendment.ErrNoTOMLBlock) {
		t.Errorf("err = %v, want errors.Is(amendment.ErrNoTOMLBlock)", err)
	}

	if !strings.Contains(err.Error(), "ApplyWithValidation: ADR-0020") {
		t.Errorf("err = %v, want substring 'ApplyWithValidation: ADR-0020' wrapping", err)
	}
}

func TestTightenViolationPayloadOptionalScalarFields(t *testing.T) {
	cases := []struct {
		name        string
		event       eventlog.DoctrineTightenViolationRejected
		wantKeys    []string
		notWantKeys []string
	}{
		{
			name: "with project_id only",
			event: eventlog.DoctrineTightenViolationRejected{
				Path:      "zenswarm.toml",
				Source:    "amendment-apply",
				ProjectID: "zen-swarm",
			},
			wantKeys:    []string{"path", "source", "project_id"},
			notWantKeys: []string{"doctrine_name", "rule_path", "rule_violations"},
		},
		{
			name: "with doctrine_name only",
			event: eventlog.DoctrineTightenViolationRejected{
				Path:         "zenswarm.toml",
				Source:       "operator-edit",
				DoctrineName: "max-scope",
			},
			wantKeys:    []string{"path", "source", "doctrine_name"},
			notWantKeys: []string{"project_id", "rule_path", "rule_violations"},
		},
		{
			name: "with both project_id and doctrine_name",
			event: eventlog.DoctrineTightenViolationRejected{
				Path:         "zenswarm.toml",
				Source:       "reload-watcher",
				ProjectID:    "zen-swarm",
				DoctrineName: "default",
				RuleViolations: []eventlog.DoctrineTightenViolation{{
					RulePath:  "Workforce.MaxDepth",
					Direction: "decrease",
				}},
			},
			wantKeys:    []string{"path", "source", "project_id", "doctrine_name", "rule_violations"},
			notWantKeys: []string{"rule_path", "attempted_value"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := amendment.TightenViolationPayloadForTest(tc.event)
			for _, k := range tc.wantKeys {
				if _, ok := payload[k]; !ok {
					t.Errorf("payload missing key %q (got keys=%v)", k, mapKeys(payload))
				}
			}
			for _, k := range tc.notWantKeys {
				if _, ok := payload[k]; ok {
					t.Errorf("payload should NOT contain key %q for empty source field; got %v",
						k, payload[k])
				}
			}
		})
	}
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestWalkHandlesPlainErrorWithoutWrapping(t *testing.T) {
	plain := errors.New("flat error")
	var visited []error
	amendment.WalkForTest(plain, func(e error) { visited = append(visited, e) })
	if len(visited) != 1 {
		t.Errorf("walk should visit single-leaf error once; got %d visits", len(visited))
	}
	if visited[0] != plain {
		t.Errorf("walk should visit the original error; got %v", visited[0])
	}

	visited = nil
	amendment.WalkForTest(nil, func(e error) { visited = append(visited, e) })
	if len(visited) != 0 {
		t.Errorf("walk(nil) should not invoke fn; got %d visits", len(visited))
	}
}

func TestApplyWithValidationSyncReloadWaitHappyPath(t *testing.T) {
	dir := initRepo(t)
	baseline, candidate := makeCleanCandidate(t)
	em := &fakeEmitter{}
	awaiter := &fakeReloadAwaiter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:      dir,
		Validator:     &fakeValidator{},
		Emitter:       em,
		ReloadSignal:  &fakeReloadSignal{},
		ReloadAwaiter: awaiter,
	})

	err := a.ApplyWithValidation(context.Background(), 20, "the-operator",
		func() *v1.Schema { return baseline },
		&fakeSchemaParser{schema: candidate})
	if err != nil {
		t.Fatalf("ApplyWithValidation: %v", err)
	}

	awaiter.mu.Lock()
	defer awaiter.mu.Unlock()
	if awaiter.calls != 1 {
		t.Errorf("ReloadAwaiter calls = %d, want 1", awaiter.calls)
	}
	wantPath := filepath.Join(dir, "zenswarm.toml")
	if awaiter.gotPath != wantPath {
		t.Errorf("ReloadAwaiter path = %q, want %q", awaiter.gotPath, wantPath)
	}
	if awaiter.gotTO != 5*time.Second {
		t.Errorf("ReloadAwaiter timeout = %v, want 5s default", awaiter.gotTO)
	}

	// No DoctrineWatcherStalled MUST be present in the emission stream.
	for _, ev := range em.snapshot() {
		if ev.typ == eventlog.EvtDoctrineWatcherStalled {
			t.Errorf("unexpected DoctrineWatcherStalled emission on happy path: payload=%v", ev.payload)
		}
	}
}

func TestApplyWithValidationSyncReloadWaitStallEmitsTelemetry(t *testing.T) {
	dir := initRepo(t)
	baseline, candidate := makeCleanCandidate(t)
	em := &fakeEmitter{}
	stallErr := context.DeadlineExceeded
	awaiter := &fakeReloadAwaiter{err: stallErr}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:          dir,
		Validator:         &fakeValidator{},
		Emitter:           em,
		ReloadSignal:      &fakeReloadSignal{},
		ReloadAwaiter:     awaiter,
		ReloadWaitTimeout: 100 * time.Millisecond,
	})

	err := a.ApplyWithValidation(context.Background(), 20, "the-operator",
		func() *v1.Schema { return baseline },
		&fakeSchemaParser{schema: candidate})
	if err != nil {
		t.Fatalf("ApplyWithValidation: %v (apply itself MUST succeed even when reload-wait stalls)", err)
	}

	awaiter.mu.Lock()
	if awaiter.gotTO != 100*time.Millisecond {
		t.Errorf("ReloadAwaiter timeout = %v, want 100ms (honors ReloadWaitTimeout config)", awaiter.gotTO)
	}
	awaiter.mu.Unlock()

	// DoctrineWatcherStalled MUST be present with the expected payload
	// fields (path = zenswarm.toml, stall_reason = error string,
	// source = amendment-apply, adr_id = ADR-0020).
	var stalled map[string]any
	for _, ev := range em.snapshot() {
		if ev.typ == eventlog.EvtDoctrineWatcherStalled {
			stalled = ev.payload
			break
		}
	}
	if stalled == nil {
		t.Fatalf("expected DoctrineWatcherStalled emission on awaiter stall; events=%v", em.snapshot())
	}
	wantPath := filepath.Join(dir, "zenswarm.toml")
	if got, _ := stalled["path"].(string); got != wantPath {
		t.Errorf("stalled.path = %q, want %q", got, wantPath)
	}
	if got, _ := stalled["source"].(string); got != "amendment-apply" {
		t.Errorf("stalled.source = %q, want amendment-apply", got)
	}
	if got, _ := stalled["adr_id"].(string); got != "ADR-0020" {
		t.Errorf("stalled.adr_id = %q, want ADR-0020", got)
	}
	if got, _ := stalled["stall_reason"].(string); !strings.Contains(got, "context deadline exceeded") {
		t.Errorf("stalled.stall_reason = %q, want substring 'context deadline exceeded'", got)
	}

	// Inner Apply MUST have emitted DoctrineAmendmentApplied (the apply
	// itself succeeded — stall is post-apply visibility only).
	var foundApplied bool
	for _, ev := range em.snapshot() {
		if ev.typ == eventlog.EvtDoctrineAmendmentApplied {
			foundApplied = true
			break
		}
	}
	if !foundApplied {
		t.Errorf("expected DoctrineAmendmentApplied from inner Apply (stall is post-success telemetry)")
	}
}

// TestApplyWithValidationFireAndForgetWhenAwaiterNil verifies
// backward-compatibility: when ReloadAwaiter is nil,
// ApplyWithValidation falls through to the inner Apply's existing
// fire-and-forget ReloadSignal.Reload(ctx) path. NO DoctrineWatcherStalled
// MUST be emitted (the awaiter codepath is short-circuited entirely).
func TestApplyWithValidationFireAndForgetWhenAwaiterNil(t *testing.T) {
	dir := initRepo(t)
	baseline, candidate := makeCleanCandidate(t)
	em := &fakeEmitter{}
	rs := &fakeReloadSignal{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:     dir,
		Validator:    &fakeValidator{},
		Emitter:      em,
		ReloadSignal: rs,
	})

	err := a.ApplyWithValidation(context.Background(), 20, "the-operator",
		func() *v1.Schema { return baseline },
		&fakeSchemaParser{schema: candidate})
	if err != nil {
		t.Fatalf("ApplyWithValidation: %v", err)
	}

	// inner Apply's fire-and-forget reload signal MUST have been invoked
	// exactly once.
	if rs.calls != 1 {
		t.Errorf("ReloadSignal.calls = %d, want 1 (Phase H legacy fire-and-forget path)", rs.calls)
	}
	for _, ev := range em.snapshot() {
		if ev.typ == eventlog.EvtDoctrineWatcherStalled {
			t.Errorf("unexpected DoctrineWatcherStalled when ReloadAwaiter is nil; payload=%v", ev.payload)
		}
	}
}

func TestApplyWithValidationReloadWaitTimeoutDefaultsTo5s(t *testing.T) {
	dir := initRepo(t)
	baseline, candidate := makeCleanCandidate(t)
	em := &fakeEmitter{}
	awaiter := &fakeReloadAwaiter{}
	a := amendment.NewApplier(amendment.ApplierConfig{
		RepoRoot:      dir,
		Validator:     &fakeValidator{},
		Emitter:       em,
		ReloadSignal:  &fakeReloadSignal{},
		ReloadAwaiter: awaiter,
	})

	err := a.ApplyWithValidation(context.Background(), 20, "the-operator",
		func() *v1.Schema { return baseline },
		&fakeSchemaParser{schema: candidate})
	if err != nil {
		t.Fatalf("ApplyWithValidation: %v", err)
	}
	awaiter.mu.Lock()
	defer awaiter.mu.Unlock()
	if awaiter.gotTO != 5*time.Second {
		t.Errorf("default ReloadWaitTimeout = %v, want 5s (zero-value fallback)", awaiter.gotTO)
	}
}
