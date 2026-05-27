// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package ecosystem

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

type SymbolIndex struct {
	mu   sync.RWMutex
	sets map[Ecosystem]*ecosystemSymbolSet
}

type ecosystemSymbolSet struct {
	mu       sync.RWMutex
	anySet   map[string]struct{}
	versions map[string]map[string]struct{}
}

type SymbolIndexStats struct {
	Ecosystem     Ecosystem
	TotalSymbols  int
	TotalVersions int
}

func NewSymbolIndex() *SymbolIndex {
	return &SymbolIndex{
		sets: make(map[Ecosystem]*ecosystemSymbolSet),
	}
}

func (s *SymbolIndex) Load(ctx context.Context, db *sql.DB, eco Ecosystem) error {
	if db == nil {
		return fmt.Errorf("ecosystem: SymbolIndex.Load(%s): nil db", eco)
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("ecosystem: SymbolIndex.Load(%s) ctx: %w", eco, err)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT s.symbol_path, COALESCE(s.introduced_in, '')
		FROM ecosystem_symbols s
		JOIN ecosystem_packages p ON s.package_id = p.id
		WHERE p.ecosystem = ?
	`, string(eco))
	if err != nil {
		return fmt.Errorf("ecosystem: SymbolIndex.Load(%s) query: %w", eco, err)
	}
	defer func() { _ = rows.Close() }()

	fresh := &ecosystemSymbolSet{
		anySet:   make(map[string]struct{}, 1024),
		versions: make(map[string]map[string]struct{}, 1024),
	}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("ecosystem: SymbolIndex.Load(%s) ctx: %w", eco, err)
		}
		var path, version string
		if err := rows.Scan(&path, &version); err != nil {
			return fmt.Errorf("ecosystem: SymbolIndex.Load(%s) scan: %w", eco, err)
		}
		fresh.anySet[path] = struct{}{}
		if version != "" {
			vs, ok := fresh.versions[path]
			if !ok {
				vs = make(map[string]struct{}, 2)
				fresh.versions[path] = vs
			}
			vs[version] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("ecosystem: SymbolIndex.Load(%s) rows.Err: %w", eco, err)
	}

	s.mu.Lock()
	s.sets[eco] = fresh
	s.mu.Unlock()
	return nil
}

func (s *SymbolIndex) Register(eco Ecosystem, symbolPath, version string) {
	set := s.getOrCreateSet(eco)
	set.mu.Lock()
	defer set.mu.Unlock()
	set.anySet[symbolPath] = struct{}{}
	if version != "" {
		vs, ok := set.versions[symbolPath]
		if !ok {
			vs = make(map[string]struct{}, 2)
			set.versions[symbolPath] = vs
		}
		vs[version] = struct{}{}
	}
}

func (s *SymbolIndex) Rebuild(ctx context.Context, db *sql.DB, eco Ecosystem) error {
	return s.Load(ctx, db, eco)
}

func (s *SymbolIndex) Contains(ref SymbolRef) bool {
	s.mu.RLock()
	set, ok := s.sets[ref.Ecosystem]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	set.mu.RLock()
	defer set.mu.RUnlock()
	if ref.Version == "" {
		_, found := set.anySet[ref.SymbolPath]
		return found
	}
	vs, ok := set.versions[ref.SymbolPath]
	if !ok {
		return false
	}
	_, found := vs[ref.Version]
	return found
}

func (s *SymbolIndex) ContainsVersioned(eco Ecosystem, symbolPath, version string) bool {
	if version == "" {
		return false
	}
	return s.Contains(SymbolRef{Ecosystem: eco, SymbolPath: symbolPath, Version: version})
}

func (s *SymbolIndex) Stats(eco Ecosystem) SymbolIndexStats {
	s.mu.RLock()
	set, ok := s.sets[eco]
	s.mu.RUnlock()
	if !ok {
		return SymbolIndexStats{Ecosystem: eco}
	}
	set.mu.RLock()
	defer set.mu.RUnlock()
	totalVersions := 0
	for _, vs := range set.versions {
		totalVersions += len(vs)
	}
	return SymbolIndexStats{
		Ecosystem:     eco,
		TotalSymbols:  len(set.anySet),
		TotalVersions: totalVersions,
	}
}

func (s *SymbolIndex) getOrCreateSet(eco Ecosystem) *ecosystemSymbolSet {
	s.mu.RLock()
	set, ok := s.sets[eco]
	s.mu.RUnlock()
	if ok {
		return set
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if set, ok = s.sets[eco]; ok {
		return set
	}
	set = &ecosystemSymbolSet{
		anySet:   make(map[string]struct{}),
		versions: make(map[string]map[string]struct{}),
	}
	s.sets[eco] = set
	return set
}
