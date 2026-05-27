// SPDX-License-Identifier: MIT
package plan9adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/state/manifest"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type stateWalker interface {
	Walk(context.Context) (manifest.WalkResult, error)
}

type StateAdapterDeps struct {
	RepoRoot           string
	ManifestPath       string
	SchemaPath         string
	AutonomyStampPath  string
	Version            string
	DoctrineRegistryFn func() []string
	Store              *store.Store
	Walker             stateWalker
	Now                func() time.Time
}

type StateAdapter struct {
	manifestPath string
	schema       *manifest.Schema
	regenerator  *manifest.Regenerator
	differ       *manifest.Differ
	tracker      *manifest.ManualTracker
	store        *store.Store
	walker       stateWalker
	now          func() time.Time
}

var _ handlers.StateService = (*StateAdapter)(nil)

func NewStateAdapter(deps StateAdapterDeps) (*StateAdapter, error) {
	manifestPath := strings.TrimSpace(deps.ManifestPath)
	if manifestPath == "" && deps.RepoRoot != "" {
		manifestPath = filepath.Join(deps.RepoRoot, "docs", "system-state.toml")
	}
	if manifestPath == "" {
		return nil, errors.New("plan9adapter: State ManifestPath is required")
	}
	schemaPath := strings.TrimSpace(deps.SchemaPath)
	if schemaPath == "" && deps.RepoRoot != "" {
		schemaPath = filepath.Join(deps.RepoRoot, "docs", "system-state.schema.json")
	}
	if schemaPath == "" {
		return nil, errors.New("plan9adapter: State SchemaPath is required")
	}
	schema, err := manifest.LoadSchema(schemaPath)
	if err != nil {
		return nil, err
	}
	walker := deps.Walker
	if walker == nil {
		if strings.TrimSpace(deps.RepoRoot) == "" {
			return nil, errors.New("plan9adapter: State RepoRoot is required when Walker is nil")
		}
		walker = manifest.NewWalker(manifest.WalkerConfig{
			GitRepoRoot:        deps.RepoRoot,
			ADRIndexPath:       filepath.Join(deps.RepoRoot, "docs", "decisions", "_index.json"),
			GoModPath:          filepath.Join(deps.RepoRoot, "go.mod"),
			InvariantGrepRoot:  deps.RepoRoot,
			AutonomyStampPath:  deps.AutonomyStampPath,
			ZenSwarmVersion:    deps.Version,
			DoctrineRegistryFn: deps.DoctrineRegistryFn,
		})
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	regenerator := manifest.NewRegenerator(schema)
	a := &StateAdapter{
		manifestPath: manifestPath,
		schema:       schema,
		regenerator:  regenerator,
		differ:       manifest.NewDiffer(schema, regenerator),
		store:        deps.Store,
		walker:       walker,
		now:          now,
	}
	if deps.Store != nil {
		a.tracker = manifest.NewManualTracker(schema, regenerator, &auditRawStateEventAppender{
			store: deps.Store,
			now:   now,
		})
	}
	return a, nil
}

func (a *StateAdapter) Show(ctx context.Context) (handlers.StateManifestP9, error) {
	body, m, err := a.readManifest(ctx)
	if err != nil {
		return handlers.StateManifestP9{}, err
	}
	manual, err := a.schema.DiscoverManualFields()
	if err != nil {
		return handlers.StateManifestP9{}, err
	}
	return handlers.StateManifestP9{
		LastRegenerateUnix: m.Provenance.LastRegenerate.Unix(),
		ManualFieldCount:   len(manual),
		MissingSourceCount: len(m.Provenance.MissingSources),
		TomlContent:        string(body),
	}, nil
}

func (a *StateAdapter) Regenerate(ctx context.Context, dryRun bool) (handlers.StateRegenerateRespP9, error) {
	walked, err := a.walker.Walk(ctx)
	if err != nil {
		return handlers.StateRegenerateRespP9{}, err
	}
	next, err := a.regenerator.DryRun(ctx, walked.Manifest, a.manifestPath)
	if err != nil {
		return handlers.StateRegenerateRespP9{}, err
	}
	current, readErr := os.ReadFile(a.manifestPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return handlers.StateRegenerateRespP9{}, fmt.Errorf("plan9adapter: read state manifest: %w", readErr)
	}
	changed := changedManifestFields(current, next, readErr)
	resp := handlers.StateRegenerateRespP9{
		DryRun:        dryRun,
		ChangedFields: changed,
		Diff:          stateDiffString(changed),
	}
	if dryRun || bytes.Equal(current, next) {
		return resp, nil
	}
	if err := atomicWriteFile(a.manifestPath, next, 0o644); err != nil {
		return handlers.StateRegenerateRespP9{}, err
	}
	if a.store != nil {
		eventType := manifest.TypeStateRegenerated
		if len(walked.MissingSources) > 0 {
			eventType = manifest.TypeStateRegeneratePartial
		}
		if err := (&auditRawStateEventAppender{store: a.store, now: a.now}).AppendEvent(ctx, manifest.EventPayload{
			Type:           eventType,
			Reason:         "regenerated via Plan 9 state API",
			Timestamp:      a.now().UTC(),
			MissingSources: walked.MissingSources,
		}); err != nil {
			_ = atomicWriteFile(a.manifestPath, current, 0o644)
			return handlers.StateRegenerateRespP9{}, err
		}
	}
	return resp, nil
}

func (a *StateAdapter) Verify(ctx context.Context) (handlers.StateDiffP9, error) {
	walked, err := a.walker.Walk(ctx)
	if err != nil {
		return handlers.StateDiffP9{}, err
	}
	recent, err := a.recentStateEvents(ctx)
	if err != nil {
		return handlers.StateDiffP9{}, err
	}
	report, err := a.differ.Verify(ctx, walked.Manifest, a.manifestPath, a.now().UTC(), recent)
	if err != nil {
		return handlers.StateDiffP9{}, err
	}
	if !report.IsFailure() {
		return handlers.StateDiffP9{Match: true}, nil
	}
	return handlers.StateDiffP9{Match: false, Diff: stateDiffString(report.AutoDriftPaths)}, nil
}

func (a *StateAdapter) Pin(ctx context.Context, field, value, reason, operatorID string) error {
	if a.tracker == nil {
		return errors.New("plan9adapter: State event sink is required for Pin")
	}
	if strings.TrimSpace(reason) == "" {
		return manifest.ErrEmptyReason
	}
	if strings.TrimSpace(operatorID) == "" {
		operatorID = "anonymous"
	}
	if err := a.validatePinCandidate(ctx, field, value); err != nil {
		return err
	}
	return a.tracker.Pin(ctx, a.manifestPath, manifest.PinRequest{
		Path:       field,
		NewValue:   value,
		Reason:     reason,
		OperatorID: operatorID,
		Timestamp:  a.now().UTC(),
	})
}

func (a *StateAdapter) History(ctx context.Context, field string) ([]handlers.StateChangeP9, error) {
	if a.store == nil {
		return nil, errors.New("plan9adapter: State history store is not configured")
	}
	rows, err := a.store.DB().QueryContext(ctx,
		`SELECT payload_json, emitted_at FROM audit_events_raw
		  WHERE type = ?
		  ORDER BY emitted_at ASC, rowid ASC`,
		manifest.TypeStateManualFieldChanged,
	)
	if err != nil {
		return nil, fmt.Errorf("plan9adapter: query state history: %w", err)
	}
	defer rows.Close()
	var out []handlers.StateChangeP9
	for rows.Next() {
		var raw string
		var emittedAt int64
		if err := rows.Scan(&raw, &emittedAt); err != nil {
			return nil, fmt.Errorf("plan9adapter: scan state history: %w", err)
		}
		var payload manifest.EventPayload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, fmt.Errorf("plan9adapter: decode state history payload: %w", err)
		}
		if field != "" && payload.Field != field {
			continue
		}
		at := payload.Timestamp.Unix()
		if at == 0 {
			at = emittedAt
		}
		out = append(out, handlers.StateChangeP9{
			Field:      payload.Field,
			OldValue:   fmt.Sprint(payload.OldValue),
			NewValue:   fmt.Sprint(payload.NewValue),
			Reason:     payload.Reason,
			At:         at,
			OperatorID: payload.OperatorID,
		})
	}
	return out, rows.Err()
}

