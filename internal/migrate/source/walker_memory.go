// SPDX-License-Identifier: MIT
package source

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func walkMemory(absRoot string, inv *Inventory) error {
	projectsDir := filepath.Join(absRoot, "projects")
	info, err := os.Stat(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	type found struct {
		ProjectSlug, Path string
		Body              []byte
	}
	var results []found
	err = filepath.WalkDir(projectsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsPermission(walkErr) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}

		rel, err := filepath.Rel(projectsDir, path)
		if err != nil {
			return err
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) < 3 || parts[1] != "memory" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			if os.IsPermission(err) {
				inv.Warnings = append(inv.Warnings, fmt.Sprintf("memory: %s denied", path))
				return nil
			}
			return err
		}
		results = append(results, found{ProjectSlug: parts[0], Path: path, Body: body})
		return nil
	})
	if err != nil {
		return err
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })
	for _, r := range results {
		inv.MemoryFiles = append(inv.MemoryFiles, MemorySource{
			ProjectSlug: r.ProjectSlug,
			Path:        r.Path,
			Body:        r.Body,
		})
	}
	return nil
}
