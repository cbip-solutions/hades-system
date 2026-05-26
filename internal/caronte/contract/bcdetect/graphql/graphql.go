// SPDX-License-Identifier: MIT
// Package graphql — Plan 20 GraphQL breaking-change detector wrapping
// vektah/gqlparser/v2 (MIT). The sole Go SDL diff library used here per
// inv-zen-267; the imports scan asserts no other diff library is imported
// in this file (except the gated Node fallback in nodefallback.go).
//
// Rule set: the six canonical SDL diff rules from the graphql-inspector
// well-known catalog (Stage-0 divergence #3 — the spec §7 explicit
// enumeration). Anything outside this set yields SevInsufficient with
// Kind = "INSUFFICIENT_<reason>" so the Node fallback (nodefallback.go)
// can take over under the inv-zen-272 opt-in gate.
//
// Severity mapping:
//
//   - FIELD_REMOVED: SevBreaking (consumer queries fail with "field
//     unknown" error).
//   - FIELD_ARGUMENT_TYPE_CHANGED: SevBreaking (existing queries with the
//     old type fail validation).
//   - TYPE_REMOVED: SevBreaking (every consumer reference fails).
//   - ENUM_VALUE_REMOVED: SevBreaking (consumers using the value lose
//     semantic meaning).
//   - INPUT_FIELD_ADDED_REQUIRED: SevBreaking (existing mutations missing
//     the field fail validation).
//   - DIRECTIVE_USAGE_REMOVED: SevDangerous (consumers depending on the
//     directive's side effect lose it, but query parsing succeeds).
//
// Concurrency Detect is safe to call concurrently.
package graphql

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"

	br "github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect"
)

type Rule string

const (
	RuleFieldRemoved             Rule = "FIELD_REMOVED"
	RuleFieldArgumentTypeChanged Rule = "FIELD_ARGUMENT_TYPE_CHANGED"
	RuleTypeRemoved              Rule = "TYPE_REMOVED"
	RuleEnumValueRemoved         Rule = "ENUM_VALUE_REMOVED"
	RuleInputFieldAddedRequired  Rule = "INPUT_FIELD_ADDED_REQUIRED"
	RuleDirectiveUsageRemoved    Rule = "DIRECTIVE_USAGE_REMOVED"
)

func CanonicalRules() []Rule {
	return []Rule{
		RuleFieldRemoved, RuleFieldArgumentTypeChanged, RuleTypeRemoved,
		RuleEnumValueRemoved, RuleInputFieldAddedRequired, RuleDirectiveUsageRemoved,
	}
}

type GraphQLDetector struct {
	params br.Params
}

func NewGraphQLDetector(p br.Params) *GraphQLDetector {
	return &GraphQLDetector{params: p}
}

func (d *GraphQLDetector) DetectorID() string { return "gqlparser" }

