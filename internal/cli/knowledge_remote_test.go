package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func newHTTP503Error() error {
	return &client.HTTPError{
		Method:  http.MethodPost,
		Path:    "/v1/knowledge/ecosystem/query",
		Status:  http.StatusServiceUnavailable,
		RawBody: []byte("ecosystem dispatcher not configured"),
	}
}

func newHTTP422Error() error {
	return &client.HTTPError{
		Method:  http.MethodPost,
		Path:    "/v1/knowledge/ecosystem/query",
		Status:  http.StatusUnprocessableEntity,
		RawBody: []byte("ecosystem must be go|python|typescript|rust"),
	}
}

func TestRunKnowledgeQueryRemoteEmptyQuery(t *testing.T) {
	c := &fakeKnowledgeClient{ecoResp: &client.EcosystemQueryResponse{}}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: ""}
	var w bytes.Buffer
	if err := RunKnowledgeQueryRemote(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ecoCalled != 1 {
		t.Errorf("EcosystemQuery call count = %d, want 1", c.ecoCalled)
	}
}

func TestRunKnowledgeQueryRemoteInvalidEcosystem(t *testing.T) {
	c := &fakeKnowledgeClient{}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "q", Ecosystem: "kotlin"}
	var w bytes.Buffer
	err := RunKnowledgeQueryRemote(context.Background(), c, flags, &w)
	if err == nil {
		t.Fatal("expected error for unknown ecosystem")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %T: %v", err, err)
	}
	if c.ecoCalled != 0 {
		t.Errorf("client should not be called on validation failure; called=%d", c.ecoCalled)
	}
}

func TestRunKnowledgeQueryRemoteInvalidDoctrine(t *testing.T) {
	c := &fakeKnowledgeClient{}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "q", Doctrine: "aggressive"}
	var w bytes.Buffer
	err := RunKnowledgeQueryRemote(context.Background(), c, flags, &w)
	if err == nil {
		t.Fatal("expected error for unknown doctrine")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestRunKnowledgeQueryRemoteInvalidFormat(t *testing.T) {
	c := &fakeKnowledgeClient{}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "q", RemoteFormat: "yaml"}
	var w bytes.Buffer
	err := RunKnowledgeQueryRemote(context.Background(), c, flags, &w)
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestRunKnowledgeQueryRemoteNegativeMaxResults(t *testing.T) {
	c := &fakeKnowledgeClient{}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "q", RemoteMaxResults: -1}
	var w bytes.Buffer
	err := RunKnowledgeQueryRemote(context.Background(), c, flags, &w)
	if err == nil {
		t.Fatal("expected error for negative --max-results")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("expected ErrRecoverable, got %v", err)
	}
}

func TestRunKnowledgeQueryRemoteJSONFormat(t *testing.T) {
	c := &fakeKnowledgeClient{ecoResp: &client.EcosystemQueryResponse{
		Chunks: []client.EcosystemChunk{
			{SymbolPath: "context.Context", PackageName: "context", Version: "1.22.0", RerankerScore: 0.95},
		},
	}}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "context", RemoteFormat: "json"}
	var w bytes.Buffer
	if err := RunKnowledgeQueryRemote(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunKnowledgeQueryRemote: %v", err)
	}
	out := w.String()
	if !strings.Contains(out, "context.Context") {
		t.Errorf("output missing symbol: %s", out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("JSON output doesn't start with {: %q", out)
	}
}

func TestRunKnowledgeQueryRemoteHumanFormat(t *testing.T) {
	c := &fakeKnowledgeClient{ecoResp: &client.EcosystemQueryResponse{
		Chunks: []client.EcosystemChunk{
			{SymbolPath: "fmt.Println", PackageName: "fmt", Version: "1.22.0", Kind: "function",
				RerankerScore: 0.85, SourceURL: "https://pkg.go.dev/fmt#Println"},
		},
		Provenance: client.EcosystemProvenance{
			DetectedVersion: "1.22.0", DetectionLayer: 1, RoutingMethod: "single",
		},
	}}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "println", RemoteFormat: "human"}
	var w bytes.Buffer
	if err := RunKnowledgeQueryRemote(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunKnowledgeQueryRemote: %v", err)
	}
	out := w.String()
	if !strings.Contains(out, "fmt.Println") {
		t.Errorf("output missing symbol: %s", out)
	}
	if !strings.Contains(out, "detected version: 1.22.0") {
		t.Errorf("provenance not rendered: %s", out)
	}
	if !strings.Contains(out, "layer 1") {
		t.Errorf("provenance detection layer not rendered: %s", out)
	}
}

func TestRunKnowledgeQueryRemoteHumanFormatNoResults(t *testing.T) {
	c := &fakeKnowledgeClient{ecoResp: &client.EcosystemQueryResponse{
		Chunks: []client.EcosystemChunk{},
	}}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "nope"}
	var w bytes.Buffer
	if err := RunKnowledgeQueryRemote(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunKnowledgeQueryRemote: %v", err)
	}
	if !strings.Contains(w.String(), "(no results)") {
		t.Errorf("missing '(no results)' marker: %q", w.String())
	}
}

