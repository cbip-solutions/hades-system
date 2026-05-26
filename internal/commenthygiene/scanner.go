// SPDX-License-Identifier: MIT
package commenthygiene

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Report struct {
	File     string
	Line     int
	Comment  string
	Decision ClassifierDecision
}

func Scan(root string) ([]Report, error) {
	var reports []Report
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == "testdata" || base == ".git" || base == "__pycache__" {
				return fs.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".py" {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		lineno := 0
		for scanner.Scan() {
			lineno++
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "#") {
				continue
			}
			decision := Classify(line)
			if decision != DecisionKeep {
				reports = append(reports, Report{
					File:     path,
					Line:     lineno,
					Comment:  trimmed,
					Decision: decision,
				})
			}
		}
		return scanner.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("commenthygiene.Scan: %w", err)
	}
	return reports, nil
}
