// SPDX-License-Identifier: MIT
// Package templates is the top-level interface shared by embedded + pluggable.
//
// Spec reference: §3.5 (template system shape).
//
// Doctrine every scaffolded project is Hermes-loadable + carries NO Claude
// attribution in its post_gen.sh git commit (invariant propagation).
package templates

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

var ErrUnknownTemplate = errors.New("unknown template")

var ErrTargetExists = errors.New("target exists and is not empty; pass --force to overwrite")

type Answers struct {
	ProjectName string

	ProjectKind string

	ProjectPath string

	Doctrine string

	MCPSelections []string

	AuthorName  string
	AuthorEmail string

	InitGit bool

	LinkHermesPlugin bool

	PingDaemon bool

	HermesPluginScope string
}

type Template interface {
	Name() string

	FS() fs.FS

	Materialize(ctx context.Context, dst string, answers Answers) error
}

type Registry struct {
	byName map[string]Template
	order  []string
}

func NewRegistry() *Registry {
	return &Registry{byName: map[string]Template{}, order: nil}
}

func (r *Registry) Add(t Template) {
	if _, exists := r.byName[t.Name()]; !exists {
		r.order = append(r.order, t.Name())
	}
	r.byName[t.Name()] = t
}

func (r *Registry) Get(name string) (Template, error) {
	t, ok := r.byName[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTemplate, name)
	}
	return t, nil
}

func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

func MaterializeFS(ctx context.Context, root fs.FS, dst string, answers Answers) error {
	return fs.WalkDir(root, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == "." {
			return os.MkdirAll(dst, 0o755)
		}
		out := filepath.Join(dst, path)
		if d.Name() == ".gitkeep" {

			return os.MkdirAll(filepath.Dir(out), 0o755)
		}
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(path, ".sh") {
			mode = 0o755
		}
		raw, err := fs.ReadFile(root, path)
		if err != nil {
			return fmt.Errorf("read %q: %w", path, err)
		}
		var data []byte
		if strings.HasSuffix(path, ".tmpl") {
			out = strings.TrimSuffix(out, ".tmpl")
			tmpl, err := template.New(filepath.Base(path)).Parse(string(raw))
			if err != nil {
				return fmt.Errorf("parse template %q: %w", path, err)
			}
			var buf strings.Builder
			if err := tmpl.Execute(&buf, answers); err != nil {
				return fmt.Errorf("execute template %q: %w", path, err)
			}
			data = []byte(buf.String())
		} else {
			data = raw
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return writeFileAtomic(out, data, mode)
	})
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, strings.NewReader(string(data))); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
