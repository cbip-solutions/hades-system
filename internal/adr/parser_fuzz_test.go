package adr

import (
	"bytes"
	"testing"
)

func FuzzParse(f *testing.F) {

	f.Add([]byte(`---
id: ADR-0001
title: Test decision
status: Proposed
plan: 9
date: 2026-05-07
risk_level: low
supersedes: []
---
# Context
Body here.
`))

	f.Add([]byte(`# ADR-0042: Legacy decision

## Context

This ADR predates the structured frontmatter migration.
`))

	f.Add([]byte{})
	f.Add([]byte("\n"))
	f.Add([]byte("   \n\t\n  "))

	f.Add([]byte(`---
not yaml at all: { broken
---
body
`))

	f.Add([]byte(`---
id: ADR-0001
title: Unclosed
status: Proposed
`))

	f.Add([]byte(`---
---
body
`))

	f.Add([]byte(`---
id: ADR-0001
unknown_field: surprise
---
`))

	f.Add([]byte(`---
supersedes:
  - id: ADR-0001
    title: nested
    children:
      - deeper
      - and: deeper
        still:
          - so: deep
---
`))

	f.Add([]byte("---\n\x00\x01\x02\xff\n---\n"))

	f.Add([]byte("---\n---\n---\n---\n"))

	f.Add([]byte("---\r\nid: ADR-0001\r\ntitle: CRLF\r\nstatus: Proposed\r\nplan: 9\r\ndate: 2026-05-07\r\nrisk_level: low\r\nsupersedes: []\r\n---\r\nbody\r\n"))

	f.Fuzz(func(t *testing.T, input []byte) {

		a1, err1 := Parse(bytes.NewReader(input))

		a2, err2 := Parse(bytes.NewReader(input))

		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("non-deterministic error presence: err1=%v err2=%v", err1, err2)
		}
		if err1 != nil && err2 != nil {
			if err1.Error() != err2.Error() {
				t.Fatalf("non-deterministic error: %q vs %q", err1, err2)
			}

			if a1 != nil || a2 != nil {
				t.Fatalf("error path returned non-nil *ADR: a1=%v a2=%v", a1, a2)
			}
			return
		}

		if a1 == nil || a2 == nil {
			t.Fatalf("success path returned nil *ADR: a1=%v a2=%v", a1, a2)
		}

		if a1.Frontmatter.ID != a2.Frontmatter.ID {
			t.Fatalf("non-deterministic ID: %q vs %q", a1.Frontmatter.ID, a2.Frontmatter.ID)
		}
		if a1.Frontmatter.Title != a2.Frontmatter.Title {
			t.Fatalf("non-deterministic Title: %q vs %q", a1.Frontmatter.Title, a2.Frontmatter.Title)
		}
		if a1.Frontmatter.Status != a2.Frontmatter.Status {
			t.Fatalf("non-deterministic Status: %q vs %q", a1.Frontmatter.Status, a2.Frontmatter.Status)
		}

		if a1.Body != a2.Body {
			t.Fatalf("non-deterministic Body: len %d vs %d", len(a1.Body), len(a2.Body))
		}

		maxBodyLen := 2*len(input) + 1
		if len(a1.Body) > maxBodyLen {
			t.Fatalf("Body length %d exceeds 2*input+1 = %d (fabrication?)",
				len(a1.Body), maxBodyLen)
		}

		if a1.HasFrontmatter() != a2.HasFrontmatter() {
			t.Fatalf("non-deterministic HasFrontmatter: %v vs %v",
				a1.HasFrontmatter(), a2.HasFrontmatter())
		}
	})
}
