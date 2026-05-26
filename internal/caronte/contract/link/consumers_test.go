//go:build cgo

package link

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

func TestConsumersFor_EmptyWorkspaceReturnsEmptySlice(t *testing.T) {
	deps := &fakeDeps{
		stores: map[string]*fakeProjectStore{},
		fed:    &fakeFedRead{links: nil},
	}
	l := &Linker{deps: deps, workspaceID: "ws-empty"}
	got, err := l.ConsumersFor(context.Background(), "ep-1", "auth-svc", "ws-empty")
	if err != nil {
		t.Fatalf("ConsumersFor(empty) = (_, %v); want nil err", err)
	}
	if got == nil {
		t.Errorf("ConsumersFor(empty) returned nil slice; want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("ConsumersFor(empty) len = %d; want 0", len(got))
	}
}

func TestConsumersFor_SingleLinkSingleConsumer(t *testing.T) {
	callStore := &fakeProjectStore{
		calls: []store.APICall{{
			CallID: "c-1", Repo: "client-app",
			CallerNodeID: "client-app:auth_handler.go:42:GetUser",
			Confidence:   "static_path", ExtractedAt: 1, ExtractorID: "gohttp",
		}},
		nodes: map[string]store.Node{
			"client-app:auth_handler.go:42:GetUser": {
				NodeID: "client-app:auth_handler.go:42:GetUser",
				Name:   "GetUser", Kind: "function", Language: "go",
				FilePath: "auth_handler.go", StartLine: 42, EndLine: 60,
			},
		},
	}
	deps := &fakeDeps{
		stores: map[string]*fakeProjectStore{"client-app": callStore},
		fed: &fakeFedRead{links: []federation.LinkRow{{
			CallID: "c-1", CallRepo: "client-app",
			EndpointID: "ep-1", EndpointRepo: "auth-svc",
			Confidence: "static_path", WorkspaceID: "ws-1",
			ResolvedAt: 1, LinkMethod: "static",
		}}},
	}
	l := &Linker{deps: deps, workspaceID: "ws-1"}
	got, err := l.ConsumersFor(context.Background(), "ep-1", "auth-svc", "ws-1")
	if err != nil {
		t.Fatalf("ConsumersFor = (_, %v); want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d; want 1", len(got))
	}
	want := coordinated.ConsumerRef{
		Repo: "client-app", CallID: "c-1",
		NodeID: "client-app:auth_handler.go:42:GetUser",
		File:   "auth_handler.go", Line: 42,
	}
	if got[0] != want {
		t.Errorf("ConsumerRef drift:\n got=%+v\nwant=%+v", got[0], want)
	}
}

func TestConsumersFor_MultipleLinksSameEndpointDeduplicated(t *testing.T) {
	callStore := &fakeProjectStore{
		calls: []store.APICall{
			{CallID: "c-dup-1", Repo: "client-app", CallerNodeID: "client-app:handler.go:10:Foo", ExtractedAt: 1, ExtractorID: "x", Confidence: "static_path"},
			{CallID: "c-dup-2", Repo: "client-app", CallerNodeID: "client-app:handler.go:10:Foo", ExtractedAt: 1, ExtractorID: "x", Confidence: "static_path"},
		},
		nodes: map[string]store.Node{
			"client-app:handler.go:10:Foo": {NodeID: "client-app:handler.go:10:Foo", FilePath: "handler.go", StartLine: 10},
		},
	}
	deps := &fakeDeps{
		stores: map[string]*fakeProjectStore{"client-app": callStore},
		fed: &fakeFedRead{links: []federation.LinkRow{
			{CallID: "c-dup-1", CallRepo: "client-app", EndpointID: "ep-1", EndpointRepo: "auth-svc", WorkspaceID: "ws-1", LinkMethod: "static", Confidence: "static_path"},
			{CallID: "c-dup-2", CallRepo: "client-app", EndpointID: "ep-1", EndpointRepo: "auth-svc", WorkspaceID: "ws-1", LinkMethod: "static", Confidence: "static_path"},
		}},
	}
	l := &Linker{deps: deps, workspaceID: "ws-1"}
	got, err := l.ConsumersFor(context.Background(), "ep-1", "auth-svc", "ws-1")
	if err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if len(got) != 1 {
		t.Errorf("len = %d; want 1 (dedup on Repo+NodeID)", len(got))
	}
}

func TestConsumersFor_CrossRepoLinksMultipleEntries(t *testing.T) {
	callStoreA := &fakeProjectStore{
		calls: []store.APICall{{CallID: "c-a", Repo: "client-app", CallerNodeID: "client-app:x.go:5:Foo", ExtractedAt: 1, ExtractorID: "x", Confidence: "static_path"}},
		nodes: map[string]store.Node{"client-app:x.go:5:Foo": {NodeID: "client-app:x.go:5:Foo", FilePath: "x.go", StartLine: 5}},
	}
	callStoreB := &fakeProjectStore{
		calls: []store.APICall{{CallID: "c-b", Repo: "admin-app", CallerNodeID: "admin-app:y.go:7:Bar", ExtractedAt: 1, ExtractorID: "x", Confidence: "static_path"}},
		nodes: map[string]store.Node{"admin-app:y.go:7:Bar": {NodeID: "admin-app:y.go:7:Bar", FilePath: "y.go", StartLine: 7}},
	}
	deps := &fakeDeps{
		stores: map[string]*fakeProjectStore{
			"client-app": callStoreA,
			"admin-app":  callStoreB,
		},
		fed: &fakeFedRead{links: []federation.LinkRow{
			{CallID: "c-a", CallRepo: "client-app", EndpointID: "ep-1", EndpointRepo: "auth-svc", WorkspaceID: "ws-1", LinkMethod: "static", Confidence: "static_path"},
			{CallID: "c-b", CallRepo: "admin-app", EndpointID: "ep-1", EndpointRepo: "auth-svc", WorkspaceID: "ws-1", LinkMethod: "static", Confidence: "static_path"},
		}},
	}
	l := &Linker{deps: deps, workspaceID: "ws-1"}
	got, err := l.ConsumersFor(context.Background(), "ep-1", "auth-svc", "ws-1")
	if err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2 (cross-repo)", len(got))
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Repo < got[j].Repo })
	if got[0].Repo != "admin-app" || got[1].Repo != "client-app" {
		t.Errorf("cross-repo set drift: %+v", got)
	}
}

