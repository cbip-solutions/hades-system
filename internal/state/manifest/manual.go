// SPDX-License-Identifier: MIT
package manifest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const TypeStateManualFieldChanged = "state.manual_field_changed"

type EventAppender interface {
	AppendEvent(ctx context.Context, ev EventPayload) error
}

type EventPayload struct {
	Type           string    `json:"type"`
	Field          string    `json:"field"`
	OldValue       any       `json:"old_value"`
	NewValue       any       `json:"new_value"`
	Reason         string    `json:"reason"`
	OperatorID     string    `json:"operator_id"`
	Timestamp      time.Time `json:"timestamp"`
	MissingSources []string  `json:"missing_sources,omitempty"`
}

type PinRequest struct {
	Path string

	NewValue any

	Reason string

	OperatorID string

	Timestamp time.Time
}

type ManualTracker struct {
	schema      *Schema
	regenerator *Regenerator
	appender    EventAppender
}

func NewManualTracker(schema *Schema, r *Regenerator, a EventAppender) *ManualTracker {
	return &ManualTracker{schema: schema, regenerator: r, appender: a}
}

func (mt *ManualTracker) Pin(ctx context.Context, manifestPath string, req PinRequest) error {

	if strings.TrimSpace(req.Reason) == "" {
		return ErrEmptyReason
	}

	manualPaths, err := mt.schema.DiscoverManualFields()
	if err != nil {
		return fmt.Errorf("discover manual fields: %w", err)
	}
	if !pathInList(manualPaths, req.Path) {
		return fmt.Errorf("%w: %s", ErrManualFieldNotFound, req.Path)
	}

	body, err := os.ReadFile(manifestPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read manifest: %w", err)
	}
	var existing Manifest
	if len(body) > 0 {
		if _, decErr := toml.NewDecoder(bytes.NewReader(body)).Decode(&existing); decErr != nil {
			return fmt.Errorf("%w: %v", ErrManifestInvalid, decErr)
		}
	}

	oldReflect := getValueAtPath(existing, req.Path)
	var oldValue any
	if oldReflect.IsValid() {
		oldValue = oldReflect.Interface()
	}

	if err := setValueAtPath(&existing, req.Path, req.NewValue); err != nil {
		return err
	}

	emitted, err := mt.regenerator.Emit(existing)
	if err != nil {
		return fmt.Errorf("emit manifest: %w", err)
	}
	if err := atomicWrite(manifestPath, emitted, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	ev := EventPayload{
		Type:       TypeStateManualFieldChanged,
		Field:      req.Path,
		OldValue:   oldValue,
		NewValue:   req.NewValue,
		Reason:     req.Reason,
		OperatorID: req.OperatorID,
		Timestamp:  req.Timestamp.UTC(),
	}
	if appendErr := mt.appender.AppendEvent(ctx, ev); appendErr != nil {

		if rollbackErr := atomicWrite(manifestPath, body, 0o644); rollbackErr != nil {
			return fmt.Errorf("%w: %v (rollback also failed: %v)",
				ErrEventEmissionFailed, appendErr, rollbackErr)
		}
		return fmt.Errorf("%w: %v", ErrEventEmissionFailed, appendErr)
	}
	return nil
}

func getValueAtPath(m Manifest, path string) reflect.Value {
	parts := splitDotPath(path)
	if len(parts) < 2 {
		return reflect.Value{}
	}
	v := reflect.ValueOf(m)
	section := sectionByTOMLName(v, parts[0])
	if !section.IsValid() {
		return reflect.Value{}
	}
	return fieldByTOMLName(section, joinDotPath(parts[1:]))
}

func setValueAtPath(m *Manifest, path string, val any) error {
	parts := splitDotPath(path)
	if len(parts) < 2 {
		return fmt.Errorf("%w: %s (too short)", ErrManualFieldNotFound, path)
	}
	v := reflect.ValueOf(m).Elem()
	section := sectionByTOMLName(v, parts[0])
	if !section.IsValid() {
		return fmt.Errorf("%w: section %q not found", ErrManualFieldNotFound, parts[0])
	}
	field := fieldByTOMLName(section, joinDotPath(parts[1:]))
	if !field.IsValid() || !field.CanSet() {
		return fmt.Errorf("%w: field %q not found or not settable", ErrManualFieldNotFound, path)
	}
	rv := reflect.ValueOf(val)
	if !rv.IsValid() {
		return fmt.Errorf("manifest: nil pin value for %s", path)
	}

	if field.Kind() == reflect.String && rv.Kind() == reflect.String {
		field.SetString(rv.String())
		return nil
	}
	if rv.Type() != field.Type() {
		return fmt.Errorf("manifest: pin value type %s incompatible with field type %s at %s",
			rv.Type(), field.Type(), path)
	}
	field.Set(rv)
	return nil
}

func splitDotPath(path string) []string {
	return strings.SplitN(path, ".", 2)
}

func joinDotPath(parts []string) string {
	return strings.Join(parts, ".")
}

func pathInList(paths []ManualFieldPath, path string) bool {
	for _, mp := range paths {
		if mp.Path == path {
			return true
		}
	}
	return false
}
