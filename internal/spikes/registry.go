// SPDX-License-Identifier: MIT
package spikes

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrRegistryEmpty = errors.New("spikes: registry empty")

type Spike struct {
	Name         string
	ReportPath   string
	LastSeverity Severity
	LastFinding  string
	LastRun      time.Time

	execute func() Result
}

func (s *Spike) Execute() (Result, error) {
	if s.execute == nil {
		return Result{
			Name:     s.Name,
			Severity: s.LastSeverity,
			Finding:  s.LastFinding,
			LastRun:  s.LastRun,
		}, nil
	}
	r := s.execute()
	if r.Name == "" {
		r.Name = s.Name
	}
	return r, nil
}

func (s *Spike) PersistReport(r Result) error {
	if r.Name == "" {
		r.Name = s.Name
	}
	return r.PersistReport(s.ReportPath)
}

type Registry map[string]*Spike

func LoadRegistry(dir string) (Registry, error) {
	reg := Registry{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if !strings.HasPrefix(base, "spike_") || !strings.HasSuffix(base, ".md") {
			return nil
		}
		r, err := LoadReport(path)
		if err != nil {
			return fmt.Errorf("spikes: load %s: %w", path, err)
		}
		name := strings.TrimSuffix(base, ".md")
		reg[name] = &Spike{
			Name:         name,
			ReportPath:   path,
			LastSeverity: r.Severity,
			LastFinding:  r.Finding,
			LastRun:      r.LastRun,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(reg) == 0 {
		return nil, ErrRegistryEmpty
	}
	return reg, nil
}

func (r Registry) Register(name string, exec func() Result) error {
	s, ok := r[name]
	if !ok {
		return fmt.Errorf("spikes: cannot register unknown spike %q (no report at docs/spikes/%s.md)", name, name)
	}
	s.execute = exec
	return nil
}

func (r Registry) SortedNames() []string {
	out := make([]string, 0, len(r))
	for k := range r {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
