// go:build !cgo
//go:build !cgo
// +build !cgo

package caronte

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/mcp/research"
)

func TestNewEngineNoCGOReturnsErrCGODisabled(t *testing.T) {
	_, err := NewEngine(Deps{})
	if !errors.Is(err, store.ErrCGODisabled) {
		t.Errorf("NewEngine (!cgo) err = %v; want store.ErrCGODisabled", err)
	}
}

func TestNoCGOEngineStillSatisfiesGitnexusClient(t *testing.T) {
	var e Engine
	var _ research.GitnexusClient = &e
	if _, err := e.CodeGraph(context.Background(), "x", "p"); !errors.Is(err, store.ErrCGODisabled) {
		t.Errorf("CodeGraph (!cgo) err = %v; want store.ErrCGODisabled", err)
	}
	if err := e.Close(); err != nil {
		t.Errorf("Close (!cgo) err = %v; want nil", err)
	}
}