func TestConsumersFor_PartialResultOnStoreError(t *testing.T) {
	callStore := &fakeProjectStore{
		calls: []store.APICall{{CallID: "c-ok", Repo: "client-app", CallerNodeID: "n-1", ExtractedAt: 1, ExtractorID: "x", Confidence: "static_path"}},
		nodes: map[string]store.Node{"n-1": {NodeID: "n-1", FilePath: "f.go", StartLine: 1}},
	}
	deps := &fakeDeps{
		stores: map[string]*fakeProjectStore{"client-app": callStore},
		fed: &fakeFedRead{links: []federation.LinkRow{
			{CallID: "c-ok", CallRepo: "client-app", EndpointID: "ep-1", EndpointRepo: "auth-svc", WorkspaceID: "ws-1", LinkMethod: "static", Confidence: "static_path"},
			{CallID: "c-bad", CallRepo: "missing-repo", EndpointID: "ep-1", EndpointRepo: "auth-svc", WorkspaceID: "ws-1", LinkMethod: "static", Confidence: "static_path"},
		}},
	}
	l := &Linker{deps: deps, workspaceID: "ws-1"}
	got, err := l.ConsumersFor(context.Background(), "ep-1", "auth-svc", "ws-1")
	if err == nil {
		t.Errorf("err = nil; want non-nil partial-result error")
	}
	if len(got) != 1 {
		t.Errorf("len = %d; want 1 (partial result preserves the good link)", len(got))
	}
}

