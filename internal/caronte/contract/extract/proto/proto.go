// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package proto

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	protogrammar "github.com/smacker/go-tree-sitter/protobuf"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/parser"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

const extractorID = "proto-v1"

func init() {
	if err := extract.Default().Register("proto", New()); err != nil {
		panic(fmt.Sprintf("proto extractor: Register: %v", err))
	}
}

type Extractor struct {
	lang  *sitter.Language
	query *sitter.Query
}

func New() *Extractor {
	lang := protogrammar.GetLanguage()
	q, err := sitter.NewQuery([]byte(protoTagsQuery), lang)
	if err != nil {
		panic(fmt.Sprintf("proto extractor: NewQuery: %v", err))
	}
	return &Extractor{lang: lang, query: q}
}

func (e *Extractor) Language() extract.Language { return extract.LangProto }

func (e *Extractor) Frameworks() []string { return []string{"proto"} }

func (e *Extractor) Detect(file string, content []byte) bool {
	return strings.EqualFold(filepath.Ext(file), ".proto")
}

// Endpoints satisfies the C-4 RouteExtractor.Endpoints contract. Walks the
// parsed tree (smacker *sitter.Tree, aliased to *parser.Tree via
// internal/caronte/parser/types.go) for service/rpc declarations and emits
// one APIEndpoint{Kind:"grpc"} per rpc + an optional sibling HTTP row when
// the rpc carries a `option (google.api.http)` annotation.
//
// The C-4 surface intentionally omits a `repo` parameter — the daemon's
// ingestion path supplies it via EndpointsFromBytes; the C-4 wrapper defaults
// Repo to "" so a registry-driven Resolve+Endpoints loop still works on a
// .proto file that arrived without workspace context (tests + tooling).
// Production callers SHOULD prefer EndpointsFromBytes to set the canonical
// Repo / per-file context.
//
// content is re-derived from the tree (the smacker tree carries no source
// bytes by itself; the source is needed for `node.Content`). The C-4 contract
// does NOT pass content alongside the tree — ingestion path passes
// content through a per-file context — so Endpoints' file argument is used
// only as ContractArtifact + a deferred read of bytes is unavailable; the
// caller MUST have produced the tree via this package's parseTree (which
// registers the source bytes in the parsedSources side-channel). A tree
// constructed externally via raw sitter.NewParser will surface
// ErrTreeNotRegistered — debug-from-hell prevention. Production callers
// SHOULD use EndpointsFromBytes which holds the source explicitly.
//
// A nil tree degrades to (nil, nil) — the registry's empty-resolution case.
func (e *Extractor) Endpoints(tree *parser.Tree, file string) ([]store.APIEndpoint, error) {
	if tree == nil {
		return nil, nil
	}

	content, ok := lookupParsedSource(tree)
	if !ok {
		return nil, ErrTreeNotRegistered
	}
	return e.endpointsFromTree(context.Background(), tree, file, content, "")
}

func (e *Extractor) EndpointsFromBytes(ctx context.Context, file string, content []byte, repo string) ([]store.APIEndpoint, error) {
	tree, err := e.parseTree(ctx, content)
	if err != nil {
		return nil, fmt.Errorf("proto extractor: parse %q: %w", file, err)
	}
	defer tree.Close()
	return e.endpointsFromTree(ctx, tree, file, content, repo)
}