func TestRunKnowledgeQueryRemoteAbstained(t *testing.T) {
	c := &fakeKnowledgeClient{ecoResp: &client.EcosystemQueryResponse{
		Abstained:     true,
		AbstainReason: "low confidence across all ecosystems",
	}}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "ambiguous"}
	var w bytes.Buffer
	if err := RunKnowledgeQueryRemote(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := w.String()
	if !strings.Contains(out, "abstained") {
		t.Errorf("abstain marker missing: %q", out)
	}
	if !strings.Contains(out, "low confidence") {
		t.Errorf("abstain reason missing: %q", out)
	}
}

func TestRunKnowledgeQueryRemoteDaemonError503(t *testing.T) {
	c := &fakeKnowledgeClient{ecoErr: newHTTP503Error()}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "q"}
	err := RunKnowledgeQueryRemote(context.Background(), c, flags, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrRecoverable) {
		t.Errorf("503 should be unrecoverable, got recoverable: %v", err)
	}
}

func TestRunKnowledgeQueryRemoteDaemonError422(t *testing.T) {
	c := &fakeKnowledgeClient{ecoErr: newHTTP422Error()}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "q"}
	err := RunKnowledgeQueryRemote(context.Background(), c, flags, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Errorf("422 should be recoverable, got %T: %v", err, err)
	}
}

func TestRunKnowledgeQueryRemoteRequestFieldsPopulated(t *testing.T) {
	c := &fakeKnowledgeClient{ecoResp: &client.EcosystemQueryResponse{}}
	flags := KnowledgeQueryFlags{
		Remote:           true,
		FreeText:         "context.Context",
		Ecosystem:        "go",
		Version:          "1.22.0",
		Doctrine:         "max-scope",
		RemoteMaxResults: 5,
	}
	var w bytes.Buffer
	if err := RunKnowledgeQueryRemote(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunKnowledgeQueryRemote: %v", err)
	}
	if c.lastEcoReq.Query != "context.Context" {
		t.Errorf("Query = %q", c.lastEcoReq.Query)
	}
	if c.lastEcoReq.Ecosystem != "go" {
		t.Errorf("Ecosystem = %q", c.lastEcoReq.Ecosystem)
	}
	if c.lastEcoReq.Version != "1.22.0" {
		t.Errorf("Version = %q", c.lastEcoReq.Version)
	}
	if c.lastEcoReq.Doctrine != "max-scope" {
		t.Errorf("Doctrine = %q", c.lastEcoReq.Doctrine)
	}
	if c.lastEcoReq.MaxResults != 5 {
		t.Errorf("MaxResults = %d", c.lastEcoReq.MaxResults)
	}
}

func TestRunKnowledgeQueryRemoteDefaultMaxResults(t *testing.T) {
	c := &fakeKnowledgeClient{ecoResp: &client.EcosystemQueryResponse{}}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "q"}
	var w bytes.Buffer
	if err := RunKnowledgeQueryRemote(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunKnowledgeQueryRemote: %v", err)
	}
	if c.lastEcoReq.MaxResults != 10 {
		t.Errorf("default MaxResults = %d, want 10", c.lastEcoReq.MaxResults)
	}
}

func TestRunKnowledgeQueryRemoteDefaultFormat(t *testing.T) {
	c := &fakeKnowledgeClient{ecoResp: &client.EcosystemQueryResponse{
		Chunks: []client.EcosystemChunk{{SymbolPath: "x", PackageName: "p", Kind: "func"}},
	}}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "q"}
	var w bytes.Buffer
	if err := RunKnowledgeQueryRemote(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunKnowledgeQueryRemote: %v", err)
	}
	out := w.String()
	if !strings.Contains(out, "SYMBOL") {
		t.Errorf("default format should be human (tabwriter); got: %q", out)
	}
}

func TestRunKnowledgeQueryDispatchesToRemoteWhenFlagSet(t *testing.T) {
	c := &fakeKnowledgeClient{ecoResp: &client.EcosystemQueryResponse{}}
	flags := KnowledgeQueryFlags{Remote: true, FreeText: "anything"}
	var w bytes.Buffer
	if err := RunKnowledgeQuery(context.Background(), c, flags, &w); err != nil {
		t.Fatalf("RunKnowledgeQuery: %v", err)
	}
	if c.ecoCalled != 1 {
		t.Errorf("EcosystemQuery should be called on --remote; called=%d", c.ecoCalled)
	}
	if c.lastQueryReq.FreeText != "" {
		t.Errorf("non-remote path should not have run; lastQueryReq.FreeText=%q", c.lastQueryReq.FreeText)
	}
}

func TestClassifyEcosystemErrorNil(t *testing.T) {
	if got := classifyEcosystemError(nil, "remote-query"); got != nil {
		t.Errorf("classifyEcosystemError(nil) = %v, want nil", got)
	}
}

func TestClassifyEcosystemErrorAlreadyRecoverableUnwrapped(t *testing.T) {
	pre := recoverable("pre-validated bad input")
	got := classifyEcosystemError(pre, "remote-query")
	if got != pre {
		t.Errorf("recoverable err should pass through unchanged; got %v want %v", got, pre)
	}
	if !errors.Is(got, ErrRecoverable) {
		t.Errorf("returned err should still satisfy ErrRecoverable")
	}
}
