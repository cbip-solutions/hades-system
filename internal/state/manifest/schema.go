// SPDX-License-Identifier: MIT
package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

type Schema struct {
	compiled *jsonschema.Schema
	raw      map[string]any
}

func LoadSchema(path string) (*Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrSchemaNotFound, path)
		}
		return nil, fmt.Errorf("%w: read %s: %v", ErrSchemaInvalid, path, err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: parse %s: %v", ErrSchemaInvalid, path, err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7
	if err := compiler.AddResource(path, bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("%w: add resource %s: %v", ErrSchemaInvalid, path, err)
	}
	compiled, err := compiler.Compile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: compile %s: %v", ErrSchemaInvalid, path, err)
	}

	return &Schema{compiled: compiled, raw: raw}, nil
}

func LoadManualFieldPaths(schemaPath string) ([]ManualFieldPath, error) {
	s, err := LoadSchema(schemaPath)
	if err != nil {
		return nil, err
	}
	return s.DiscoverManualFields()
}

func (s *Schema) Validate(v any) error {
	if s == nil || s.compiled == nil {
		return errors.New("manifest: nil schema")
	}
	if err := s.compiled.Validate(v); err != nil {
		return fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	return nil
}

func (s *Schema) DiscoverManualFields() ([]ManualFieldPath, error) {
	if s == nil || s.raw == nil {
		return nil, errors.New("manifest: nil schema")
	}
	var paths []ManualFieldPath
	walkProperties(s.raw, "", func(path string, leaf map[string]any) {
		if v, ok := leaf["x-manual-field"]; ok {
			if b, ok := v.(bool); ok && b {
				paths = append(paths, ManualFieldPath{Path: path})
			}
		}
	})
	sort.Slice(paths, func(i, j int) bool { return paths[i].Path < paths[j].Path })
	return paths, nil
}

func (s *Schema) DiscoverAutoSources() ([]AutoSourceMapping, error) {
	if s == nil || s.raw == nil {
		return nil, errors.New("manifest: nil schema")
	}
	var srcs []AutoSourceMapping
	walkProperties(s.raw, "", func(path string, leaf map[string]any) {
		if v, ok := leaf["x-source"]; ok {
			if str, ok := v.(string); ok && str != "" {
				srcs = append(srcs, AutoSourceMapping{Path: path, Source: str})
			}
		}
	})
	sort.Slice(srcs, func(i, j int) bool { return srcs[i].Path < srcs[j].Path })
	return srcs, nil
}

func walkProperties(node map[string]any, prefix string, visit func(path string, leaf map[string]any)) {
	props, ok := node["properties"].(map[string]any)
	if !ok {
		return
	}
	for name, raw := range props {
		child, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		visit(path, child)
		if t, ok := child["type"].(string); ok && t == "object" {
			walkProperties(child, path, visit)
		}
	}
}