func (a *StateAdapter) validatePinCandidate(ctx context.Context, field string, value any) error {
	_, current, err := a.readManifest(ctx)
	if err != nil {
		return err
	}
	manual, err := a.schema.DiscoverManualFields()
	if err != nil {
		return fmt.Errorf("plan9adapter: discover manual state fields: %w", err)
	}
	if !stateManualPathAllowed(manual, field) {
		return fmt.Errorf("%w: %s", manifest.ErrManualFieldNotFound, field)
	}
	if err := setStateValueAtPath(&current, field, value); err != nil {
		return err
	}
	emitted, err := a.regenerator.Emit(current)
	if err != nil {
		return fmt.Errorf("plan9adapter: emit candidate state manifest: %w", err)
	}
	schemaValue, err := tomlBodyToSchemaValue(emitted)
	if err != nil {
		return err
	}
	if err := a.schema.Validate(schemaValue); err != nil {
		return fmt.Errorf("plan9adapter: state pin would violate schema: %w", err)
	}
	return nil
}

func (a *StateAdapter) readManifest(ctx context.Context) ([]byte, manifest.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return nil, manifest.Manifest{}, err
	}
	body, err := os.ReadFile(a.manifestPath)
	if err != nil {
		return nil, manifest.Manifest{}, fmt.Errorf("plan9adapter: read state manifest: %w", err)
	}
	var m manifest.Manifest
	if _, err := toml.NewDecoder(bytes.NewReader(body)).Decode(&m); err != nil {
		return nil, manifest.Manifest{}, fmt.Errorf("plan9adapter: decode state manifest: %w", err)
	}
	return body, m, nil
}

