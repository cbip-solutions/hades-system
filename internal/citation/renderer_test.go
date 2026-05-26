// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// internal/citation/renderer_test.go — Plan 11 Phase D Task D-3.
//
// Tests for the Renderer interface + Registry + Dispatch routing. Plan 12
// platform renderers register against Registry; Phase D substrate ships
// MarkdownFallback only.
package citation_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

type fakeRenderer struct{ platform string }

func (f *fakeRenderer) Render(env *citation.Envelope, sess citation.SessionContext) (string, error) {
	return "platform=" + f.platform + " env.id=" + string(env.ID), nil
}

func (f *fakeRenderer) Platform() string { return f.platform }

func newTestEnv() *citation.Envelope {
	return &citation.Envelope{
		ID:           "c-test0001",
		Type:         citation.CitationTypeKGNode,
		Source:       citation.SourceCaronteQuery,
		Lane:         citation.LaneSemantic,
		AuditEventID: "evt-test",
		Confidence:   0.5,
		RRFScore:     0.01,
		RRFRank:      0,
		ProjectID:    "p",
		Payload:      "x",
	}
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	reg := citation.NewRegistry()
	r := &fakeRenderer{platform: "ink"}
	reg.Register(r)

	got, ok := reg.Lookup("ink")
	if !ok {
		t.Fatal("Lookup ink: not found")
	}
	if got.Platform() != "ink" {
		t.Errorf("Platform: want ink got %s", got.Platform())
	}
}

func TestRegistryNoDuplicateRegistration(t *testing.T) {
	reg := citation.NewRegistry()
	reg.Register(&fakeRenderer{platform: "ink"})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate Register")
		}
	}()
	reg.Register(&fakeRenderer{platform: "ink"})
}

func TestRegistryPanicsOnEmptyPlatform(t *testing.T) {
	reg := citation.NewRegistry()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on empty platform name")
		}
	}()
	reg.Register(&fakeRenderer{platform: ""})
}

func TestDispatchExactMatch(t *testing.T) {
	reg := citation.NewRegistry()
	reg.Register(&fakeRenderer{platform: "ink"})
	reg.Register(citation.NewMarkdownFallback(nil))

	env := newTestEnv()
	sess := citation.SessionContext{Doctrine: "max-scope", Platform: "ink", Now: time.Now()}
	got, err := reg.Dispatch(env, sess)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.Contains(got, "platform=ink") {
		t.Errorf("Dispatch did not select ink renderer: %s", got)
	}
}

func TestDispatchFallbackToMarkdown(t *testing.T) {
	reg := citation.NewRegistry()
	reg.Register(citation.NewMarkdownFallback(nil))

	env := newTestEnv()
	sess := citation.SessionContext{Doctrine: "max-scope", Platform: "ink", Now: time.Now()}
	got, err := reg.Dispatch(env, sess)
	if err != nil {
		t.Fatalf("Dispatch fallback: %v", err)
	}

	if !strings.Contains(got, "[^") {
		t.Errorf("Dispatch did not fall back to markdown footnote: %s", got)
	}
}

func TestDispatchEmptyPlatformUsesMarkdown(t *testing.T) {
	reg := citation.NewRegistry()
	reg.Register(citation.NewMarkdownFallback(nil))

	env := newTestEnv()
	sess := citation.SessionContext{Doctrine: "max-scope", Platform: "", Now: time.Now()}
	got, err := reg.Dispatch(env, sess)
	if err != nil {
		t.Fatalf("Dispatch empty-platform: %v", err)
	}
	if !strings.Contains(got, "[^") {
		t.Errorf("Dispatch empty-platform did not use markdown: %s", got)
	}
}

func TestDispatchNoMarkdownFallbackErrors(t *testing.T) {
	reg := citation.NewRegistry()

	env := newTestEnv()
	sess := citation.SessionContext{Doctrine: "max-scope", Platform: "ink", Now: time.Now()}
	_, err := reg.Dispatch(env, sess)
	if err == nil {
		t.Error("expected error when no renderer registered")
	}
}

