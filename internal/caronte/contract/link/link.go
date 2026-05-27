// go:build cgo

// SPDX-License-Identifier: MIT

package link

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

type ProjectStorePort interface {
	ListAPICallsByRepo(ctx context.Context, repo string) ([]store.APICall, error)
	ListAPIEndpointsByRepo(ctx context.Context, repo string) ([]store.APIEndpoint, error)
	GetAPICall(ctx context.Context, callID string) (store.APICall, error)
	GetNode(ctx context.Context, nodeID string) (store.Node, error)
}

type WorkspaceLinkPort interface {
	CrossRepoLink(ctx context.Context, link store.ContractLink) error
}

type FederationReadPort interface {
	ListContractLinks(ctx context.Context, workspaceID string, limit int) ([]federation.LinkRow, error)
}

type LinkerDeps interface {
	OpenProjectStore(ctx context.Context, repo string) (ProjectStorePort, error)
	FederationDB() FederationReadPort
}

type Linker struct {
	workspace    WorkspaceLinkPort
	unresolved   *unresolvedSurfacer
	audit        federation.AuditEmitter
	manifests    map[string]*yaml.Manifest
	extractStubs map[string][]extract.StubReference
	workspaceID  string
	deps         LinkerDeps
}

func NewLinker(
	ws WorkspaceLinkPort,
	unresolvedStore UnresolvedStorePort,
	audit federation.AuditEmitter,
	manifests map[string]*yaml.Manifest,
	extractStubs map[string][]extract.StubReference,
	workspaceID string,
	deps LinkerDeps,
) *Linker {
	return &Linker{
		workspace: ws,
		unresolved: &unresolvedSurfacer{
			store:       unresolvedStore,
			audit:       audit,
			workspaceID: workspaceID,
		},
		audit:        audit,
		manifests:    manifests,
		extractStubs: extractStubs,
		workspaceID:  workspaceID,
		deps:         deps,
	}
}

type Result struct {
	ProjectID      string
	Repo           string
	CallsScanned   int
	LinksPersisted int
	UnresolvedRows int
	SilentDrops    int
	TierCounts     map[Confidence]int
	Errors         []error
}

func (l *Linker) LinkProject(ctx context.Context, projectID, repo string) (Result, error) {
	res := Result{ProjectID: projectID, Repo: repo, TierCounts: map[Confidence]int{}}
	if l == nil || l.deps == nil {
		return res, fmt.Errorf("caronte/link: LinkProject: nil linker deps")
	}
	src, err := l.deps.OpenProjectStore(ctx, repo)
	if err != nil {
		return res, fmt.Errorf("caronte/link: open source store %q: %w", repo, err)
	}
	calls, err := src.ListAPICallsByRepo(ctx, repo)
	if err != nil {
		return res, fmt.Errorf("caronte/link: list api_calls: %w", err)
	}
	res.CallsScanned = len(calls)
	for _, call := range calls {
		if err := l.linkOne(ctx, repo, call, &res); err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("call %s: %w", call.CallID, err))
		}
	}
	return res, nil
}

func (l *Linker) linkOne(ctx context.Context, repo string, call store.APICall, res *Result) error {
	manifest := l.manifests[repo]

	if call.TargetProto != "" {
		if hit := l.tryProtoArtifact(ctx, repo, call); hit != nil {
			return l.persistLink(ctx, *hit, LinkArtifact, ConfExactProtoImport, res)
		}
	}
	if call.TargetPathTemplate != "" && manifest != nil {
		if hit := l.trySpecArtifact(ctx, repo, call, manifest); hit != nil {
			return l.persistLink(ctx, *hit, LinkArtifact, ConfSpecArtifact, res)
		}
	}

	if call.TargetPathTemplate != "" && manifest != nil {
		targetRepo, _, err := resolveTargetRepo(call, manifest)
		if err == nil {
			if hit := l.tryStaticPath(ctx, repo, targetRepo, call); hit != nil {
				return l.persistLink(ctx, *hit, LinkStatic, ConfStaticPath, res)
			}
			if hit := l.tryFuzzyPath(ctx, repo, targetRepo, call); hit != nil {
				return l.persistLink(ctx, *hit, LinkFuzzy, ConfFuzzyPath, res)
			}
		}
	}

	res.UnresolvedRows++
	policy := yaml.DefaultUnresolvedPolicy
	if manifest != nil {
		policy = manifest.UnresolvedPolicy
	}
	if err := l.unresolved.Surface(ctx, call, policy, "no manifest entry / no path match"); err != nil {
		return err
	}
	if policy == yaml.PolicySilent {
		res.SilentDrops++
	}

	return nil
}

