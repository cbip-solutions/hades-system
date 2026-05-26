// SPDX-License-Identifier: MIT
package spikes

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

type Result struct {
	Name     string
	Severity Severity
	Finding  string
	LastRun  time.Time
}

func (r Result) PersistReport(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("spikes: create %s: %w", path, err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	fmt.Fprintf(w, "---\n")
	fmt.Fprintf(w, "name: %s\n", r.Name)
	fmt.Fprintf(w, "severity: %s\n", r.Severity)
	fmt.Fprintf(w, "last_run: %s\n", r.LastRun.UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "---\n\n")
	fmt.Fprintf(w, "# Spike report: %s\n\n", r.Name)
	fmt.Fprintf(w, "## Finding\n\n%s\n", r.Finding)
	return nil
}

func LoadReport(path string) (Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return Result{}, fmt.Errorf("spikes: open %s: %w", path, err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var r Result
	inFront := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			if inFront {
				break
			}
			inFront = true
			continue
		}
		if !inFront {
			continue
		}
		switch {
		case strings.HasPrefix(line, "name: "):
			r.Name = strings.TrimPrefix(line, "name: ")
		case strings.HasPrefix(line, "severity: "):
			r.Severity = ParseSeverity(strings.TrimPrefix(line, "severity: "))
		case strings.HasPrefix(line, "last_run: "):
			ts, err := time.Parse(time.RFC3339, strings.TrimPrefix(line, "last_run: "))
			if err == nil {
				r.LastRun = ts
			}
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "## Finding") {
			for scanner.Scan() {
				body := scanner.Text()
				if body == "" {
					continue
				}
				if r.Finding != "" {
					r.Finding += "\n"
				}
				r.Finding += body
			}
		}
	}
	return r, scanner.Err()
}