// TestConsumersFor_FilterByEndpointTuple pins that links pointing at a
// DIFFERENT endpoint on the same target_repo do NOT contribute.
func TestConsumersFor_FilterByEndpointTuple(t *testing.T) {
	callStore := &fakeProjectStore{
		calls: []store.APICall{
			{CallID: "c-target", Repo: "client-app", CallerNodeID: "n-target", ExtractedAt: 1, ExtractorID: "x", Confidence: "static_path"},
			{CallID: "c-other", Repo: "client-app", CallerNodeID: "n-other", ExtractedAt: 1, ExtractorID: "x", Confidence: "static_path"},
		},
		nodes: map[string]store.Node{
			"n-target": {NodeID: "n-target", FilePath: "f.go", StartLine: 1},
			"n-other":  {NodeID: "n-other", FilePath: "f.go", StartLine: 2},
		},
	}
	deps := &fakeDeps{
		stores: map[string]*fakeProjectStore{"client-app": callStore},
		fed: &fakeFedRead{links: []federation.LinkRow{
			{CallID: "c-target", CallRepo: "client-app", EndpointID: "ep-1", EndpointRepo: "auth-svc", WorkspaceID: "ws-1", LinkMethod: "static", Confidence: "static_path"},
			{CallID: "c-other", CallRepo: "client-app", EndpointID: "ep-2", EndpointRepo: "auth-svc", WorkspaceID: "ws-1", LinkMethod: "static", Confidence: "static_path"},
		}},
	}
	l := &Linker{deps: deps, workspaceID: "ws-1"}
	got, err := l.ConsumersFor(context.Background(), "ep-1", "auth-svc", "ws-1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0].NodeID != "n-target" {
		t.Errorf("filter drift: %+v; want only n-target", got)
	}
}

func TestConsumersFor_NodeMissingDegradesGracefully(t *testing.T) {
	callStore := &fakeProjectStore{
		calls: []store.APICall{{CallID: "c-ng", Repo: "client-app", CallerNodeID: "ghost-node", ExtractedAt: 1, ExtractorID: "x", Confidence: "static_path"}},
		nodes: map[string]store.Node{},
	}
	deps := &fakeDeps{
		stores: map[string]*fakeProjectStore{"client-app": callStore},
		fed: &fakeFedRead{links: []federation.LinkRow{{
			CallID: "c-ng", CallRepo: "client-app", EndpointID: "ep-1", EndpointRepo: "auth-svc", WorkspaceID: "ws-1", LinkMethod: "static", Confidence: "static_path",
		}}},
	}
	l := &Linker{deps: deps, workspaceID: "ws-1"}
	got, err := l.ConsumersFor(context.Background(), "ep-1", "auth-svc", "ws-1")
	if err != nil {
		t.Fatalf("err = %v; want nil (node-missing is degraded-gracefully)", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d; want 1", len(got))
	}
	if got[0].File != "" || got[0].Line != 0 {
		t.Errorf("ghost-node ConsumerRef: %+v; want File=\"\" Line=0", got[0])
	}
	if got[0].NodeID != "ghost-node" {
		t.Errorf("NodeID = %q; want ghost-node (preserved)", got[0].NodeID)
	}
}

func TestConsumersFor_NilDepsReturnsError(t *testing.T) {
	l := &Linker{workspaceID: "ws-1"}
	got, err := l.ConsumersFor(context.Background(), "ep-1", "auth-svc", "ws-1")
	if err == nil {
		t.Errorf("err = nil; want non-nil (nil deps)")
	}
	if got == nil || len(got) != 0 {
		t.Errorf("got = %v; want []ConsumerRef{}", got)
	}
}

func TestConsumersFor_FedListError(t *testing.T) {
	deps := &fakeDeps{
		stores: map[string]*fakeProjectStore{},
		fed:    &fakeFedRead{err: errors.New("federation read down")},
	}
	l := &Linker{deps: deps, workspaceID: "ws-1"}
	_, err := l.ConsumersFor(context.Background(), "ep-1", "auth-svc", "ws-1")
	if err == nil {
		t.Errorf("err = nil; want federation-list error")
	}
}