func (a *StateAdapter) recentStateEvents(ctx context.Context) ([]manifest.ChainAnchoredEvent, error) {
	if a.store == nil {
		return nil, nil
	}
	rows, err := a.store.DB().QueryContext(ctx,
		`SELECT type, emitted_at FROM audit_events_raw
		  WHERE type = ?
		  ORDER BY emitted_at ASC`,
		manifest.TypeStateManualFieldChanged,
	)
	if err != nil {
		return nil, fmt.Errorf("plan9adapter: query state events: %w", err)
	}
	defer rows.Close()
	var out []manifest.ChainAnchoredEvent
	for rows.Next() {
		var typ string
		var emittedAt int64
		if err := rows.Scan(&typ, &emittedAt); err != nil {
			return nil, fmt.Errorf("plan9adapter: scan state event: %w", err)
		}
		out = append(out, manifest.ChainAnchoredEvent{Type: typ, Timestamp: time.Unix(emittedAt, 0).UTC()})
	}
	return out, rows.Err()
}

type auditRawStateEventAppender struct {
	store *store.Store
	now   func() time.Time
}

func (a *auditRawStateEventAppender) AppendEvent(ctx context.Context, ev manifest.EventPayload) error {
	if a == nil || a.store == nil {
		return errors.New("plan9adapter: nil state event store")
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = a.now().UTC()
	}
	if ev.Type == "" {
		return errors.New("plan9adapter: state event type is required")
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("plan9adapter: marshal state event: %w", err)
	}
	id, err := randomHexID()
	if err != nil {
		return err
	}
	_, err = a.store.DB().ExecContext(ctx,
		`INSERT INTO audit_events_raw(id, project_id, type, payload_json, emitted_at)
		 VALUES (?, '', ?, ?, ?)`,
		id, ev.Type, string(raw), ev.Timestamp.Unix(),
	)
	if err != nil {
		return fmt.Errorf("plan9adapter: insert state audit event: %w", err)
	}
	return nil
}

