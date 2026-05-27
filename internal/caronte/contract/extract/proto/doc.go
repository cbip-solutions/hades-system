// SPDX-License-Identifier: MIT
// Package proto is the release gRPC +.proto IDL extractor namespace
// (Language: LangProto). The concrete RouteExtractor implementation lands in
// (master row D, wave W3) as proto.go alongside fixtures under
// fixtures/. The (LangProto, "grpc") tuple is reserved for this package by
// daemon-time Register() call; until then this package holds the
// import-path namespace and the doc-comment contract.
//
// What will add:
// - proto.go: NewExtractor() *Extractor; (*Extractor).Language() / Frameworks() /
// Detect() / Endpoints() / Calls() / StubArtifacts() — RouteExtractor impl
// for.proto files (service/rpc + the option (google.api.http) REST
// transcoding) and gRPC client-side generated stubs (*_grpc.pb.go /
// *_pb2_grpc.py / *_grpc_web_pb.js — the StubArtifacts() return is the
// load-bearing exact_proto_import linking tier per master C-5 / spec §6).
// - proto_test.go: per-fixture extraction tests + StubArtifacts roundtrip
// tests (≥10 fixtures per spec §13.1).
// - fixtures/: real.proto + generated-stub source pairs from the SOTA
// research catalog.
//
// Boundary this package and code under it MUST NOT import
// internal/store; the only store access is
// via internal/caronte/store APIEndpoint / APICall types provides.
package proto
