// SPDX-License-Identifier: MIT
package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestMainCompositionRootWiresPlan9ADRAdapter(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile(main.go): %v", err)
	}
	text := string(raw)
	newADRAt := strings.Index(text, "plan9adapter.NewADRAdapter(")
	setAt := strings.Index(text, "srv.SetPlan9Adapters(plan9)")
	if newADRAt < 0 {
		t.Fatal("main.go does not construct plan9adapter.NewADRAdapter before SetPlan9Adapters")
	}
	if setAt < 0 {
		t.Fatal("main.go does not call srv.SetPlan9Adapters(plan9)")
	}
	if newADRAt > setAt {
		t.Fatalf("plan9adapter.NewADRAdapter appears after SetPlan9Adapters (%d > %d)", newADRAt, setAt)
	}
	if !regexp.MustCompile(`ADR:\s+adrAdapter`).MatchString(text) {
		t.Fatal("Plan9Adapters does not receive the constructed ADR adapter")
	}
	if strings.Contains(text, "Plan 9 Phase H wired with nil substrates") {
		t.Fatal("main.go still logs Plan 9 as fully nil-wired")
	}
}

func TestMainCompositionRootWiresPlan9StateAdapter(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile(main.go): %v", err)
	}
	text := string(raw)
	newStateAt := strings.Index(text, "plan9adapter.NewStateAdapter(")
	setAt := strings.Index(text, "srv.SetPlan9Adapters(plan9)")
	if newStateAt < 0 {
		t.Fatal("main.go does not construct plan9adapter.NewStateAdapter before SetPlan9Adapters")
	}
	if setAt < 0 {
		t.Fatal("main.go does not call srv.SetPlan9Adapters(plan9)")
	}
	if newStateAt > setAt {
		t.Fatalf("plan9adapter.NewStateAdapter appears after SetPlan9Adapters (%d > %d)", newStateAt, setAt)
	}
	if !regexp.MustCompile(`State:\s+stateAdapter`).MatchString(text) {
		t.Fatal("Plan9Adapters does not receive the constructed State adapter")
	}
}

func TestMainCompositionRootWiresPlan9ResearchAdapter(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile(main.go): %v", err)
	}
	text := string(raw)
	openAt := strings.Index(text, "cache.Open(ctx, filepath.Join(dataRoot, \"global\", \"research_cache.db\"))")
	newResearchAt := strings.Index(text, "plan9adapter.NewResearchAdapter(")
	setAt := strings.Index(text, "srv.SetPlan9Adapters(plan9)")
	if openAt < 0 {
		t.Fatal("main.go does not open the Plan 9 research_cache.db before SetPlan9Adapters")
	}
	if newResearchAt < 0 {
		t.Fatal("main.go does not construct plan9adapter.NewResearchAdapter before SetPlan9Adapters")
	}
	if setAt < 0 {
		t.Fatal("main.go does not call srv.SetPlan9Adapters(plan9)")
	}
	if openAt > setAt || newResearchAt > setAt {
		t.Fatalf("research adapter wiring appears after SetPlan9Adapters (open=%d new=%d set=%d)", openAt, newResearchAt, setAt)
	}
	if !regexp.MustCompile(`Research:\s+researchAdapter`).MatchString(text) {
		t.Fatal("Plan9Adapters does not receive the constructed Research adapter")
	}
}

func TestMainCompositionRootWiresPlan9KnowledgeAdapter(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile(main.go): %v", err)
	}
	text := string(raw)
	openAt := strings.Index(text, "aggregator.Open(ctx, filepath.Join(dataRoot, \"global\", \"aggregator.db\"))")
	initAt := strings.Index(text, "aggregator.Init(ctx, knowledgeDB)")
	newKnowledgeAt := strings.Index(text, "plan9adapter.NewKnowledgeAdapter(")
	setAt := strings.Index(text, "srv.SetPlan9Adapters(plan9)")
	if openAt < 0 {
		t.Fatal("main.go does not open the Plan 9 aggregator.db before SetPlan9Adapters")
	}
	if initAt < 0 {
		t.Fatal("main.go does not initialize the Plan 9 aggregator.db before SetPlan9Adapters")
	}
	if newKnowledgeAt < 0 {
		t.Fatal("main.go does not construct plan9adapter.NewKnowledgeAdapter before SetPlan9Adapters")
	}
	if setAt < 0 {
		t.Fatal("main.go does not call srv.SetPlan9Adapters(plan9)")
	}
	if openAt > setAt || initAt > setAt || newKnowledgeAt > setAt {
		t.Fatalf("knowledge adapter wiring appears after SetPlan9Adapters (open=%d init=%d new=%d set=%d)", openAt, initAt, newKnowledgeAt, setAt)
	}
	if !regexp.MustCompile(`Knowledge:\s+knowledgeAdapter`).MatchString(text) {
		t.Fatal("Plan9Adapters does not receive the constructed Knowledge adapter")
	}
}

