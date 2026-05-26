package parser

import (
	"strings"
	"testing"
)

func TestGoTagsQueryNonEmpty(t *testing.T) {
	if strings.TrimSpace(goTagsQuery) == "" {
		t.Fatal("goTagsQuery is empty; //go:embed queries/go.scm did not load")
	}
}

func TestGoTagsQueryDeclaresCaptures(t *testing.T) {
	wants := []string{
		"@definition.function",
		"@definition.method",
		"@definition.struct",
		"@definition.interface",
		"@definition.type",
		"@definition.field",
		"@name",
	}
	for _, w := range wants {
		if !strings.Contains(goTagsQuery, w) {
			t.Errorf("go.scm missing capture %q", w)
		}
	}
}

func TestGoTagsQueryTargetsGoNodeTypes(t *testing.T) {
	wants := []string{
		"function_declaration",
		"method_declaration",
		"type_spec",
		"struct_type",
		"interface_type",
		"field_declaration",
	}
	for _, w := range wants {
		if !strings.Contains(goTagsQuery, w) {
			t.Errorf("go.scm does not target node type %q", w)
		}
	}
}