func (d *GraphQLDetector) Detect(ctx context.Context, oldSpec, newSpec []byte) ([]br.DiffResult, error) {
	if len(oldSpec) > d.params.MaxSpecBytes || len(newSpec) > d.params.MaxSpecBytes {
		return nil, br.ErrSpecTooLarge
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	oldSchema, oldErr := gqlparser.LoadSchema(&ast.Source{Name: "old.graphql", Input: string(oldSpec)})
	if oldErr != nil {
		return nil, fmt.Errorf("%w: load old SDL: %v", br.ErrInvalidSpec, oldErr)
	}
	newSchema, newErr := gqlparser.LoadSchema(&ast.Source{Name: "new.graphql", Input: string(newSpec)})
	if newErr != nil {
		return nil, fmt.Errorf("%w: load new SDL: %v", br.ErrInvalidSpec, newErr)
	}

	return classifyDiff(oldSchema, newSchema), nil
}

func classifyDiff(oldSchema, newSchema *ast.Schema) []br.DiffResult {
	results := []br.DiffResult{}

	for name, oldDef := range oldSchema.Types {
		if isBuiltin(name) {
			continue
		}
		newDef, ok := newSchema.Types[name]
		if !ok {

			results = append(results, mkResult(string(RuleTypeRemoved), br.SevBreaking, mustJSON(map[string]any{
				"type": name,
				"kind": string(oldDef.Kind),
			})))
			continue
		}

		if oldDef.Kind != newDef.Kind {

			results = append(results, mkResult("INSUFFICIENT_TYPE_KIND_CHANGED", br.SevInsufficient, mustJSON(map[string]any{
				"type": name,
				"old":  string(oldDef.Kind),
				"new":  string(newDef.Kind),
			})))
			continue
		}
		results = append(results, classifyTypeDefinition(name, oldDef, newDef)...)
	}

	for name, oldDir := range oldSchema.Directives {
		if isBuiltinDirective(name) {
			continue
		}
		newDir, ok := newSchema.Directives[name]
		if !ok {

			results = append(results, mkResult("INSUFFICIENT_DIRECTIVE_REMOVED", br.SevInsufficient, mustJSON(map[string]any{
				"directive": name,
			})))
			continue
		}
		results = append(results, classifyDirectiveArgs(name, oldDir, newDir)...)
	}

	return results
}

func classifyTypeDefinition(typeName string, oldDef, newDef *ast.Definition) []br.DiffResult {
	results := []br.DiffResult{}

	switch oldDef.Kind {
	case ast.Object, ast.Interface:
		results = append(results, classifyFields(typeName, oldDef.Fields, newDef.Fields)...)
	case ast.InputObject:
		results = append(results, classifyInputFields(typeName, oldDef.Fields, newDef.Fields)...)
	case ast.Enum:
		results = append(results, classifyEnumValues(typeName, oldDef.EnumValues, newDef.EnumValues)...)
	default:

	}

	results = append(results, classifyDirectiveUsageLoss(typeName, "type", oldDef.Directives, newDef.Directives)...)

	return results
}

func classifyFields(typeName string, oldFields, newFields ast.FieldList) []br.DiffResult {
	results := []br.DiffResult{}
	newByName := indexFields(newFields)
	for _, oldField := range oldFields {
		newField, ok := newByName[oldField.Name]
		if !ok {

			results = append(results, mkResult(string(RuleFieldRemoved), br.SevBreaking, mustJSON(map[string]any{
				"type":  typeName,
				"field": oldField.Name,
			})))
			continue
		}

		newArgsByName := indexArgs(newField.Arguments)
		for _, oldArg := range oldField.Arguments {
			newArg, ok := newArgsByName[oldArg.Name]
			if !ok {

				results = append(results, mkResult("INSUFFICIENT_FIELD_ARGUMENT_REMOVED", br.SevInsufficient, mustJSON(map[string]any{
					"type":     typeName,
					"field":    oldField.Name,
					"argument": oldArg.Name,
				})))
				continue
			}
			if !typesEqual(oldArg.Type, newArg.Type) {

				results = append(results, mkResult(string(RuleFieldArgumentTypeChanged), br.SevBreaking, mustJSON(map[string]any{
					"type":     typeName,
					"field":    oldField.Name,
					"argument": oldArg.Name,
					"old":      typeStr(oldArg.Type),
					"new":      typeStr(newArg.Type),
				})))
			}
		}

		results = append(results, classifyDirectiveUsageLoss(typeName+"."+oldField.Name, "field", oldField.Directives, newField.Directives)...)
	}
	return results
}

func classifyInputFields(typeName string, oldFields, newFields ast.FieldList) []br.DiffResult {
	results := []br.DiffResult{}
	oldByName := indexFields(oldFields)
	for _, newField := range newFields {
		if _, exists := oldByName[newField.Name]; exists {
			continue
		}

		if newField.Type.NonNull && newField.DefaultValue == nil {
			results = append(results, mkResult(string(RuleInputFieldAddedRequired), br.SevBreaking, mustJSON(map[string]any{
				"input": typeName,
				"field": newField.Name,
				"type":  typeStr(newField.Type),
			})))
		}
	}
	return results
}

func classifyEnumValues(typeName string, oldValues, newValues ast.EnumValueList) []br.DiffResult {
	results := []br.DiffResult{}
	newByName := map[string]struct{}{}
	for _, v := range newValues {
		newByName[v.Name] = struct{}{}
	}
	for _, oldValue := range oldValues {
		if _, ok := newByName[oldValue.Name]; !ok {
			results = append(results, mkResult(string(RuleEnumValueRemoved), br.SevBreaking, mustJSON(map[string]any{
				"enum":  typeName,
				"value": oldValue.Name,
			})))
		}
	}
	return results
}

func classifyDirectiveUsageLoss(location, kind string, oldDirs, newDirs ast.DirectiveList) []br.DiffResult {
	results := []br.DiffResult{}
	newByName := map[string]struct{}{}
	for _, d := range newDirs {
		newByName[d.Name] = struct{}{}
	}
	for _, oldDir := range oldDirs {
		if _, ok := newByName[oldDir.Name]; !ok {

			results = append(results, mkResult(string(RuleDirectiveUsageRemoved), br.SevDangerous, mustJSON(map[string]any{
				"location":  location,
				"kind":      kind,
				"directive": oldDir.Name,
			})))
		}
	}
	return results
}

func classifyDirectiveArgs(name string, oldDir, newDir *ast.DirectiveDefinition) []br.DiffResult {
	results := []br.DiffResult{}
	oldByName := map[string]struct{}{}
	for _, a := range oldDir.Arguments {
		oldByName[a.Name] = struct{}{}
	}
	for _, newArg := range newDir.Arguments {
		if _, exists := oldByName[newArg.Name]; exists {
			continue
		}

		results = append(results, mkResult("INSUFFICIENT_DIRECTIVE_ARGUMENT_ADDED", br.SevInsufficient, mustJSON(map[string]any{
			"directive": name,
			"argument":  newArg.Name,
			"type":      typeStr(newArg.Type),
			"required":  newArg.Type.NonNull && newArg.DefaultValue == nil,
		})))
	}
	return results
}

func indexFields(fields ast.FieldList) map[string]*ast.FieldDefinition {
	out := map[string]*ast.FieldDefinition{}
	for _, f := range fields {
		out[f.Name] = f
	}
	return out
}

func indexArgs(args ast.ArgumentDefinitionList) map[string]*ast.ArgumentDefinition {
	out := map[string]*ast.ArgumentDefinition{}
	for _, a := range args {
		out[a.Name] = a
	}
	return out
}

func typesEqual(a, b *ast.Type) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.NamedType != b.NamedType || a.NonNull != b.NonNull {
		return false
	}
	return typesEqual(a.Elem, b.Elem)
}

func typeStr(t *ast.Type) string {
	if t == nil {
		return ""
	}
	if t.Elem != nil {
		s := "[" + typeStr(t.Elem) + "]"
		if t.NonNull {
			return s + "!"
		}
		return s
	}
	if t.NonNull {
		return t.NamedType + "!"
	}
	return t.NamedType
}

func isBuiltin(name string) bool {
	switch name {
	case "Int", "Float", "String", "Boolean", "ID":
		return true
	}

	if len(name) >= 2 && name[0] == '_' && name[1] == '_' {
		return true
	}
	return false
}

func isBuiltinDirective(name string) bool {
	switch name {
	case "include", "skip", "deprecated", "specifiedBy":
		return true
	}
	return false
}

func mkResult(kind string, sev br.Severity, detail []byte) br.DiffResult {
	return br.DiffResult{
		DetectorID: "gqlparser",
		Kind:       kind,
		Severity:   sev,
		Detail:     detail,
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

var _ br.Detector = (*GraphQLDetector)(nil)