func (l *Linker) persistLink(ctx context.Context, link store.ContractLink, method LinkMethod, conf Confidence, res *Result) error {

	if err := checkTierConsistency(method, conf); err != nil {
		return err
	}
	link.Confidence = string(conf)
	link.WorkspaceID = l.workspaceID

	link.ResolvedAt = time.Now().UnixNano()
	link.LinkMethod = string(method)

	if err := l.workspace.CrossRepoLink(ctx, link); err != nil {
		return fmt.Errorf("caronte/link: persist: %w", err)
	}
	res.LinksPersisted++
	res.TierCounts[conf]++
	payload, _ := json.Marshal(map[string]any{
		"call_id":       link.CallID,
		"call_repo":     link.CallRepo,
		"endpoint_id":   link.EndpointID,
		"endpoint_repo": link.EndpointRepo,
		"confidence":    string(conf),
		"link_method":   string(method),
		"resolved_at":   link.ResolvedAt,
	})
	if err := l.audit.Emit(ctx, federation.EvtCrossRepoLink, payload); err != nil {
		return fmt.Errorf("caronte/link: audit emit: %w", err)
	}
	return nil
}

func checkTierConsistency(method LinkMethod, conf Confidence) error {
	switch method {
	case LinkArtifact:
		if conf != ConfExactProtoImport && conf != ConfSpecArtifact {
			return fmt.Errorf("%w: method=artifact conf=%s (want exact_proto_import|spec_artifact)", ErrConfidenceTierDowngrade, conf)
		}
	case LinkStatic:
		if conf != ConfStaticPath {
			return fmt.Errorf("%w: method=static conf=%s (want static_path)", ErrConfidenceTierDowngrade, conf)
		}
	case LinkFuzzy:
		if conf != ConfFuzzyPath {
			return fmt.Errorf("%w: method=fuzzy conf=%s (want fuzzy_path)", ErrConfidenceTierDowngrade, conf)
		}
	case LinkCaronteYAML:

		return fmt.Errorf("%w: bare caronte_yaml LinkMethod not confidence-bearing", ErrConfidenceTierDowngrade)
	default:
		return fmt.Errorf("%w: unknown LinkMethod %q", ErrConfidenceTierDowngrade, method)
	}
	return nil
}

func (l *Linker) tryProtoArtifact(ctx context.Context, sourceRepo string, call store.APICall) *store.ContractLink {
	stubs := l.extractStubs[sourceRepo]
	if len(stubs) == 0 {
		return nil
	}
	wantKey := call.TargetProto
	for _, ref := range stubs {
		candidate := GRPCKey(ref.ProtoPackage, ref.ServiceName, ref.RpcName)
		if candidate != wantKey {
			continue
		}

		targetRepo := ref.Repo
		ep := l.findGRPCEndpoint(ctx, targetRepo, ref.ProtoPackage, ref.ServiceName, ref.RpcName)
		if ep == nil {
			continue
		}
		return &store.ContractLink{
			CallID:       call.CallID,
			CallRepo:     call.Repo,
			EndpointID:   ep.EndpointID,
			EndpointRepo: ep.Repo,
		}
	}
	return nil
}

func (l *Linker) trySpecArtifact(ctx context.Context, sourceRepo string, call store.APICall, manifest *yaml.Manifest) *store.ContractLink {
	targetRepo, _, err := resolveTargetRepo(call, manifest)
	if err != nil {
		return nil
	}
	endpoints := l.listEndpoints(ctx, targetRepo)
	wantKey := HTTPKey(call.TargetMethod, call.TargetPathTemplate)
	for _, ep := range endpoints {
		if ep.Kind != "http" || ep.ContractArtifact == "" {
			continue
		}
		if HTTPKey(ep.Method, ep.PathTemplate) != wantKey {
			continue
		}
		return &store.ContractLink{
			CallID:       call.CallID,
			CallRepo:     call.Repo,
			EndpointID:   ep.EndpointID,
			EndpointRepo: ep.Repo,
		}
	}
	return nil
}

func (l *Linker) tryStaticPath(ctx context.Context, sourceRepo, targetRepo string, call store.APICall) *store.ContractLink {
	endpoints := l.listEndpoints(ctx, targetRepo)
	for _, ep := range endpoints {
		if ep.Kind != "http" {
			continue
		}
		if !sameHTTPMethod(ep.Method, call.TargetMethod) {
			continue
		}
		if ep.PathTemplate != call.TargetPathTemplate {
			continue
		}
		return &store.ContractLink{
			CallID:       call.CallID,
			CallRepo:     call.Repo,
			EndpointID:   ep.EndpointID,
			EndpointRepo: ep.Repo,
		}
	}
	return nil
}

func (l *Linker) tryFuzzyPath(ctx context.Context, sourceRepo, targetRepo string, call store.APICall) *store.ContractLink {
	endpoints := l.listEndpoints(ctx, targetRepo)
	wantKey := HTTPKey(call.TargetMethod, call.TargetPathTemplate)
	for _, ep := range endpoints {
		if ep.Kind != "http" {
			continue
		}
		if HTTPKey(ep.Method, ep.PathTemplate) != wantKey {
			continue
		}
		return &store.ContractLink{
			CallID:       call.CallID,
			CallRepo:     call.Repo,
			EndpointID:   ep.EndpointID,
			EndpointRepo: ep.Repo,
		}
	}
	return nil
}

