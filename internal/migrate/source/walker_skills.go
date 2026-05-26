// SPDX-License-Identifier: MIT
package source

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

func walkSkills(absRoot string, inv *Inventory) error {
	dir := filepath.Join(absRoot, "skills")
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat skills: %w", err)
	}
	if !info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read skills: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillMD := filepath.Join(dir, e.Name(), "SKILL.md")
		st, err := os.Stat(skillMD)
		if err != nil {
			if os.IsNotExist(err) {
				inv.Warnings = append(inv.Warnings, fmt.Sprintf("skills/%s: missing SKILL.md", e.Name()))
				continue
			}
			return fmt.Errorf("stat %s: %w", skillMD, err)
		}
		if st.Mode()&os.ModeType != 0 {

			continue
		}
		body, err := os.ReadFile(skillMD)
		if err != nil {
			if os.IsPermission(err) {
				inv.Warnings = append(inv.Warnings, fmt.Sprintf("skills/%s: %s", e.Name(), ErrPermissionDenied))
				continue
			}
			return fmt.Errorf("read %s: %w", skillMD, err)
		}
		inv.Skills = append(inv.Skills, SkillSource{
			Name: e.Name(),
			Path: skillMD,
			Body: body,
		})
	}
	return nil
}

func readMDFilesFlat(dir string) (map[string][]byte, error) {
	out := map[string][]byte{}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".md" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out[e.Name()] = body
	}
	return out, nil
}

func walkAnyExt(dir string, exts ...string) ([]fs.DirEntry, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, e := range exts {
		want[e] = true
	}
	out := make([]fs.DirEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if want[filepath.Ext(e.Name())] {
			out = append(out, e)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}