func changedManifestFields(oldBody, newBody []byte, readErr error) []string {
	if readErr != nil {
		return []string{"<file-missing>"}
	}
	var oldM, newM manifest.Manifest
	if _, err := toml.NewDecoder(bytes.NewReader(oldBody)).Decode(&oldM); err != nil {
		return []string{"<malformed-toml>"}
	}
	if _, err := toml.NewDecoder(bytes.NewReader(newBody)).Decode(&newM); err != nil {
		return []string{"<generated-malformed-toml>"}
	}
	oldLeaves := map[string]any{}
	newLeaves := map[string]any{}
	collectManifestLeaves("", reflect.ValueOf(oldM), oldLeaves)
	collectManifestLeaves("", reflect.ValueOf(newM), newLeaves)
	keys := make(map[string]struct{}, len(oldLeaves)+len(newLeaves))
	for k := range oldLeaves {
		keys[k] = struct{}{}
	}
	for k := range newLeaves {
		keys[k] = struct{}{}
	}
	var changed []string
	for k := range keys {
		if !reflect.DeepEqual(oldLeaves[k], newLeaves[k]) {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}

func collectManifestLeaves(prefix string, v reflect.Value, out map[string]any) {
	if !v.IsValid() {
		return
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			out[prefix] = nil
			return
		}
		v = v.Elem()
	}
	if v.Type() == reflect.TypeOf(time.Time{}) || v.Kind() != reflect.Struct {
		out[prefix] = v.Interface()
		return
	}
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		name := tomlTagNameLocal(field.Tag.Get("toml"))
		if name == "" || name == "-" {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		collectManifestLeaves(path, v.Field(i), out)
	}
}

func stateDiffString(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	return "changed fields: " + strings.Join(fields, ", ")
}

func stateManualPathAllowed(paths []manifest.ManualFieldPath, want string) bool {
	for _, path := range paths {
		if path.Path == want {
			return true
		}
	}
	return false
}

func setStateValueAtPath(m *manifest.Manifest, path string, val any) error {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) != 2 {
		return fmt.Errorf("%w: %s (too short)", manifest.ErrManualFieldNotFound, path)
	}
	root := reflect.ValueOf(m).Elem()
	section := stateFieldByTOMLName(root, parts[0])
	if !section.IsValid() {
		return fmt.Errorf("%w: section %q not found", manifest.ErrManualFieldNotFound, parts[0])
	}
	field := stateFieldByTOMLName(section, parts[1])
	if !field.IsValid() || !field.CanSet() {
		return fmt.Errorf("%w: field %q not found or not settable", manifest.ErrManualFieldNotFound, path)
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

func stateFieldByTOMLName(v reflect.Value, name string) reflect.Value {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		if tomlTagNameLocal(field.Tag.Get("toml")) == name {
			return v.Field(i)
		}
	}
	return reflect.Value{}
}

func tomlBodyToSchemaValue(body []byte) (map[string]any, error) {
	var decoded map[string]any
	if _, err := toml.NewDecoder(bytes.NewReader(body)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("plan9adapter: decode candidate state manifest: %w", err)
	}
	normalizeSchemaValue(decoded)
	return decoded, nil
}

func normalizeSchemaValue(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			switch tv := child.(type) {
			case time.Time:
				x[k] = tv.UTC().Format(time.RFC3339)
			default:
				normalizeSchemaValue(child)
			}
		}
	case []any:
		for i, child := range x {
			if tv, ok := child.(time.Time); ok {
				x[i] = tv.UTC().Format(time.RFC3339)
				continue
			}
			normalizeSchemaValue(child)
		}
	}
}

func tomlTagNameLocal(tag string) string {
	if idx := strings.IndexByte(tag, ','); idx >= 0 {
		return tag[:idx]
	}
	return tag
}

func atomicWriteFile(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("plan9adapter: mkdir for atomic write: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return fmt.Errorf("plan9adapter: write tmp state manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("plan9adapter: rename state manifest: %w", err)
	}
	return nil
}
