// SPDX-License-Identifier: MIT
package parser

import _ "embed"

// goTagsQuery is the Aider-pattern tree-sitter tags query for Go, embedded
// from queries/go.scm. It declares the symbol definitions Caronte's L1
// extractor captures: @definition.{function,method,struct,interface,type,
// field} each paired with a @name.<kind> identifier capture.
//
// Embedding (vs reading the file at runtime) ships the query in the binary —
// no filesystem dependency, no path resolution, no partial-deploy risk. The
// matching kind→store.NodeKind table is nodeKindForCapture in lang_go.go; the
// two MUST stay in sync (queries_test.go gates the capture-name contract).
//
// go:embed queries/go.scm
var goTagsQuery string
