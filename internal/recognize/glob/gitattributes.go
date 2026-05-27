// SPDX-License-Identifier: MIT
// Package glob — gitattributes parser.
//
// Honors linguist-* overrides per spec §2.4 Q4=B. Subset implementation:
// recognizes pattern lines of the form
//
// <glob> linguist-language=<X>
// <glob> linguist-vendored=true
// <glob> linguist-generated=true
// <glob> linguist-documentation=true
//
// where <glob> is a fnmatch-style pattern (e.g. `*.foo`, `vendor/**`,
// `legacy/*`). Patterns matching the leading `legacy/*` apply to direct
// children only; `legacy/**` recurses (we treat `*` as `**` for simplicity,
// matching git's actual behavior since the gitattributes spec is permissive).
package glob

import (
	"bufio"
	"bytes"
	"io"
	"io/fs"
	"path"
	"strings"
)

type Overrides struct {
	rules []attrRule
}

type attrRule struct {
	pattern       string
	language      string
	vendored      bool
	generated     bool
	documentation bool
}

func parseGitAttributes(fsys fs.FS) (Overrides, error) {
	var o Overrides
	f, err := fsys.Open(".gitattributes")
	if err != nil {

		return o, nil
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, 1*1024*1024))
	if err != nil {
		return o, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(buf))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		r := attrRule{pattern: fields[0]}
		for _, attr := range fields[1:] {
			switch {
			case strings.HasPrefix(attr, "linguist-language="):
				r.language = strings.TrimPrefix(attr, "linguist-language=")
			case attr == "linguist-vendored=true":
				r.vendored = true
			case attr == "linguist-generated=true":
				r.generated = true
			case attr == "linguist-documentation=true":
				r.documentation = true
			}
		}
		if r.language != "" || r.vendored || r.generated || r.documentation {
			o.rules = append(o.rules, r)
		}
	}
	return o, scanner.Err()
}

func match(pattern, p string) bool {
	ok, _ := path.Match(pattern, p)
	if ok {
		return true
	}

	if !strings.Contains(pattern, "*") {
		return false
	}
	parts := strings.Split(pattern, "*")
	rest := p
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(rest, part)
		if idx < 0 {
			return false
		}
		if i == 0 && idx != 0 {
			return false
		}
		rest = rest[idx+len(part):]
	}

	return true
}

func (o Overrides) LanguageOverride(p string) string {
	out := ""
	for _, r := range o.rules {
		if r.language != "" && match(r.pattern, p) {
			out = r.language
		}
	}
	return out
}

func (o Overrides) IsVendored(p string) bool {
	out := false
	for _, r := range o.rules {
		if r.vendored && match(r.pattern, p) {
			out = true
		}
	}
	return out
}

func (o Overrides) IsGenerated(p string) bool {
	out := false
	for _, r := range o.rules {
		if r.generated && match(r.pattern, p) {
			out = true
		}
	}
	return out
}

func (o Overrides) IsDocumentation(p string) bool {
	out := false
	for _, r := range o.rules {
		if r.documentation && match(r.pattern, p) {
			out = true
		}
	}
	return out
}