func (l *Linker) listEndpoints(ctx context.Context, repo string) []store.APIEndpoint {
	if l.deps == nil {
		return nil
	}
	s, err := l.deps.OpenProjectStore(ctx, repo)
	if err != nil {
		return nil
	}
	eps, err := s.ListAPIEndpointsByRepo(ctx, repo)
	if err != nil {
		return nil
	}
	return eps
}

func (l *Linker) findGRPCEndpoint(ctx context.Context, repo, pkg, service, rpc string) *store.APIEndpoint {
	endpoints := l.listEndpoints(ctx, repo)
	for i := range endpoints {
		ep := &endpoints[i]
		if ep.Kind != "grpc" {
			continue
		}

		fqn := service
		if pkg != "" {
			fqn = pkg + "." + service
		}
		if (ep.ProtoService == fqn || ep.ProtoService == service) && ep.ProtoRPC == rpc {
			return ep
		}
	}
	return nil
}

func sameHTTPMethod(a, b string) bool {
	if a == b {
		return true
	}

	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'a' && ca <= 'z' {
			ca -= 32
		}
		if cb >= 'a' && cb <= 'z' {
			cb -= 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// ConsumersFor returns the deduplicated set of consumer call sites that
// link to (endpointID, endpointRepo) inside workspaceID.
// breaking-change pipeline consumes this surface (Pipeline.Fan calls
// (Linker).ConsumersFor to enumerate `[]coordinated.ConsumerRef` for a
// changed endpoint). FIX-1 of review locks the signature
// against the ConsumerEnumerator port.
//
// Algorithm
// 1. Read every contract_links row for workspaceID via the federation
// read port.
// 2. Filter to rows where (EndpointID, EndpointRepo) match the request.
// 3. For each matching link, open the call-side repo's caronte.db via
// deps.OpenProjectStore(ctx, link.CallRepo) and read the api_calls
// row by call_id; then resolve the caller node (graph_nodes) via
// GetNode(call.CallerNodeID) for File + Line attribution; build a
// coordinated.ConsumerRef carrying Repo / CallID / NodeID / File /
// Line.
// 4. Deduplicate on (Repo, NodeID) — multiple links from the same call
// site collapse to one ConsumerRef.
// 5. Partial-result pattern: on a per-link store-read error, the function
// returns the consumers gathered so far PLUS the error so the caller
// sees both the partial set and the failure
// context — never silently drop a consumer.
//
// Boundary: this method's
// import set is limited to `internal/caronte/coordinated` (for the value
// type), `internal/caronte/store` (for the per-repo handle the deps
// accessor returns), and `internal/caronte/store/federation` (for the
// ListContractLinks reader). It MUST NOT reach `internal/store` directly.
func (l *Linker) ConsumersFor(ctx context.Context, endpointID, endpointRepo, workspaceID string) ([]coordinated.ConsumerRef, error) {
	out := []coordinated.ConsumerRef{}
	if l == nil || l.deps == nil {
		return out, fmt.Errorf("caronte/link: ConsumersFor: nil linker deps")
	}
	fed := l.deps.FederationDB()
	if fed == nil {
		return out, fmt.Errorf("caronte/link: ConsumersFor: nil federation DB handle")
	}
	links, err := fed.ListContractLinks(ctx, workspaceID, 0)
	if err != nil {
		return out, fmt.Errorf("caronte/link: ConsumersFor: list contract_links: %w", err)
	}
	seen := make(map[string]bool, len(links))
	var firstErr error
	for _, lnk := range links {
		if lnk.EndpointID != endpointID || lnk.EndpointRepo != endpointRepo {
			continue
		}
		callStore, openErr := l.deps.OpenProjectStore(ctx, lnk.CallRepo)
		if openErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("caronte/link: ConsumersFor: open store %q: %w", lnk.CallRepo, openErr)
			}
			continue
		}
		call, getErr := callStore.GetAPICall(ctx, lnk.CallID)
		if getErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("caronte/link: ConsumersFor: get api_call %q in %q: %w", lnk.CallID, lnk.CallRepo, getErr)
			}
			continue
		}

		key := lnk.CallRepo + "\x00" + call.CallerNodeID
		if seen[key] {
			continue
		}
		seen[key] = true

		file, line := "", 0
		if n, nerr := callStore.GetNode(ctx, call.CallerNodeID); nerr == nil {
			file = n.FilePath
			line = n.StartLine
		}
		out = append(out, coordinated.ConsumerRef{
			Repo:   lnk.CallRepo,
			CallID: lnk.CallID,
			NodeID: call.CallerNodeID,
			File:   file,
			Line:   line,
		})
	}
	return out, firstErr
}

var _ WorkspaceLinkPort = (*store.Workspace)(nil)
