//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package parser

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type Parser struct{}

type ParseResult struct {
	Nodes   []store.Node
	Partial bool
}

func NewParser() (*Parser, error) { return nil, ErrCGODisabled }

func (p *Parser) ParseFile(_ context.Context, _ string, _ []byte) (*ParseResult, error) {
	return nil, ErrCGODisabled
}
