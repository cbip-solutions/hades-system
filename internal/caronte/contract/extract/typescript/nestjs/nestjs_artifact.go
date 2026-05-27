// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package nestjs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type swaggerDoc struct {
	Paths map[string]map[string]swaggerOperation `json:"paths" yaml:"paths"`
}

type swaggerOperation struct {
	OperationID string `json:"operationId" yaml:"operationId"`
	Summary     string `json:"summary" yaml:"summary"`
}

var artifactCandidates = []string{
	"swagger.json",
	"swagger.yaml",
	"swagger.yml",
}

var swaggerHTTPMethods = map[string]bool{
	"get":     true,
	"put":     true,
	"post":    true,
	"delete":  true,
	"options": true,
	"head":    true,
	"patch":   true,
	"trace":   true,
}

func (e *Extractor) endpointsFromArtifact(repoRoot, repo string) ([]store.APIEndpoint, bool) {
	for _, name := range artifactCandidates {
		full := filepath.Join(repoRoot, name)
		st, err := os.Stat(full)
		if err != nil || st.IsDir() {
			continue
		}

		body, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		doc, err := decodeSwagger(body, filepath.Ext(name))
		if err != nil {

			return nil, false
		}
		return artifactToEndpoints(doc, full, repo), true
	}
	return nil, false
}

func decodeSwagger(body []byte, ext string) (*swaggerDoc, error) {
	var doc swaggerDoc
	switch strings.ToLower(ext) {
	case ".json":
		if err := json.Unmarshal(body, &doc); err != nil {
			return nil, fmt.Errorf("swagger.json: %w", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(body, &doc); err != nil {
			return nil, fmt.Errorf("swagger.yaml: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported artifact extension %q", ext)
	}
	return &doc, nil
}

func artifactToEndpoints(doc *swaggerDoc, artifactPath, repo string) []store.APIEndpoint {
	if doc == nil || len(doc.Paths) == 0 {
		return []store.APIEndpoint{}
	}
	now := extractedAtFn()
	eps := make([]store.APIEndpoint, 0, len(doc.Paths))
	for path, methods := range doc.Paths {
		canon := canonicalisePath(path)
		for method, op := range methods {
			lower := strings.ToLower(method)
			if !swaggerHTTPMethods[lower] {
				continue
			}
			handler := op.OperationID
			if handler == "" {
				handler = fmt.Sprintf("%s:%s", strings.ToUpper(method), canon)
			}
			eps = append(eps, store.APIEndpoint{
				EndpointID:       fmt.Sprintf("%s:%s:%s", repo, strings.ToUpper(method), canon),
				Repo:             repo,
				Kind:             "http",
				Method:           strings.ToUpper(method),
				PathTemplate:     canon,
				HandlerNodeID:    handler,
				ContractArtifact: artifactPath,
				ExtractedAt:      now,
				ExtractorID:      ExtractorID,
			})
		}
	}
	return eps
}
