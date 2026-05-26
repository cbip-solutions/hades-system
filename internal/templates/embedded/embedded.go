// SPDX-License-Identifier: MIT
// Package embedded ships 6 canonical baked-in templates.
//
// Doctrine note: the embed directive uses `//go:embed all:fixtures` so
// dotfiles (`.gitignore.tmpl`, `.gitkeep`) survive embedding. The default
// `//go:embed fixtures` would silently drop them; "all:" prefix is the
// fix per Go 1.18+.
package embedded

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"

	t "github.com/cbip-solutions/hades-system/internal/templates"
)

//go:embed all:fixtures
var fixturesFS embed.FS

func Templates() []string {
	return []string{
		"hermes-plugin-only",
		"hermes-plugin+daemon",
		"go-cli",
		"python-cli",
		"ts-saas",
		"ml-pipeline",
	}
}

func Template(name string) (t.Template, error) {
	known := false
	for _, n := range Templates() {
		if n == name {
			known = true
			break
		}
	}
	if !known {
		return nil, fmt.Errorf("%w: %s", t.ErrUnknownTemplate, name)
	}

	sub, err := fs.Sub(fixturesFS, path.Join("fixtures", name))
	if err != nil {
		return nil, fmt.Errorf("embedded sub-fs for %q: %w", name, err)
	}
	return &embeddedTemplate{name: name, root: sub}, nil
}

func Registry() *t.Registry {
	r := t.NewRegistry()
	for _, n := range Templates() {
		tmpl, err := Template(n)
		if err != nil {

			panic(fmt.Sprintf("embedded registry seed %q: %v", n, err))
		}
		r.Add(tmpl)
	}
	return r
}

type embeddedTemplate struct {
	name string
	root fs.FS
}

func (e *embeddedTemplate) Name() string { return e.name }
func (e *embeddedTemplate) FS() fs.FS    { return e.root }

func (e *embeddedTemplate) Materialize(ctx context.Context, dst string, answers t.Answers) error {
	return t.MaterializeFS(ctx, e.root, dst, answers)
}