func TestMainCompositionRootWiresPlan9AuditAdapter(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile(main.go): %v", err)
	}
	text := string(raw)
	newAuditAt := strings.Index(text, "plan9adapter.NewAuditAdapter(")
	setAt := strings.Index(text, "srv.SetPlan9Adapters(plan9)")
	if newAuditAt < 0 {
		t.Fatal("main.go does not construct plan9adapter.NewAuditAdapter before SetPlan9Adapters")
	}
	if setAt < 0 {
		t.Fatal("main.go does not call srv.SetPlan9Adapters(plan9)")
	}
	if newAuditAt > setAt {
		t.Fatalf("plan9adapter.NewAuditAdapter appears after SetPlan9Adapters (%d > %d)", newAuditAt, setAt)
	}
	if !regexp.MustCompile(`Audit:\s+auditAdapter`).MatchString(text) {
		t.Fatal("Plan9Adapters does not receive the constructed Audit adapter")
	}
	if !strings.Contains(text, "ColdArchiveDownloader: litestream.NewColdArchiveDownloader(") || !strings.Contains(text, "s3CredsStore") {
		t.Fatal("Audit adapter is not wired with the production cold-archive downloader")
	}
	if strings.Contains(text, "AuditCtxP9 facade not yet implemented") {
		t.Fatal("main.go still logs AuditCtxP9 as not implemented")
	}
}

func TestMainCompositionRootWiresLitestreamProjectEnumeration(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile(main.go): %v", err)
	}
	text := string(raw)
	listAt := strings.Index(text, "projectStoreAdapter.List(ctx, false)")
	startAt := strings.Index(text, "litestreamLifecycle.StartAllProjects(ctx, knownProjectIDs)")
	if listAt < 0 {
		t.Fatal("main.go does not enumerate active projects from projectStoreAdapter before Litestream startup")
	}
	if startAt < 0 {
		t.Fatal("main.go does not call litestreamLifecycle.StartAllProjects")
	}
	if listAt > startAt {
		t.Fatalf("project enumeration appears after Litestream StartAllProjects (%d > %d)", listAt, startAt)
	}
	if strings.Contains(text, "knownProjectIDs := []string{}") {
		t.Fatal("main.go still boots Litestream with an unconditional empty project roster")
	}
	if strings.Contains(text, "project-enumeration follow-up") {
		t.Fatal("main.go still documents project enumeration as a future follow-up")
	}
}

func TestMainCompositionRootLogsPlan9AdapterStatusFromConstructedValues(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile(main.go): %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `auditStatus := "nil`) || !strings.Contains(text, `adrStatus := "nil`) || !strings.Contains(text, `stateStatus := "nil`) ||
		!strings.Contains(text, `knowledgeStatus := "nil`) {
		t.Fatal("main.go does not initialize Plan9 adapter log status to nil before successful construction")
	}
	if !regexp.MustCompile(`auditStatus\s+=\s+"live`).MatchString(text) {
		t.Fatal("main.go does not switch Audit log status to live only after construction")
	}
	if !regexp.MustCompile(`adrStatus\s+=\s+"live`).MatchString(text) {
		t.Fatal("main.go does not switch ADR log status to live only after construction")
	}
	if !regexp.MustCompile(`stateStatus\s+=\s+"live`).MatchString(text) {
		t.Fatal("main.go does not switch State log status to live only after construction")
	}
	if !regexp.MustCompile(`knowledgeStatus\s+=\s+"live`).MatchString(text) {
		t.Fatal("main.go does not switch Knowledge log status to live only after construction")
	}
	if strings.Contains(text, `"adr", "live — ADRCtx`) || strings.Contains(text, `"state", "live — StateService`) {
		t.Fatal("main.go still logs Plan9 ADR/State as unconditionally live")
	}
}
