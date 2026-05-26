// SPDX-License-Identifier: MIT
package source

import (
	"path/filepath"
	"sort"
	"strings"
)

func walkCommands(absRoot string, inv *Inventory) error {
	dir := filepath.Join(absRoot, "commands")
	files, err := readMDFilesFlat(dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}

	sort.Strings(names)
	for _, name := range names {
		base := strings.TrimSuffix(name, ".md")
		inv.Commands = append(inv.Commands, CommandSource{
			Name: base,
			Path: filepath.Join(dir, name),
			Body: files[name],
		})
	}
	return nil
}
