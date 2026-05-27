// SPDX-License-Identifier: MIT
// Package extract is the cross-language API-contract extractor seam:
// the registry of per-(Language, framework) RouteExtractor impls that turn
// source files into store.APIEndpoint + store.APICall rows. This package OWNS
// the contract surface (master C-4) — the RouteExtractor interface, the
// Registry, the Language + StubReference value types, the sentinel errors.
// Concrete extractor impls live under per-framework subpackages (proto/,
// gohttp/{chi,gin,echo,stdlib}/, python/fastapi/, typescript/{nextjs,nestjs}/)
// and land in (W3 Go-stack) + (W3 Python/TS-stack).
//
// Boundary: this package and its
// subpackages NEVER import internal/store; bridge only via the
// internal/caronte/store package (where added the APIEndpoint +
// APICall types). The federation store (internal/caronte/store/federation)
// and the Coordinator (internal/caronte/coordinated) are downstream;
// this package never imports either.
//
// CGO posture: the registry + types + errors are CGO-AGNOSTIC (pure interface +
// struct + sentinel errors); the package compiles trivially under !cgo. The
// RouteExtractor interface declares *parser.Tree which is a type-alias to
// *sitter.Tree under cgo and an opaque struct under !cgo (see
// internal/caronte/parser/types.go + types_nocgo.go) — the interface declaration
// compiles under both tags, the !cgo extractors return ErrCGODisabled before
// constructing any tree.
package extract

type Language string

const (
	LangProto      Language = "proto"
	LangGo         Language = "go"
	LangPython     Language = "python"
	LangTypeScript Language = "typescript"
	LangJavaScript Language = "javascript"
)

// StubReference is the value type a gRPC-aware extractor returns from
// StubArtifacts() to surface the generated-stub import linking the client repo
// to the server's.proto package. Each field is a string identifier — together
// they uniquely identify the RPC the client is calling, enabling the
// highest-confidence cross-repo link tier (exact_proto_import per spec §6 +
// master C-5). A zero value (all-empty fields) means "no stub import found at
// this site"; downstream consumers check the individual fields
// rather than the value's address.
//
// Master C-4 froze the four-field shape; do not add fields without amending
// the master and rerunning review across every downstream consumer.
type StubReference struct {
	Repo string

	ProtoPackage string

	ServiceName string

	RpcName string
}
