// SPDX-License-Identifier: MIT
package walkers

import (
	"context"
	"os"
)

type GoModResult struct {
	Version        string
	MissingSources []string
}

type GoModWalker struct {
	path    string
	version string
}

func NewGoModWalker(path, version string) *GoModWalker {
	return &GoModWalker{path: path, version: version}
}

func (w *GoModWalker) Walk(_ context.Context) (GoModResult, error) {
	res := GoModResult{Version: w.version}
	if _, err := os.Stat(w.path); err != nil {
		res.MissingSources = append(res.MissingSources, "go.mod")
		return res, nil
	}
	return res, nil
}
