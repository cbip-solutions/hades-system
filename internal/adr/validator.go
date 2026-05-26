// SPDX-License-Identifier: MIT
package adr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
	_ "github.com/santhosh-tekuri/jsonschema/v5/httploader"
)

type Validator struct {
	schema     *jsonschema.Schema
	schemaPath string
}

func NewValidator(path string) (*Validator, error) {
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft7
	s, err := c.Compile(path)
	if err != nil {
		return nil, fmt.Errorf("adr: compile schema %s: %w", path, err)
	}
	return &Validator{schema: s, schemaPath: path}, nil
}

func (v *Validator) ParseAndValidate(path string, body []byte) (*ADR, error) {
	a, err := Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("adr: parse %s: %w", path, err)
	}
	a.Path = path
	if err := v.ValidateOne(context.Background(), a); err != nil {
		return nil, err
	}
	return a, nil
}

func (v *Validator) ValidateOne(ctx context.Context, a *ADR) error {
	_ = ctx

	doc := marshalFrontmatterToDoc(a.Frontmatter)

	if err := v.schema.Validate(doc); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrSchemaViolation, a.Path, err)
	}
	return nil
}

func marshalFrontmatterToDoc(fm Frontmatter) interface{} {
	raw, _ := json.Marshal(fm)
	var doc interface{}
	_ = json.Unmarshal(raw, &doc)
	return doc
}

func (v *Validator) ValidateAll(ctx context.Context, adrs []*ADR) error {
	if len(adrs) == 0 {
		return nil
	}

	var schemaErrs []error
	for _, a := range adrs {
		if err := v.ValidateOne(ctx, a); err != nil {
			schemaErrs = append(schemaErrs, err)
		}
	}
	if len(schemaErrs) > 0 {
		return &ValidationError{errs: schemaErrs}
	}

	if err := checkIDUniqueness(adrs); err != nil {
		return err
	}

	if err := detectSupersedeCycle(adrs); err != nil {
		return err
	}

	return nil
}

func checkIDUniqueness(adrs []*ADR) error {
	seen := make(map[string]string, len(adrs))
	for _, a := range adrs {
		id := a.Frontmatter.ID
		if id == "" {
			continue
		}
		if first, dup := seen[id]; dup {
			return fmt.Errorf("%w: id %q claimed by both %s and %s",
				ErrIDCollision, id, first, a.Path)
		}
		seen[id] = a.Path
	}
	return nil
}

func detectSupersedeCycle(adrs []*ADR) error {

	byID := make(map[string]*ADR, len(adrs))
	for _, a := range adrs {
		if a.Frontmatter.ID != "" {
			byID[a.Frontmatter.ID] = a
		}
	}

	index := 0
	stack := []string{}
	onStack := map[string]bool{}
	indices := map[string]int{}
	lowlink := map[string]int{}
	var cycles [][]string

	var strongconnect func(id string)
	strongconnect = func(id string) {
		indices[id] = index
		lowlink[id] = index
		index++
		stack = append(stack, id)
		onStack[id] = true

		a := byID[id]
		if a != nil && a.Frontmatter.SupersededBy != "" {
			target := a.Frontmatter.SupersededBy
			if _, inCorpus := byID[target]; inCorpus {
				if _, visited := indices[target]; !visited {
					strongconnect(target)
					if lowlink[target] < lowlink[id] {
						lowlink[id] = lowlink[target]
					}
				} else if onStack[target] {
					if indices[target] < lowlink[id] {
						lowlink[id] = indices[target]
					}
				}
			}
		}

		if lowlink[id] == indices[id] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == id {
					break
				}
			}

			isCycle := len(scc) > 1
			if !isCycle && len(scc) == 1 {

				if node := byID[scc[0]]; node != nil && node.Frontmatter.SupersededBy == scc[0] {
					isCycle = true
				}
			}
			if isCycle {
				cycles = append(cycles, scc)
			}
		}
	}

	for _, a := range adrs {
		id := a.Frontmatter.ID
		if id == "" {
			continue
		}
		if _, visited := indices[id]; !visited {
			strongconnect(id)
		}
	}

	if len(cycles) > 0 {
		var sb strings.Builder
		for i, scc := range cycles {
			if i > 0 {
				sb.WriteString("; ")
			}
			sb.WriteString(strings.Join(scc, " → "))
		}
		return fmt.Errorf("%w: %s", ErrSupersedeCycle, sb.String())
	}
	return nil
}

type ValidationError struct {
	errs []error
}

func (ve *ValidationError) Error() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d schema violation(s):", len(ve.errs))
	for _, e := range ve.errs {
		sb.WriteString("\n  - ")
		sb.WriteString(e.Error())
	}
	return sb.String()
}

func (ve *ValidationError) Unwrap() []error {
	return ve.errs
}