func TestDispatchNilEnvelopeErrors(t *testing.T) {
	reg := citation.NewRegistry()
	reg.Register(citation.NewMarkdownFallback(nil))

	_, err := reg.Dispatch(nil, citation.SessionContext{Platform: "markdown"})
	if err == nil {
		t.Error("expected error on nil envelope")
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	reg := citation.NewRegistry()
	reg.Register(citation.NewMarkdownFallback(nil))

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			env := newTestEnv()
			sess := citation.SessionContext{Platform: "markdown", Now: time.Now()}
			_, _ = reg.Dispatch(env, sess)
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ {
		_, _ = reg.Lookup("markdown")
	}
	<-done
}

type recursiveRenderer struct {
	platform string
	reg      *citation.Registry

	inner chan citation.Renderer

	gateRenderEntered chan struct{}

	gateRegisterQueued chan struct{}
}

func (r *recursiveRenderer) Render(env *citation.Envelope, sess citation.SessionContext) (string, error) {

	close(r.gateRenderEntered)

	<-r.gateRegisterQueued

	inner, _ := r.reg.Lookup("markdown")
	r.inner <- inner
	return "platform=" + r.platform + " inner_lookup_ok", nil
}

func (r *recursiveRenderer) Platform() string { return r.platform }

func TestDispatchSnapshotsRendererPointerBeforeRender(t *testing.T) {
	reg := citation.NewRegistry()
	reg.Register(citation.NewMarkdownFallback(nil))

	rec := &recursiveRenderer{
		platform:           "recursive-test",
		reg:                reg,
		inner:              make(chan citation.Renderer, 1),
		gateRenderEntered:  make(chan struct{}),
		gateRegisterQueued: make(chan struct{}),
	}
	reg.Register(rec)

	env := newTestEnv()
	sess := citation.SessionContext{
		Doctrine: "max-scope",
		Platform: "recursive-test",
		Now:      time.Now(),
	}

	dispatchDone := make(chan error, 1)
	var got string
	go func() {
		var err error
		got, err = reg.Dispatch(env, sess)
		dispatchDone <- err
	}()

	<-rec.gateRenderEntered
	registerDone := make(chan struct{})
	go func() {
		reg.Register(&fakeRenderer{platform: "writer-queue-trigger"})
		close(registerDone)
	}()

	time.Sleep(50 * time.Millisecond)
	close(rec.gateRegisterQueued)

	select {
	case err := <-dispatchDone:
		if err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Dispatch deadlocked: re-entrant Lookup blocked on parent RLock " +
			"with queued writer (regression of I-2 snapshot-then-release)")
	}

	select {
	case <-registerDone:
	case <-time.After(1 * time.Second):
		t.Fatal("queued Register never unblocked")
	}

	select {
	case inner := <-rec.inner:
		if inner == nil {
			t.Fatal("recursive Lookup returned nil renderer (markdown should be registered)")
		}
		if inner.Platform() != "markdown" {
			t.Errorf("recursive Lookup Platform: want markdown got %s", inner.Platform())
		}
	case <-time.After(1 * time.Second):
		t.Fatal("recursive renderer never observed inner Lookup result")
	}

	if !strings.Contains(got, "inner_lookup_ok") {
		t.Errorf("Dispatch output: want substring inner_lookup_ok got %q", got)
	}
}

func TestDispatchSnapshotSurvivesConcurrentRegister(t *testing.T) {
	reg := citation.NewRegistry()
	reg.Register(citation.NewMarkdownFallback(nil))

	hold := make(chan struct{})
	release := make(chan struct{})
	slow := &slowRenderer{
		platform: "slow",
		holding:  hold,
		release:  release,
	}
	reg.Register(slow)

	dispatchDone := make(chan error, 1)
	go func() {
		_, err := reg.Dispatch(newTestEnv(), citation.SessionContext{
			Platform: "slow",
			Now:      time.Now(),
		})
		dispatchDone <- err
	}()

	<-hold

	registerDone := make(chan struct{})
	go func() {
		reg.Register(&fakeRenderer{platform: "fresh"})
		close(registerDone)
	}()

	select {
	case <-registerDone:

	case <-time.After(2 * time.Second):
		close(release)
		<-dispatchDone
		t.Fatal("concurrent Register blocked while Dispatch was rendering " +
			"(regression of I-2 snapshot-then-release)")
	}

	close(release)
	if err := <-dispatchDone; err != nil {
		t.Errorf("Dispatch: %v", err)
	}

	if _, ok := reg.Lookup("fresh"); !ok {
		t.Error("fresh platform never landed in registry post-Register")
	}
}

type slowRenderer struct {
	platform string
	holding  chan struct{}
	release  chan struct{}
}

func (s *slowRenderer) Render(_ *citation.Envelope, _ citation.SessionContext) (string, error) {
	close(s.holding)
	<-s.release
	return "slow-rendered", nil
}

func (s *slowRenderer) Platform() string { return s.platform }
