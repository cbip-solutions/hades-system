// SPDX-License-Identifier: MIT
package parser

import _ "embed"

// tsTagsQuery is the Aider-pattern tree-sitter tags query for TypeScript,
// embedded from queries/typescript.scm. Captures @definition.{function,method,
// struct,interface,field}. Shared by the.ts and.tsx grammars (the tsx grammar
// is a JSX-accepting superset; the same defs query applies). The matching
// capture→store.NodeKind table is tsCaptures (lang_typescript.go).
//
//go:embed queries/typescript.scm
var tsTagsQuery string

// pyTagsQuery is the Python tags query (queries/python.scm); captures
// @definition.{function,method,struct}. Table: pyCaptures (lang_python.go).
//
//go:embed queries/python.scm
var pyTagsQuery string

// rustTagsQuery is the Rust tags query (queries/rust.scm); captures
// @definition.{function,method,struct,interface,field}. Table: rustCaptures
// (lang_rust.go).
//
//go:embed queries/rust.scm
var rustTagsQuery string
