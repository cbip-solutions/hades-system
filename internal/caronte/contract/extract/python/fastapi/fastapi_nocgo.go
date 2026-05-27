//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package fastapi

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/parser"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

const ExtractorID = "python.fastapi-v1"

type Extractor struct{}

func New() *Extractor { return &Extractor{} }

func init() {}

func (e *Extractor) Language() extract.Language { return extract.LangPython }
func (e *Extractor) Frameworks() []string       { return []string{"fastapi"} }
func (e *Extractor) Detect(string, []byte) bool { return false }

func (e *Extractor) Endpoints(*parser.Tree, string) ([]store.APIEndpoint, error) {
	return nil, nil
}

func (e *Extractor) Calls(*parser.Tree, string) ([]store.APICall, error) { return nil, nil }

func (e *Extractor) StubArtifacts(string, []byte) []extract.StubReference {
	return nil
}

func (e *Extractor) EndpointsFromBytes(_ context.Context, file string, src []byte, repo, repoRoot string) ([]store.APIEndpoint, error) {
	return nil, nil
}
