// SPDX-License-Identifier: MIT
package yaml

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	yaml3 "gopkg.in/yaml.v3"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

func Load(path string, roster []string) (*Manifest, error) {

	b, err := os.ReadFile(path)
	if err != nil {

		return nil, fmt.Errorf("caronte/yaml: open %s: %w", path, err)
	}

	if err := walkAndValidateInlineSecretsBytes(b, path); err != nil {
		return nil, err
	}

	dec := yaml3.NewDecoder(bytesReader(b))
	dec.KnownFields(true)
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("caronte/yaml: decode %s: %w", path, err)
	}

	if m.UnresolvedPolicy == "" {
		m.UnresolvedPolicy = DefaultUnresolvedPolicy
	}

	if err := validateSchemaVersion(m.SchemaVersion); err != nil {
		return nil, err
	}

	m.compiled = make([]*regexp.Regexp, len(m.Services))

	for i, s := range m.Services {
		if err := validateBaseURLExclusive(s); err != nil {
			return nil, fmt.Errorf("services[%d]: %w", i, err)
		}
		if err := validateTargetRepo(s.TargetRepo, roster); err != nil {
			return nil, fmt.Errorf("services[%d]: %w", i, err)
		}
		if s.BaseURLPattern != "" {
			if err := validatePatternRunes(s.BaseURLPattern); err != nil {
				return nil, fmt.Errorf("services[%d]: %w", i, err)
			}
			if err := validatePatternRegexDoS(s.BaseURLPattern); err != nil {
				return nil, fmt.Errorf("services[%d]: %w", i, err)
			}

			re, err := regexp.Compile(s.BaseURLPattern)
			if err != nil {
				return nil, fmt.Errorf("caronte/yaml: services[%d]: compile base_url_pattern: %w", i, err)
			}
			m.compiled[i] = re
		}
	}

	if err := validateUnresolvedPolicy(m.UnresolvedPolicy); err != nil {
		return nil, err
	}
	return &m, nil
}

func walkAndValidateInlineSecretsBytes(b []byte, path string) error {
	var root yaml3.Node
	if err := yaml3.Unmarshal(b, &root); err != nil {

		return nil
	}
	keys := collectMappingKeys(&root)
	if len(keys) == 0 {
		return nil
	}
	fields := make(map[string]string, len(keys))
	for _, k := range keys {
		fields[k] = ""
	}
	if err := validateInlineSecrets(fields); err != nil {

		return fmt.Errorf("caronte/yaml: secret-walk %s: %w", path, err)
	}
	return nil
}

func collectMappingKeys(n *yaml3.Node) []string {
	if n == nil {
		return nil
	}
	var out []string
	switch n.Kind {
	case yaml3.MappingNode:

		for i := 0; i+1 < len(n.Content); i += 2 {
			out = append(out, n.Content[i].Value)
			out = append(out, collectMappingKeys(n.Content[i+1])...)
		}
	case yaml3.SequenceNode, yaml3.DocumentNode:
		for _, c := range n.Content {
			out = append(out, collectMappingKeys(c)...)
		}
	}
	return out
}

func LoadAll(workspaceRoot string, repos []string) (map[string]*Manifest, error) {
	out := make(map[string]*Manifest, len(repos))
	for _, repo := range repos {
		path := filepath.Join(workspaceRoot, repo, "caronte.yaml")
		m, err := Load(path, repos)
		switch {
		case err == nil:
			out[repo] = m
		case errors.Is(err, os.ErrNotExist):

			continue
		default:
			return out, fmt.Errorf("caronte/yaml: LoadAll: repo %q: %w", repo, err)
		}
	}
	return out, nil
}
