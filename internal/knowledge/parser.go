// SPDX-License-Identifier: MIT
package knowledge

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

var ErrBinaryContent = errors.New("knowledge: binary content rejected")

var frontmatterRE = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n(.*)\z`)

var h1RE = regexp.MustCompile(`(?m)^# (.+?)\s*$`)

const binaryNULThreshold = 0.10

const binaryHeadBytes = 4096

// Parse reads sf.Path and returns a populated Doc. Partial-tolerance per
// spec §4.5: malformed YAML frontmatter drops to content-only indexing
// (Doc returned, FrontmatterJSON nil); only ErrBinaryContent and
// hard I/O errors propagate.
//
// Per invariant: Parse MUST NOT populate AuditChainAnchor /
// EcosystemJoinKeys / CaronteSymbolRefs even if the operator's YAML
// frontmatter contains keys with those names. release / release / caronte
// are the authoritative writers for those fields.
//
// Title fallback chain (spec §3.5 + §4.5):
// 1. Frontmatter `title:` field if it exists, parses cleanly, decodes
// to a non-empty string.
// 2. First `^# ` ATX-style H1 heading anywhere in the body.
// 3. Basename of sf.Path with the `.md` extension stripped.
//
// LastModified is populated from sf.ModTime (the scanner's stat at
// enumeration time); the parser does NOT re-stat. LastIndexed is the
// wall-clock UTC timestamp at parse time — the watcher's "we just
// indexed this" anchor for staleness detection.
//
// Boundary this function uses stdlib + gopkg.in/yaml.v3 only. No
// internal/store import (separate-DB boundary per knowledge package
// docs). No net/http (invariant no remote queries).
func Parse(sf ScannedFile) (Doc, error) {
	raw, err := os.ReadFile(sf.Path)
	if err != nil {
		return Doc{}, fmt.Errorf("knowledge: read %q: %w", sf.Path, err)
	}

	if isBinary(raw) {
		return Doc{}, ErrBinaryContent
	}

	doc := Doc{
		FilePath:     sf.Path,
		ProjectID:    sf.ProjectID,
		ProjectAlias: sf.ProjectAlias,
		FileType:     sf.Kind,
		LastModified: time.Unix(0, sf.ModTime).UTC(),
		LastIndexed:  time.Now().UTC(),
		// Extension-hook fields LEFT AS ZERO sql.NullString (Valid=false).
		// invariant: parser MUST NOT populate. release (audit_chain_anchor),
		// are the authoritative writers at materialization time.
		AuditChainAnchor:  sql.NullString{},
		EcosystemJoinKeys: sql.NullString{},
		CaronteSymbolRefs: sql.NullString{},
	}

	contentBytes := extractFrontmatter(raw, &doc)

	doc.ContentText = string(contentBytes)

	if doc.Title == "" {
		if m := h1RE.FindStringSubmatch(doc.ContentText); m != nil {
			doc.Title = strings.TrimSpace(m[1])
		}
	}
	if doc.Title == "" {
		doc.Title = strings.TrimSuffix(filepath.Base(sf.Path), ".md")
	}

	return doc, nil
}

func extractFrontmatter(raw []byte, doc *Doc) []byte {
	m := frontmatterRE.FindSubmatch(raw)
	if m == nil {
		return raw
	}
	yamlBlob := m[1]
	body := m[2]

	var fmMap map[string]any
	if err := yaml.Unmarshal(yamlBlob, &fmMap); err != nil {

		return raw
	}
	if len(fmMap) == 0 {

		return body
	}

	jsonBytes, err := json.Marshal(fmMap)
	if err != nil {

		return body
	}
	doc.FrontmatterJSON = jsonBytes
	if t, ok := fmMap["title"].(string); ok && t != "" {
		doc.Title = t
	}
	return body
}

func isBinary(buf []byte) bool {
	if len(buf) == 0 {
		return false
	}
	n := len(buf)
	if n > binaryHeadBytes {
		n = binaryHeadBytes
	}
	head := buf[:n]
	if bytes.IndexByte(head, 0x00) >= 0 {

		nulCount := bytes.Count(head, []byte{0x00})
		if float64(nulCount)/float64(n) >= binaryNULThreshold {
			return true
		}
	}
	if !utf8.Valid(head) {
		return true
	}
	return false
}
