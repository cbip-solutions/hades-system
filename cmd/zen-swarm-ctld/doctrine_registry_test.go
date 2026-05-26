// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func TestBootDoctrineRegistry_PopulatesActiveAccessor(t *testing.T) {
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)

	if err := bootDoctrineRegistry(); err != nil {
		t.Fatalf("bootDoctrineRegistry: %v", err)
	}

	got := active.Active()
	if got == nil {
		t.Fatal("active.Active() returned nil after bootDoctrineRegistry; " +
			"expected a non-nil *v1.Schema (registry max-scope fallback)")
	}

	if got.SchemaVersion == "" {
		t.Errorf("active.Active().SchemaVersion empty; want non-empty (built-in TOML carries it)")
	}
	if got.DoctrineVersion == "" {
		t.Errorf("active.Active().DoctrineVersion empty; want non-empty (built-in TOML carries it)")
	}
}

func TestBootDoctrineRegistry_IsIdempotent(t *testing.T) {
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)

	if err := bootDoctrineRegistry(); err != nil {
		t.Fatalf("first bootDoctrineRegistry: %v", err)
	}
	if err := bootDoctrineRegistry(); err != nil {
		t.Fatalf("second bootDoctrineRegistry: %v", err)
	}
	if active.Active() == nil {
		t.Fatal("Active() nil after second bootDoctrineRegistry call")
	}
}

func TestBootDoctrineRegistryFrom_PropagatesLoaderError(t *testing.T) {
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)

	sentinel := errors.New("corrupted embedded TOML")
	failingLoader := func() (map[string]*v1.Schema, error) {
		return nil, sentinel
	}
	err := bootDoctrineRegistryFrom(failingLoader)
	if err == nil {
		t.Fatal("bootDoctrineRegistryFrom with failing loader: nil err; want corrupted-binary wrap")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err wrap broken: errors.Is(err, sentinel) = false; got %v", err)
	}
	if !strings.Contains(err.Error(), "doctrine builtin.LoadAll") {
		t.Errorf("err message must mention the failing call site, got: %q", err.Error())
	}
}

func TestBootDoctrineRegistry_RegistersAllThreeBuiltins(t *testing.T) {
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)

	if err := bootDoctrineRegistry(); err != nil {
		t.Fatalf("bootDoctrineRegistry: %v", err)
	}
	for _, name := range []string{"max-scope", "default", "capa-firewall"} {
		if err := active.SetUserDefault(name); err != nil {
			t.Errorf("SetUserDefault(%q) failed after bootDoctrineRegistry: %v", name, err)
		}
	}
}
