// SPDX-License-Identifier: MIT
//
// Real compilable Go (no third-party deps so the parent module's
// tests can run on a CI without grpc-go installed). The K-4 test
// does NOT execute this; it reads the .proto file via the proto
// extractor's EndpointsFromBytes path + the *_grpc.pb.go stub
// via StubArtifacts for the link resolution.
package main

import (
	"context"
	"log"
)

type GreeterRequest struct{ Name string }
type GreeterReply struct{ Message string }

type greeterServer struct{}

func (greeterServer) Hello(_ context.Context, req *GreeterRequest) (*GreeterReply, error) {
	return &GreeterReply{Message: "hello " + req.Name}, nil
}

func main() {
	log.Println("greeter server (fixture only — does not actually serve)")
}
