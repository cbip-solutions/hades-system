// SPDX-License-Identifier: MIT
package components

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

type ProseRender struct {
	Width int
}

func (p ProseRender) Render(md string) (string, error) {
	stripped := stripFrontmatter(md)
	w := p.Width
	if w == 0 {
		w = 80
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(w),
	)
	if err != nil {
		return "", err
	}
	return r.Render(stripped)
}

func stripFrontmatter(md string) string {
	if !strings.HasPrefix(md, "---\n") {
		return md
	}
	rest := md[4:]
	end := strings.Index(rest, "\n---\n")
	if end == -1 {
		return md
	}
	return rest[end+5:]
}
