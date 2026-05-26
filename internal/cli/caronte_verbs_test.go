package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type fakeCaronteVerbsClient struct {
	why      *client.WhyResponse
	risk     *client.RiskResponse
	cochange *client.CoChangeResponse
	impl     *client.ImplResponse
	err      error
	gotRisk  client.RiskRequest
}

func (f *fakeCaronteVerbsClient) Why(_ context.Context, _ client.WhyRequest) (*client.WhyResponse, error) {
	return f.why, f.err
}
func (f *fakeCaronteVerbsClient) Risk(_ context.Context, req client.RiskRequest) (*client.RiskResponse, error) {
	f.gotRisk = req
	return f.risk, f.err
}
func (f *fakeCaronteVerbsClient) CoChange(_ context.Context, _ client.CoChangeRequest) (*client.CoChangeResponse, error) {
	return f.cochange, f.err
}
func (f *fakeCaronteVerbsClient) Impl(_ context.Context, _ client.ImplRequest) (*client.ImplResponse, error) {
	return f.impl, f.err
}

func TestRunWhyTextRendersADRsAndLore(t *testing.T) {
	c := &fakeCaronteVerbsClient{why: &client.WhyResponse{
		Subject:      "pkg.M",
		LinkedADRs:   []client.WhyLinkedADR{{ADRID: "ADR-0081", ADRTitle: "lanes", LinkKind: "explicit_ref", Confidence: 0.9, Stale: true}},
		LoreTrailers: []client.WhyLoreEntry{{CommitSHA: "abc1234", TrailerKind: "constraint", Body: "no subprocess"}},
	}}
	var buf bytes.Buffer
	if err := RunWhy(context.Background(), c, WhyFlags{Symbol: "pkg.M", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunWhy: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "ADR-0081") || !strings.Contains(out, "stale") {
		t.Errorf("why text missing ADR/stale: %s", out)
	}
	if !strings.Contains(out, "no subprocess") {
		t.Errorf("why text missing lore body: %s", out)
	}
}

func TestRunWhyRequiresSymbol(t *testing.T) {
	c := &fakeCaronteVerbsClient{}
	err := RunWhy(context.Background(), c, WhyFlags{Format: "text"}, &bytes.Buffer{})
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("empty symbol → err %v; want recoverable", err)
	}
}

func TestRunWhyJSON(t *testing.T) {
	c := &fakeCaronteVerbsClient{why: &client.WhyResponse{Subject: "pkg.M"}}
	var buf bytes.Buffer
	if err := RunWhy(context.Background(), c, WhyFlags{Symbol: "pkg.M", Format: "json"}, &buf); err != nil {
		t.Fatalf("RunWhy json: %v", err)
	}
	if !strings.Contains(buf.String(), `"subject": "pkg.M"`) {
		t.Errorf("json out = %s", buf.String())
	}
}

func TestRunRiskClassifiesArgsAndRendersLevel(t *testing.T) {
	c := &fakeCaronteVerbsClient{risk: &client.RiskResponse{Level: "high", Score: 0.72, TopAffected: []string{"pkg.A"}}}
	var buf bytes.Buffer

	if err := RunRisk(context.Background(), c, RiskFlags{Args: []string{"a/b.go", "pkg.Sym"}, Format: "text"}, &buf); err != nil {
		t.Fatalf("RunRisk: %v", err)
	}
	if len(c.gotRisk.ChangedFiles) != 1 || c.gotRisk.ChangedFiles[0] != "a/b.go" {
		t.Errorf("ChangedFiles = %v; want [a/b.go]", c.gotRisk.ChangedFiles)
	}
	if len(c.gotRisk.ChangedSymbols) != 1 || c.gotRisk.ChangedSymbols[0] != "pkg.Sym" {
		t.Errorf("ChangedSymbols = %v; want [pkg.Sym]", c.gotRisk.ChangedSymbols)
	}
	if !strings.Contains(buf.String(), "high") {
		t.Errorf("risk text missing level: %s", buf.String())
	}
}

