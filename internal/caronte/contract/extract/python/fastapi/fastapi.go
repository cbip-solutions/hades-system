// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package fastapi

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract"
	"github.com/cbip-solutions/hades-system/internal/caronte/parser"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

const ExtractorID = "python.fastapi-v1"

const extractorName = "python.fastapi"

type Extractor struct{}

func New() *Extractor { return &Extractor{} }

func init() {
	if err := extract.Default().Register(extractorName, New()); err != nil {
		panic("caronte/contract/extract/python/fastapi: Register failed: " + err.Error())
	}
}

func (e *Extractor) Language() extract.Language { return extract.LangPython }

// Frameworks reports the framework identifiers this extractor implements.
// staleness join + the registry's (Language, framework) collision
// check both consult this; do NOT add aliases here.
func (e *Extractor) Frameworks() []string { return []string{"fastapi"} }

func (e *Extractor) Detect(file string, content []byte) bool {
	if !strings.HasSuffix(file, ".py") {
		return false
	}

	return bytes.Contains(content, []byte("import fastapi")) ||
		bytes.Contains(content, []byte("from fastapi"))
}

func (e *Extractor) Endpoints(tree *parser.Tree, file string) ([]store.APIEndpoint, error) {
	return nil, nil
}

func (e *Extractor) Calls(tree *parser.Tree, file string) ([]store.APICall, error) {
	return nil, nil
}

func (e *Extractor) StubArtifacts(file string, content []byte) []extract.StubReference {
	return []extract.StubReference{}
}

func (e *Extractor) EndpointsFromBytes(ctx context.Context, file string, src []byte, repo, repoRoot string) ([]store.APIEndpoint, error) {

	if repoRoot != "" {
		eps, ok := e.endpointsFromArtifact(repoRoot, repo)
		if ok {
			return eps, nil
		}
	}

	return e.endpointsFromAST(ctx, file, src, repo)
}

var extractedAtFn = func() int64 { return time.Now().Unix() }

func canonicalisePath(p string) string {
	if p == "" {
		return ""
	}

	out := strings.Builder{}
	i := 0
	for i < len(p) {
		ch := p[i]
		if ch != '{' {
			out.WriteByte(ch)
			i++
			continue
		}

		end := strings.IndexByte(p[i:], '}')
		if end < 0 {

			out.WriteString(p[i:])
			break
		}
		end += i
		segment := p[i+1 : end]
		colon := strings.IndexByte(segment, ':')
		out.WriteByte('{')
		if colon >= 0 {
			out.WriteString(segment[:colon])
		} else {
			out.WriteString(segment)
		}
		out.WriteByte('}')
		i = end + 1
	}
	canon := out.String()

	for strings.Contains(canon, "//") {
		canon = strings.ReplaceAll(canon, "//", "/")
	}

	if len(canon) > 1 && strings.HasSuffix(canon, "/") {
		canon = strings.TrimRight(canon, "/")
	}
	return canon
}

func pyModulePath(filePath string) string {
	slash := strings.ReplaceAll(filePath, "\\", "/")
	dot := strings.LastIndex(slash, ".")
	var noExt string
	if dot < 0 {
		noExt = slash
	} else {
		noExt = slash[:dot]
	}

	if strings.HasSuffix(noExt, "/__init__") {
		noExt = strings.TrimSuffix(noExt, "/__init__")
	} else if noExt == "__init__" {
		return ""
	}
	return strings.ReplaceAll(noExt, "/", ".")
}

func pyNodeID(filePath, funcName string) string {
	mod := pyModulePath(filePath)
	if mod == "" {
		return funcName
	}
	return mod + "." + funcName
}
