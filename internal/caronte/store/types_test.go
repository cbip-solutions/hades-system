package store

import (
	"reflect"
	"testing"
)

func TestConfidenceConstants(t *testing.T) {
	cases := []struct {
		got  Confidence
		want string
	}{
		{ConfExactStatic, "exact_static"},
		{ConfExactVTA, "exact_vta"},
		{ConfExactCHA, "exact_cha"},
		{ConfSCIPImpl, "scip_impl"},
		{ConfHeuristicName, "heuristic_name"},
		{ConfLLMHint, "llm_hint"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("Confidence = %q; want %q", string(c.got), c.want)
		}
	}
}

func TestConfidenceValid(t *testing.T) {
	for _, c := range AllConfidences() {
		if !c.Valid() {
			t.Errorf("AllConfidences() member %q reports !Valid()", c)
		}
	}
	if Confidence("bogus").Valid() {
		t.Error("Confidence(\"bogus\").Valid() = true; want false")
	}
	if Confidence("").Valid() {
		t.Error("empty Confidence reports Valid(); want false")
	}
}

func TestNodeFieldSet(t *testing.T) {
	n := Node{
		NodeID: "pkg/x.T.M", Name: "M", Kind: string(KindMethod),
		Language: "go", FilePath: "pkg/x/x.go",
		StartLine: 10, EndLine: 20,
		Signature: "func (T) M() error", Doc: "M does things.",
		Coreness: 3, SCCID: 7, PackageID: "pkg/x",
		ContentHash: "deadbeef",
	}
	if n.NodeID == "" || n.Kind == "" || n.ContentHash == "" {
		t.Fatal("Node field set incomplete")
	}
}

func TestEdgeReachablePointer(t *testing.T) {
	yes := true
	e := Edge{
		SourceID: "a", TargetID: "b", Kind: string(EdgeCalls),
		Confidence: ConfExactVTA, Reachable: &yes,
		SiteFile: "a.go", SiteLine: 42,
	}
	if e.Reachable == nil || *e.Reachable != true {
		t.Error("Edge.Reachable must be a settable *bool")
	}
	var nilReach Edge
	if nilReach.Reachable != nil {
		t.Error("zero-value Edge.Reachable must be nil (NULL in DB)")
	}
}

func TestAllNodeKindsContainsExactlySevenKinds(t *testing.T) {
	got := AllNodeKinds()
	if len(got) != 7 {
		t.Errorf("AllNodeKinds() len = %d; want 7 (one per KindXxx const)", len(got))
	}

	set := make(map[NodeKind]bool, len(got))
	for _, k := range got {
		set[k] = true
	}
	required := []NodeKind{
		KindFunction, KindMethod, KindStruct, KindInterface,
		KindType, KindField, KindPackage,
	}
	for _, k := range required {
		if !set[k] {
			t.Errorf("AllNodeKinds() missing %q", k)
		}
	}

	a := AllNodeKinds()
	b := AllNodeKinds()
	a[0] = "corrupted"
	if b[0] == "corrupted" {
		t.Error("AllNodeKinds() returns a shared slice; must return a fresh copy each call")
	}
}

func TestEvolutionAndIntentFieldSets(t *testing.T) {
	_ = CoChange{FileA: "a.go", FileB: "b.go", SharedRevs: 4, RevsA: 9, RevsB: 7, WindowDays: 90, UpdatedAt: 1}
	_ = Churn{Path: "a.go", WindowDays: 90, TouchCount: 12, AuthorCount: 3, LastTouched: 100, UpdatedAt: 200}
	_ = ADRLink{ADRID: "docs/decisions/0100-x.md", NodeID: "pkg/x.T", PackageID: "pkg/x", LinkKind: string(LinkExplicitRef), Confidence: 0.91, Stale: false}
	_ = LoreTrailer{CommitSHA: "abc", FilePath: "a.go", NodeID: "pkg/x.T", TrailerKind: string(TrailerConstraint), Body: "no http here", AuthoredAt: 1}
}

func TestAPIEndpointFieldSet(t *testing.T) {
	e := APIEndpoint{
		EndpointID:       "github.com/acme/svc:http:GET /users/{id}",
		Repo:             "github.com/acme/svc",
		Kind:             string(KindHTTP),
		Method:           "GET",
		PathTemplate:     "/users/{id}",
		ProtoService:     "",
		ProtoRPC:         "",
		Topic:            "",
		GraphQLType:      "",
		GraphQLField:     "",
		HandlerNodeID:    "internal/handler.UsersGet",
		ContractArtifact: "openapi/users.yaml",
		ExtractedAt:      1716480000,
		ExtractorID:      "gohttp/chi@v1",
	}

	if e.EndpointID == "" || e.Kind == "" || e.HandlerNodeID == "" || e.ExtractorID == "" {
		t.Fatal("APIEndpoint field set incomplete (required fields zero)")
	}
	if e.ExtractedAt == 0 {
		t.Error("APIEndpoint.ExtractedAt is unix-seconds (>0 for any real row)")
	}
}

func TestAPICallFieldSet(t *testing.T) {
	c := APICall{
		CallID:             "github.com/acme/ui:internal/client.GetUser:42",
		Repo:               "github.com/acme/ui",
		CallerNodeID:       "internal/client.GetUser",
		TargetMethod:       "GET",
		TargetPathTemplate: "/users/{id}",
		TargetProto:        "",
		TargetTopic:        "",
		TargetGraphQLType:  "",
		TargetGraphQLField: "",
		BaseURLRef:         "BACKEND_URL",
		Confidence:         "static_path",
		ExtractedAt:        1716480000,
		ExtractorID:        "gohttp-client@v1",
	}
	if c.CallID == "" || c.CallerNodeID == "" || c.Confidence == "" || c.ExtractorID == "" {
		t.Fatal("APICall field set incomplete (required fields zero)")
	}
	if c.ExtractedAt == 0 {
		t.Error("APICall.ExtractedAt is unix-seconds (>0 for any real row)")
	}
}

func TestAPIEndpointKindsAreFiveExact(t *testing.T) {
	got := AllAPIEndpointKinds()
	want := []APIEndpointKind{KindHTTP, KindGRPC, KindGraphQL, KindMQ, KindWS}
	if len(got) != len(want) {
		t.Fatalf("AllAPIEndpointKinds() = %v; want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("AllAPIEndpointKinds()[%d] = %q; want %q", i, got[i], want[i])
		}
	}

	wantStrings := []string{"http", "grpc", "graphql", "mq", "ws"}
	for i, k := range got {
		if string(k) != wantStrings[i] {
			t.Errorf("AllAPIEndpointKinds()[%d] string = %q; want %q", i, string(k), wantStrings[i])
		}
	}
}

func TestBreakingChangeFieldCount(t *testing.T) {
	rv := reflect.ValueOf(BreakingChange{})
	if got, want := rv.NumField(), 12; got != want {
		t.Errorf("store.BreakingChange field count = %d; want %d", got, want)
	}
}