func TestRunRiskRequiresArg(t *testing.T) {
	c := &fakeCaronteVerbsClient{}
	err := RunRisk(context.Background(), c, RiskFlags{Format: "text"}, &bytes.Buffer{})
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("no args → err %v; want recoverable", err)
	}
}

func TestRunRiskExplicitFlagsOverrideHeuristic(t *testing.T) {
	c := &fakeCaronteVerbsClient{risk: &client.RiskResponse{Level: "low"}}

	if err := RunRisk(context.Background(), c, RiskFlags{Symbols: []string{"weird/sym"}, Format: "text"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunRisk: %v", err)
	}
	if len(c.gotRisk.ChangedSymbols) != 1 || c.gotRisk.ChangedSymbols[0] != "weird/sym" {
		t.Errorf("explicit --symbol ignored: ChangedSymbols = %v", c.gotRisk.ChangedSymbols)
	}
	if len(c.gotRisk.ChangedFiles) != 0 {
		t.Errorf("ChangedFiles should be empty with explicit --symbol: %v", c.gotRisk.ChangedFiles)
	}
}

func TestRunCochangeText(t *testing.T) {
	c := &fakeCaronteVerbsClient{cochange: &client.CoChangeResponse{
		File:  "a.go",
		Peers: []client.CoChangePeerDTO{{Path: "b.go", CouplingPercent: 60, SharedRevs: 6, WindowDays: 90}},
	}}
	var buf bytes.Buffer
	if err := RunCochange(context.Background(), c, CochangeFlags{File: "a.go", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunCochange: %v", err)
	}
	if !strings.Contains(buf.String(), "b.go") || !strings.Contains(buf.String(), "60") {
		t.Errorf("cochange text = %s", buf.String())
	}
}

func TestRunCochangeRequiresFile(t *testing.T) {
	c := &fakeCaronteVerbsClient{}
	if err := RunCochange(context.Background(), c, CochangeFlags{Format: "text"}, &bytes.Buffer{}); !errors.Is(err, ErrRecoverable) {
		t.Errorf("empty file → err %v; want recoverable", err)
	}
}

func TestRunImplText(t *testing.T) {
	c := &fakeCaronteVerbsClient{impl: &client.ImplResponse{
		Interface:       "io.Writer",
		Implementations: []client.ImplDTO{{InterfaceID: "io.Writer", ImplID: "bytes.Buffer", Confidence: "exact_vta", Reachable: true}},
	}}
	var buf bytes.Buffer
	if err := RunImpl(context.Background(), c, ImplFlags{Interface: "io.Writer", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunImpl: %v", err)
	}
	if !strings.Contains(buf.String(), "bytes.Buffer") || !strings.Contains(buf.String(), "exact_vta") {
		t.Errorf("impl text = %s", buf.String())
	}
}

func TestRunImplRequiresInterface(t *testing.T) {
	c := &fakeCaronteVerbsClient{}
	if err := RunImpl(context.Background(), c, ImplFlags{Format: "text"}, &bytes.Buffer{}); !errors.Is(err, ErrRecoverable) {
		t.Errorf("empty interface → err %v; want recoverable", err)
	}
}

func TestRunWhyFormatValidated(t *testing.T) {
	c := &fakeCaronteVerbsClient{why: &client.WhyResponse{Subject: "x"}}
	if err := RunWhy(context.Background(), c, WhyFlags{Symbol: "x", Format: "yaml"}, &bytes.Buffer{}); !errors.Is(err, ErrRecoverable) {
		t.Errorf("invalid format → err %v; want recoverable", err)
	}
}

func TestRunWhyDegradedAndNoLinks(t *testing.T) {
	c := &fakeCaronteVerbsClient{why: &client.WhyResponse{Subject: "pkg.X", Degraded: true}}
	var buf bytes.Buffer
	if err := RunWhy(context.Background(), c, WhyFlags{Symbol: "pkg.X", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunWhy: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "degraded") {
		t.Errorf("want degraded warning: %s", out)
	}
	if !strings.Contains(out, "undocumented") {
		t.Errorf("want undocumented fallback: %s", out)
	}
}

func TestRunWhyPassages(t *testing.T) {
	c := &fakeCaronteVerbsClient{why: &client.WhyResponse{
		Subject:          "pkg.M",
		SemanticPassages: []client.WhySemanticPassage{{SourceID: "spec-1", SourceKind: "spec", Text: "the engine runs plans", Score: 0.8}},
	}}
	var buf bytes.Buffer
	if err := RunWhy(context.Background(), c, WhyFlags{Symbol: "pkg.M", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunWhy: %v", err)
	}
	if !strings.Contains(buf.String(), "spec-1") {
		t.Errorf("passages missing source id: %s", buf.String())
	}
}

func TestRunWhyClientError(t *testing.T) {
	c := &fakeCaronteVerbsClient{err: errors.New("net error")}
	err := RunWhy(context.Background(), c, WhyFlags{Symbol: "x", Format: "text"}, &bytes.Buffer{})
	if err == nil {
		t.Error("expected error from client")
	}
}

func TestRunRiskJSON(t *testing.T) {
	c := &fakeCaronteVerbsClient{risk: &client.RiskResponse{Level: "medium", Score: 0.5}}
	var buf bytes.Buffer
	if err := RunRisk(context.Background(), c, RiskFlags{Args: []string{"pkg.Sym"}, Format: "json"}, &buf); err != nil {
		t.Fatalf("RunRisk json: %v", err)
	}
	if !strings.Contains(buf.String(), `"level"`) {
		t.Errorf("json out = %s", buf.String())
	}
}

func TestRunRiskFormatValidated(t *testing.T) {
	c := &fakeCaronteVerbsClient{}
	err := RunRisk(context.Background(), c, RiskFlags{Args: []string{"pkg.X"}, Format: "yaml"}, &bytes.Buffer{})
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("invalid format → err %v; want recoverable", err)
	}
}

func TestRunRiskClientError(t *testing.T) {
	c := &fakeCaronteVerbsClient{err: errors.New("net error")}
	err := RunRisk(context.Background(), c, RiskFlags{Args: []string{"pkg.X"}, Format: "text"}, &bytes.Buffer{})
	if err == nil {
		t.Error("expected error from client")
	}
}

func TestRunRiskFileExtension(t *testing.T) {
	c := &fakeCaronteVerbsClient{risk: &client.RiskResponse{Level: "low"}}
	if err := RunRisk(context.Background(), c, RiskFlags{Args: []string{"main.go"}, Format: "text"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("RunRisk: %v", err)
	}
	if len(c.gotRisk.ChangedFiles) != 1 || c.gotRisk.ChangedFiles[0] != "main.go" {
		t.Errorf("ChangedFiles = %v; want [main.go]", c.gotRisk.ChangedFiles)
	}
}

func TestRunCochangeJSON(t *testing.T) {
	c := &fakeCaronteVerbsClient{cochange: &client.CoChangeResponse{File: "a.go"}}
	var buf bytes.Buffer
	if err := RunCochange(context.Background(), c, CochangeFlags{File: "a.go", Format: "json"}, &buf); err != nil {
		t.Fatalf("RunCochange json: %v", err)
	}
	if !strings.Contains(buf.String(), `"file"`) {
		t.Errorf("json out = %s", buf.String())
	}
}

func TestRunCochangeNoPeers(t *testing.T) {
	c := &fakeCaronteVerbsClient{cochange: &client.CoChangeResponse{File: "a.go", Peers: nil}}
	var buf bytes.Buffer
	if err := RunCochange(context.Background(), c, CochangeFlags{File: "a.go", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunCochange: %v", err)
	}
	if !strings.Contains(buf.String(), "no co-change peers") {
		t.Errorf("want no-peers message: %s", buf.String())
	}
}

func TestRunCochangeFormatValidated(t *testing.T) {
	c := &fakeCaronteVerbsClient{}
	err := RunCochange(context.Background(), c, CochangeFlags{File: "a.go", Format: "yaml"}, &bytes.Buffer{})
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("invalid format → err %v; want recoverable", err)
	}
}

func TestRunCochangeClientError(t *testing.T) {
	c := &fakeCaronteVerbsClient{err: errors.New("net error")}
	err := RunCochange(context.Background(), c, CochangeFlags{File: "a.go", Format: "text"}, &bytes.Buffer{})
	if err == nil {
		t.Error("expected error from client")
	}
}

func TestRunImplJSON(t *testing.T) {
	c := &fakeCaronteVerbsClient{impl: &client.ImplResponse{Interface: "io.Writer"}}
	var buf bytes.Buffer
	if err := RunImpl(context.Background(), c, ImplFlags{Interface: "io.Writer", Format: "json"}, &buf); err != nil {
		t.Fatalf("RunImpl json: %v", err)
	}
	if !strings.Contains(buf.String(), `"interface"`) {
		t.Errorf("json out = %s", buf.String())
	}
}

func TestRunImplNoImplementations(t *testing.T) {
	c := &fakeCaronteVerbsClient{impl: &client.ImplResponse{Interface: "io.Writer", Implementations: nil}}
	var buf bytes.Buffer
	if err := RunImpl(context.Background(), c, ImplFlags{Interface: "io.Writer", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunImpl: %v", err)
	}
	if !strings.Contains(buf.String(), "no implementations found") {
		t.Errorf("want no-impl message: %s", buf.String())
	}
}

func TestRunImplUnreachable(t *testing.T) {
	c := &fakeCaronteVerbsClient{impl: &client.ImplResponse{
		Interface:       "io.Writer",
		Implementations: []client.ImplDTO{{ImplID: "bytes.Buffer", Confidence: "exact_vta", Reachable: false}},
	}}
	var buf bytes.Buffer
	if err := RunImpl(context.Background(), c, ImplFlags{Interface: "io.Writer", Format: "text"}, &buf); err != nil {
		t.Fatalf("RunImpl: %v", err)
	}
	if !strings.Contains(buf.String(), "unreachable") {
		t.Errorf("want unreachable tag: %s", buf.String())
	}
}

func TestRunImplFormatValidated(t *testing.T) {
	c := &fakeCaronteVerbsClient{}
	err := RunImpl(context.Background(), c, ImplFlags{Interface: "io.Writer", Format: "yaml"}, &bytes.Buffer{})
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("invalid format → err %v; want recoverable", err)
	}
}

func TestRunImplClientError(t *testing.T) {
	c := &fakeCaronteVerbsClient{err: errors.New("net error")}
	err := RunImpl(context.Background(), c, ImplFlags{Interface: "io.Writer", Format: "text"}, &bytes.Buffer{})
	if err == nil {
		t.Error("expected error from client")
	}
}

func TestTruncateLine(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := truncateLine(long, 80)
	if len([]rune(got)) > 83 {
		t.Errorf("truncateLine did not truncate: len=%d", len(got))
	}
	if truncateLine("short", 80) != "short" {
		t.Errorf("truncateLine modified short string")
	}
	if !strings.Contains(truncateLine("line1\nline2", 80), " ") {
		t.Errorf("truncateLine should replace newline with space")
	}
}

func TestShortSHA(t *testing.T) {
	if shortSHA("abc1234567890") != "abc1234" {
		t.Errorf("shortSHA should return 7 chars")
	}
	if shortSHA("abc") != "abc" {
		t.Errorf("shortSHA should return short string as-is")
	}
}
