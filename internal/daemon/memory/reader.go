// SPDX-License-Identifier: MIT
// Package memory wraps the auto-memory tree at
// local agent memory/projects/<encoded-path>/memory/. release implements;
package memory

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type Entry struct {
	Name        string
	Description string
	Type        string
	Body        string
	Path        string
	WrittenBy   string
}

type Reader struct{}

func NewReader(memDir string) *Reader { return &Reader{} }

func (r *Reader) LoadIndex() ([]Entry, error) {
	return nil, zerrors.ErrNotImplementedPlan9
}

func (r *Reader) LoadEntry(path string) (*Entry, error) {
	return nil, zerrors.ErrNotImplementedPlan9
}
