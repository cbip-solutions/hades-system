// SPDX-License-Identifier: MIT
package semantic

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/tools/go/packages"
)

const goLoadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo |
	packages.NeedModule

type loadResult struct {
	Packages   []*packages.Package
	Buildable  bool
	TypeErrors []packages.Error
}

func loadGoPackages(ctx context.Context, dir string) (loadResult, error) {

	if fi, statErr := os.Stat(dir); statErr != nil || !fi.IsDir() {
		return loadResult{}, fmt.Errorf("caronte/semantic: load dir %q: %w", dir, statErr)
	}
	cfg := &packages.Config{
		Mode:    goLoadMode,
		Context: ctx,
		Dir:     dir,
		Tests:   false,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return loadResult{}, fmt.Errorf("caronte/semantic: packages.Load %q: %w", dir, err)
	}
	if len(pkgs) == 0 {
		return loadResult{}, fmt.Errorf("caronte/semantic: no Go packages under %q", dir)
	}

	var typeErrs []packages.Error
	buildable := true
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if len(p.Errors) > 0 {
			typeErrs = append(typeErrs, p.Errors...)
		}
		if p.IllTyped || len(p.Errors) > 0 {
			buildable = false
		}
	})
	return loadResult{Packages: pkgs, Buildable: buildable, TypeErrors: typeErrs}, nil
}
