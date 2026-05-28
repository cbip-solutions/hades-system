// SPDX-License-Identifier: MIT
package walkers

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type InvariantsResult struct {
	Count          int
	MissingSources []string
}

type InvariantsWalker struct {
	root string
}

func NewInvariantsWalker(root string) *InvariantsWalker { return &InvariantsWalker{root: root} }

var invHadesRE = regexp.MustCompile("invariant-([0-9]+)")

func (w *InvariantsWalker) Walk(_ context.Context) (InvariantsResult, error) {
	res := InvariantsResult{}

	roots := []string{
		filepath.Join(w.root, "internal"),
		filepath.Join(w.root, "tests"),
	}

	var existing []string
	for _, r := range roots {
		if _, err := os.Stat(r); err == nil {
			existing = append(existing, r)
		}
	}

	if len(existing) == 0 {
		if _, err := os.Stat(w.root); err != nil {
			res.MissingSources = append(res.MissingSources, "grep-roots")
			return res, nil
		}
		existing = []string{w.root}
	}

	seen := map[string]struct{}{}
	for _, r := range existing {
		_ = filepath.Walk(r, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			if !shouldScan(path) {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer f.Close()
			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)
			for scanner.Scan() {
				for _, m := range invHadesRE.FindAllStringSubmatch(scanner.Text(), -1) {
					seen[m[1]] = struct{}{}
				}
			}
			return nil
		})
	}
	res.Count = len(seen)
	return res, nil
}

func shouldScan(path string) bool {
	switch {
	case strings.HasSuffix(path, ".go"),
		strings.HasSuffix(path, ".md"),
		strings.HasSuffix(path, ".sql"):
		return true
	}
	return false
}
