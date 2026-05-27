// SPDX-License-Identifier: MIT
package link

import (
	"regexp"
	"strings"
)

// HTTPKey is the canonical normalized HTTP route key both extractors and the
// linker compare. Method is upper-cased; path is collapsed so every
// parameter syntax (chi/gin :id, Next.js [id], FastAPI {id:int}, OpenAPI
// {id}, Flask <id> or <int:id>, template ${id}) becomes the OpenAPI
// canonical `/path/{param}` form (parameter NAMES are erased — fuzzy match
// across naming conventions is the doctrine-default; a "static_path" link
// uses the literal pre-collapse form via paramNamesMatch in resolve.go).
//
// Output shape: `<UPPER-METHOD> <path>` (space-separated). Spec §6 D3 +
// §13.1 corpus drives this; sister-test to every extractor in + E
// (the link side MUST agree byte-for-byte on the key, otherwise every
// link silently misses).
func HTTPKey(method, rawPath string) string {
	return strings.ToUpper(method) + " " + collapseHTTPPath(rawPath)
}

func collapseHTTPPath(p string) string {
	if p == "" {
		return p
	}

	p = paramColonRE.ReplaceAllString(p, "/{param}")
	p = paramBracketRE.ReplaceAllString(p, "/{param}")
	p = paramBraceRE.ReplaceAllString(p, "/{param}")
	p = paramAngleRE.ReplaceAllString(p, "/{param}")
	p = paramDollarBraceRE.ReplaceAllString(p, "/{param}")
	return p
}

var (
	paramColonRE = regexp.MustCompile(`/:[A-Za-z_][A-Za-z0-9_]*(\([^)]*\))?`)

	paramBracketRE = regexp.MustCompile(`/\[[A-Za-z_][A-Za-z0-9_]*\]`)

	paramBraceRE = regexp.MustCompile(`/\{[A-Za-z_][A-Za-z0-9_]*(:[^}]*)?\}`)

	paramAngleRE = regexp.MustCompile(`/<([A-Za-z_][A-Za-z0-9_]*:)?[A-Za-z_][A-Za-z0-9_]*>`)

	paramDollarBraceRE = regexp.MustCompile(`/\$\{[A-Za-z_][A-Za-z0-9_]*\}`)
)

func GRPCKey(pkg, service, rpc string) string {
	if pkg == "" {
		return service + "/" + rpc
	}
	return pkg + "." + service + "/" + rpc
}

func MQKey(topic string) string {
	return topic
}

func GraphQLKey(typeName, field string) string {
	return typeName + "." + field
}