func (e *Extractor) endpointsFromTree(_ context.Context, tree *parser.Tree, file string, content []byte, repo string) ([]store.APIEndpoint, error) {
	root := tree.RootNode()
	pkg := extractProtoPackage(root, content)

	var eps []store.APIEndpoint
	nowExtractedAt := nowSeconds()

	for i := 0; i < int(root.NamedChildCount()); i++ {
		svc := root.NamedChild(i)
		if svc == nil || svc.Type() != "service" {
			continue
		}
		serviceName := childTextByType(svc, "service_name", content)
		if serviceName == "" {
			continue
		}

		for j := 0; j < int(svc.NamedChildCount()); j++ {
			rpcNode := svc.NamedChild(j)
			if rpcNode == nil || rpcNode.Type() != "rpc" {
				continue
			}
			rpcName := childTextByType(rpcNode, "rpc_name", content)
			if rpcName == "" {
				continue
			}
			handlerNodeID := composeHandlerNodeID(pkg, serviceName, rpcName)
			eps = append(eps, store.APIEndpoint{
				EndpointID:       fmt.Sprintf("%s:grpc:%s/%s", repo, handlerNodeID, rpcName),
				Repo:             repo,
				Kind:             "grpc",
				ProtoService:     serviceName,
				ProtoRPC:         rpcName,
				HandlerNodeID:    handlerNodeID,
				ContractArtifact: file,
				ExtractedAt:      nowExtractedAt,
				ExtractorID:      extractorID,
			})

			if httpMethod, httpPath := parseGoogleAPIHttpOption(rpcNode, content); httpMethod != "" && httpPath != "" {
				normalized := normalizePathTemplate(httpPath)
				eps = append(eps, store.APIEndpoint{
					EndpointID:       fmt.Sprintf("%s:http:%s %s", repo, httpMethod, normalized),
					Repo:             repo,
					Kind:             "http",
					Method:           httpMethod,
					PathTemplate:     normalized,
					HandlerNodeID:    handlerNodeID,
					ContractArtifact: file,
					ExtractedAt:      nowExtractedAt,
					ExtractorID:      extractorID,
				})
			}
		}
	}
	return eps, nil
}

func childTextByType(n *sitter.Node, typ string, content []byte) string {
	if n == nil {
		return ""
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if c == nil {
			continue
		}
		if c.Type() == typ {
			return c.Content(content)
		}
	}
	return ""
}

func composeHandlerNodeID(pkg, serviceName, rpcName string) string {
	if pkg == "" {
		return fmt.Sprintf("%s.%s", serviceName, rpcName)
	}
	return fmt.Sprintf("%s.%s.%s", pkg, serviceName, rpcName)
}

func (e *Extractor) Calls(tree *parser.Tree, file string) ([]store.APICall, error) {
	return nil, nil
}

func (e *Extractor) StubArtifacts(file string, content []byte) []extract.StubReference {
	switch {
	case strings.HasSuffix(file, "_grpc.pb.go"):
		return goGrpcStubRefs(file, content)
	case strings.HasSuffix(file, "_pb2_grpc.py"):
		return pyGrpcStubRefs(file, content)
	case strings.HasSuffix(file, "_grpc_web_pb.js"):
		return jsGrpcStubRefs(file, content)
	default:
		return nil
	}
}

const protoTagsQuery = `
(service
  (service_name) @service.name) @service.def

(rpc
  (rpc_name) @rpc.name) @rpc
`

func extractProtoPackage(root *sitter.Node, content []byte) string {
	for i := 0; i < int(root.NamedChildCount()); i++ {
		n := root.NamedChild(i)
		if n == nil {
			continue
		}
		if n.Type() != "package" {
			continue
		}

		for j := 0; j < int(n.NamedChildCount()); j++ {
			c := n.NamedChild(j)
			if c == nil {
				continue
			}
			if c.Type() == "full_ident" {
				return c.Content(content)
			}
		}
	}
	return ""
}

func parseGoogleAPIHttpOption(rpcNode *sitter.Node, content []byte) (method, path string) {
	if rpcNode == nil {
		return "", ""
	}

	var stack []*sitter.Node
	for i := 0; i < int(rpcNode.NamedChildCount()); i++ {
		stack = append(stack, rpcNode.NamedChild(i))
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}
		if n.Type() == "option" {
			text := n.Content(content)
			if strings.Contains(text, "google.api.http") {
				return scanHttpOption(text)
			}
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			stack = append(stack, n.NamedChild(i))
		}
	}
	return "", ""
}

func scanHttpOption(text string) (method, path string) {
	matches := httpOptionVerbRE.FindStringSubmatch(text)
	if len(matches) < 3 {
		return "", ""
	}
	verb := strings.ToUpper(matches[1])
	if verb == "CUSTOM" {

		return "", ""
	}
	return verb, matches[2]
}

