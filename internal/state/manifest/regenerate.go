// SPDX-License-Identifier: MIT
package manifest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type Regenerator struct {
	schema       *Schema
	manualFields []ManualFieldPath
}

func NewRegenerator(schema *Schema) *Regenerator {
	return &Regenerator{schema: schema}
}

func (r *Regenerator) Regenerate(ctx context.Context, fresh Manifest, existingPath string) (Manifest, error) {
	manualPaths, err := r.cachedManualPaths()
	if err != nil {
		return Manifest{}, fmt.Errorf("discover manual fields: %w", err)
	}

	merged := fresh

	body, err := os.ReadFile(existingPath)
	if err != nil {
		if os.IsNotExist(err) {

			return merged, nil
		}
		return Manifest{}, fmt.Errorf("read existing manifest: %w", err)
	}

	var existing Manifest
	if _, decErr := toml.NewDecoder(bytes.NewReader(body)).Decode(&existing); decErr != nil {
		// Existing TOML malformed: return ErrManifestInvalid.
		// The regenerate-and-diff CI gate (invariant) will surface
		// the problem; we do NOT silently fall back to fresh-only values
		// because that would constitute a T10 threat (silent manual-field loss).
		return Manifest{}, fmt.Errorf("%w: parse %s: %v", ErrManifestInvalid, existingPath, decErr)
	}

	for _, mp := range manualPaths {
		copyManualField(&merged, &existing, mp.Path)
	}

	return merged, nil
}

func (r *Regenerator) Emit(m Manifest) ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = "  "
	if err := enc.Encode(stableManifest(m)); err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	return buf.Bytes(), nil
}

func (r *Regenerator) RegenerateAndWrite(ctx context.Context, fresh Manifest, manifestPath string) error {
	merged, err := r.Regenerate(ctx, fresh, manifestPath)
	if err != nil {
		return err
	}
	body, err := r.Emit(merged)
	if err != nil {
		return err
	}
	return atomicWrite(manifestPath, body, 0o644)
}

func (r *Regenerator) DryRun(ctx context.Context, fresh Manifest, manifestPath string) ([]byte, error) {
	merged, err := r.Regenerate(ctx, fresh, manifestPath)
	if err != nil {
		return nil, err
	}
	return r.Emit(merged)
}

func (r *Regenerator) cachedManualPaths() ([]ManualFieldPath, error) {
	if r.manualFields != nil {
		return r.manualFields, nil
	}
	paths, err := r.schema.DiscoverManualFields()
	if err != nil {
		return nil, err
	}
	r.manualFields = paths
	return paths, nil
}

type emitManifest struct {
	HadesSystem    HadesSystemSection    `toml:"hades-system"`
	Plans          PlansSection          `toml:"plans"`
	Invariants     InvariantsSection     `toml:"invariants"`
	Doctrines      DoctrinesSection      `toml:"doctrines"`
	MCPs           emitMCPsSection       `toml:"mcps"`
	ADR            ADRSection            `toml:"adr"`
	AutonomousMode AutonomousModeSection `toml:"autonomous-mode"`
	Provenance     Provenance            `toml:"provenance"`
}

type emitMCPsSection struct {
	Entries map[string]MCPEntry `toml:"entries,omitempty"`
}

func stableManifest(m Manifest) emitManifest {
	entries := make(map[string]MCPEntry, len(m.MCPs.Entries))
	keys := make([]string, 0, len(m.MCPs.Entries))
	for k := range m.MCPs.Entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		entries[k] = m.MCPs.Entries[k]
	}
	return emitManifest{
		HadesSystem:    m.HadesSystem,
		Plans:          m.Plans,
		Invariants:     m.Invariants,
		Doctrines:      m.Doctrines,
		MCPs:           emitMCPsSection{Entries: entries},
		ADR:            m.ADR,
		AutonomousMode: m.AutonomousMode,
		Provenance:     m.Provenance,
	}
}

func copyManualField(dst *Manifest, src *Manifest, path string) {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) != 2 {

		return
	}
	sectionName, leafName := parts[0], parts[1]

	dstSection := sectionByTOMLName(reflect.ValueOf(dst).Elem(), sectionName)
	srcSection := sectionByTOMLName(reflect.ValueOf(src).Elem(), sectionName)
	if !dstSection.IsValid() || !srcSection.IsValid() {
		return
	}

	dstField := fieldByTOMLName(dstSection, leafName)
	srcField := fieldByTOMLName(srcSection, leafName)
	if !dstField.IsValid() || !srcField.IsValid() {
		return
	}
	if dstField.CanSet() {
		dstField.Set(srcField)
	}
}

func sectionByTOMLName(v reflect.Value, sectionName string) reflect.Value {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		tag := tomlTagName(t.Field(i).Tag.Get("toml"))
		if tag == sectionName {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

func fieldByTOMLName(section reflect.Value, fieldName string) reflect.Value {
	t := section.Type()
	for i := 0; i < t.NumField(); i++ {
		tag := tomlTagName(t.Field(i).Tag.Get("toml"))
		if tag == fieldName {
			return section.Field(i)
		}
	}
	return reflect.Value{}
}

func tomlTagName(tag string) string {
	if i := strings.IndexByte(tag, ','); i >= 0 {
		return tag[:i]
	}
	return tag
}

func atomicWrite(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("atomic write create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("atomic write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("atomic write sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("atomic write close: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("atomic write chmod: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("atomic write rename: %w", err)
	}
	return nil
}