var httpOptionVerbRE = regexp.MustCompile(`(?i)\b(get|post|put|delete|patch|head|options|custom)\s*:\s*"([^"]+)"`)

func normalizePathTemplate(p string) string {
	return paramSyntaxRE.ReplaceAllString(p, "{param}")
}

var paramSyntaxRE = regexp.MustCompile(`\{[^}]*\}|:[A-Za-z_][A-Za-z0-9_]*|\[[^\]]+\]|\$\{[^}]*\}|<[^>]+>`)

var nowSeconds = func() int64 { return realNowSeconds() }

func goGrpcStubRefs(file string, content []byte) []extract.StubReference {
	pkg := goProtoPkgRE.FindSubmatch(content)
	if pkg == nil {
		return nil
	}
	protoPkg := string(pkg[1])
	var refs []extract.StubReference
	repoGuess := file
	for _, m := range goServerIfaceRE.FindAllSubmatch(content, -1) {
		serviceName := string(m[1])
		body := string(m[2])
		for _, rm := range goServerMethodRE.FindAllStringSubmatch(body, -1) {
			methodName := rm[1]

			if methodName == "" || !unicode.IsUpper(rune(methodName[0])) {
				continue
			}
			refs = append(refs, extract.StubReference{
				Repo:         repoGuess,
				ProtoPackage: protoPkg,
				ServiceName:  serviceName,
				RpcName:      methodName,
			})
		}
	}
	return refs
}

var (
	goProtoPkgRE = regexp.MustCompile(`(?m)^\s*(?:proto|api|spec|pb)\s+"([^"]+)"`)

	goServerIfaceRE = regexp.MustCompile(`(?s)type\s+(\w+)Server\s+interface\s*\{(.*?)\}`)

	goServerMethodRE = regexp.MustCompile(`(?m)^\s*(\w+)\s*\(`)
)

func pyGrpcStubRefs(file string, content []byte) []extract.StubReference {
	pkg := pyProtoPkgRE.FindSubmatch(content)
	if pkg == nil {
		return nil
	}
	protoPkg := string(pkg[1])
	var refs []extract.StubReference
	repoGuess := file
	for _, m := range pyServicerRE.FindAllSubmatch(content, -1) {
		serviceName := string(m[1])
		body := string(m[2])
		for _, rm := range pyServicerMethodRE.FindAllStringSubmatch(body, -1) {
			refs = append(refs, extract.StubReference{
				Repo:         repoGuess,
				ProtoPackage: protoPkg,
				ServiceName:  serviceName,
				RpcName:      rm[1],
			})
		}
	}
	return refs
}

var (
	pyProtoPkgRE = regexp.MustCompile(`(?m)^import\s+(\w+)_pb2(?:\s+as\s+\w+)?\s*$`)

	pyServicerRE       = regexp.MustCompile(`(?s)class\s+(\w+)Servicer\s*\(\s*object\s*\)\s*:(.*?)(?:^class\s|\z)`)
	pyServicerMethodRE = regexp.MustCompile(`(?m)^\s+def\s+(\w+)\s*\(\s*self`)
)

func jsGrpcStubRefs(file string, content []byte) []extract.StubReference {
	pkg := jsProtoRequireRE.FindSubmatch(content)
	if pkg == nil {
		return nil
	}
	protoPkg := string(pkg[1])
	var refs []extract.StubReference
	repoGuess := file
	for _, m := range jsServiceMethodRE.FindAllSubmatch(content, -1) {
		serviceName := string(m[1])
		rpcName := upperFirst(string(m[2]))
		refs = append(refs, extract.StubReference{
			Repo:         repoGuess,
			ProtoPackage: protoPkg,
			ServiceName:  serviceName,
			RpcName:      rpcName,
		})
	}
	return refs
}

var (
	jsProtoRequireRE  = regexp.MustCompile(`(?m)require\s*\(\s*['"]([^'"]+)['"]\s*\)`)
	jsServiceMethodRE = regexp.MustCompile(`(?m)^\s*(\w+)Client\.prototype\.(\w+)\s*=`)
)

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}
