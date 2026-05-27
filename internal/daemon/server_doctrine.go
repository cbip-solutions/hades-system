// SPDX-License-Identifier: MIT
// Package daemon — server_doctrine.go.
//
// Thin Server-side accessors that satisfy handlers.DoctrineHandlerCtx by
// routing to the internal/doctrine/* package singletons. Mirrors the
// server_audit_query.go + server_research_cache_admin.go pattern: keep
// server.go small + group doctrine wiring in one file.
//
// Boundary discipline (invariant generalized as invariant): this file
// imports internal/doctrine/{active,builtin,parser,reinforcement,reload,
// schema/v1,errors} which is allowed because daemon is the orchestrator
// that wires doctrine → handlers; doctrine itself never imports daemon
// (one-way edge, no cycle).
//
// (DoctrineState/DoctrineValidate(string)/DoctrineReload()) accessors that
// previously lived in server_phase_g_defaults.go. The new method signatures
// match the release handlers.DoctrineHandlerCtx interface and the
// client DTOs at internal/client/doctrine_v2.go.
package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	"github.com/cbip-solutions/hades-system/internal/doctrine/builtin"
	doctrineerrors "github.com/cbip-solutions/hades-system/internal/doctrine/errors"
	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reinforcement"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

var _ handlers.DoctrineHandlerCtx = (*Server)(nil)

func (s *Server) SetReloadWatcherForTest(w *reload.Watcher) { s.reloadWatcher = w }

func (s *Server) SetReloadWatcher(w *reload.Watcher) { s.reloadWatcher = w }

func (s *Server) SetDoctrineReinforceEngine(e *reinforcement.Engine) { s.reinforceEngine = e }

func (s *Server) SetDoctrinePendingChangesProvider(fn func() []string) {
	s.pendingChangesProvider = fn
}

func (s *Server) DoctrineActive(projectID string) (string, *v1.Schema, string, error) {
	defer func() {

		_ = recover()
	}()
	var schema *v1.Schema
	var source string
	if projectID != "" {

		schema = safeFor(projectID)
		source = "project"
	}
	if schema == nil {
		schema = safeActive()
		source = "embed"
	}
	if schema == nil {
		return "", nil, "", doctrineerrors.ErrDoctrineNotFound
	}
	name := doctrineNameForSchema(schema)
	if name == "" {
		// Defensive fallback: name not derivable from registry — use a
		// stable label so the response is shape-correct.
		//
		// SECURITY NOTE (M-5 + I-1 closure): "active" is intentionally
		// NOT a recognised session doctrine. If this fallback ever
		// flows into handlers.DoctrineVisible as sessionDoctrine, the
		// switch-case whitelist (audit_event.go:DoctrineVisible) routes
		// to the default branch → fail closed (no row visible).
		// The fallback is a SHAPE-correctness signal for the HTTP
		// response only; it is NOT a usable doctrine identity.
		// Extending the whitelist to accept "active" would silently
		// authorise reads under a non-doctrine label — DO NOT do that.
		name = "active"
	}
	return name, schema, source, nil
}

func safeActive() (out *v1.Schema) {
	defer func() {
		if r := recover(); r != nil {
			out = nil
		}
	}()
	return active.Active()
}

func safeFor(projectID string) (out *v1.Schema) {
	defer func() {
		if r := recover(); r != nil {
			out = nil
		}
	}()
	return active.For(projectID)
}

func doctrineNameForSchema(schema *v1.Schema) string {
	if schema == nil {
		return ""
	}
	all, err := builtin.LoadAll()
	if err != nil {
		return ""
	}
	for name, sch := range all {
		if sch == schema {
			return name
		}
	}

	for name, sch := range all {
		if sch.SchemaVersion == schema.SchemaVersion && sch.DoctrineVersion == schema.DoctrineVersion {
			return name
		}
	}
	return ""
}

func (s *Server) DoctrineList(sourceFilter string) ([]client.DoctrineV2ListItem, error) {
	if sourceFilter != "" && sourceFilter != "all" && sourceFilter != "embed" {

		return []client.DoctrineV2ListItem{}, nil
	}
	all, err := builtin.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("doctrine list: %w", err)
	}
	out := make([]client.DoctrineV2ListItem, 0, len(all))
	for name, sch := range all {
		out = append(out, client.DoctrineV2ListItem{
			Name:            name,
			Source:          "embed",
			SchemaVersion:   sch.SchemaVersion,
			DoctrineVersion: sch.DoctrineVersion,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Server) DoctrineShow(name, format, section string) (string, string, error) {
	all, err := builtin.LoadAll()
	if err != nil {
		return "", "", fmt.Errorf("doctrine show: load: %w", err)
	}
	sch, ok := all[name]
	if !ok {
		return "", "", fmt.Errorf("show %q: %w", name, doctrineerrors.ErrDoctrineNotFound)
	}
	if format == "" {
		format = "toml"
	}

	var body string
	switch format {
	case "toml":
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(sch); err != nil {
			return "", "", fmt.Errorf("toml encode %q: %w", name, err)
		}
		body = buf.String()
	case "json":

		jb, err := jsonMarshal(sch)
		if err != nil {
			return "", "", fmt.Errorf("json encode %q: %w", name, err)
		}
		body = string(jb)
	case "md", "markdown":

		if s.reinforceEngine != nil {
			rendered, rerr := s.reinforceEngine.Render(sch, &reinforcement.Vars{
				DoctrineName: name,
				TaskKind:     "worker",
			})
			if rerr != nil {
				return "", "", fmt.Errorf("reinforce render %q: %w", name, rerr)
			}
			body = rendered
		} else {

			body = fmt.Sprintf("# Doctrine: %s\n\nschema_version: %s\ndoctrine_version: %s\n",
				name, sch.SchemaVersion, sch.DoctrineVersion)
		}
	default:

		var buf bytes.Buffer
		_ = toml.NewEncoder(&buf).Encode(sch)
		body = buf.String()
		format = "toml"
	}

	if section != "" {
		body = sliceSection(body, section, format)
	}
	return format, body, nil
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func sliceSection(body, section, format string) string {
	if format != "toml" {
		return body
	}
	header := "[" + section + "]"
	idx := strings.Index(body, header)
	if idx < 0 {
		return body
	}

	rest := body[idx:]
	end := strings.Index(rest[len(header):], "\n[")
	if end < 0 {
		return rest
	}
	return rest[:len(header)+end]
}

// DoctrineValidate parses TOML bytes via + validates via
// Returns nil on success; sentinel-wrapped error on failure (handler
// discriminator translates to 422 / 400 / 500 per Hard Rule 11).
//
// The againstBaseline argument is currently advisory — a future
// extension can run candidate.ValidateTighten(active.For(againstBaseline))
// to surface override-loosen attempts via the same /validate surface.
// (CLI's `hades doctrine-v2 validate --against-baseline` populates the
// flag for forward-compat).
//
// invariant contract reconciliation: user TOMLs MUST
// NOT declare [doctrine_transverse] (parser's ParseOpts{} default rejects
// the section), but the in-memory Validate() requires Transverse fields
// to equal TransverseExpected() (all-true). The accessor reconciles by
// populating the four transverse axioms BEFORE Validate so a user TOML
// without the section parses → fills → validates cleanly. This preserves
// invariant source-level enforcement (user TOMLs literally cannot
// override transverse via TOML) while letting the user-side validate
// pipeline succeed.
func (s *Server) DoctrineValidate(tomlContent, againstBaseline string) error {
	sch := &v1.Schema{}
	if err := parser.ParseStrict([]byte(tomlContent), "http:/v1/doctrine/validate", sch, parser.ParseOpts{}); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	sch.Transverse = v1.TransverseExpected()
	if err := sch.Validate(); err != nil {

		return fmt.Errorf("validate: %w", err)
	}
	if againstBaseline != "" {
		if baseline := safeFor(againstBaseline); baseline != nil {
			if vErr := sch.ValidateTighten(baseline); vErr != nil {
				return fmt.Errorf("tighten %q: %w", againstBaseline, vErr)
			}
		}
	}
	return nil
}

func (s *Server) DoctrineStatus(projectAlias string) (handlers.DoctrineStatusSnapshot, error) {
	name, sch, source, err := s.DoctrineActive(projectAlias)
	if err != nil {
		return handlers.DoctrineStatusSnapshot{}, err
	}
	pending := []string{}
	if s.pendingChangesProvider != nil {
		pending = s.pendingChangesProvider()
	}
	return handlers.DoctrineStatusSnapshot{
		Active: client.DoctrineV2ActiveResp{
			Name:            name,
			SchemaVersion:   sch.SchemaVersion,
			DoctrineVersion: sch.DoctrineVersion,
			Source:          source,
		},

		LastReloadAt:   time.Time{},
		LastReloadOk:   true,
		WatcherHealthy: s.reloadWatcher != nil,
		PendingChanges: pending,
	}, nil
}

func (s *Server) DoctrineHistory(since time.Time, filter string, limit int) ([]handlers.DoctrineHistoryEventRow, error) {

	_ = since
	_ = filter
	_ = limit
	return []handlers.DoctrineHistoryEventRow{}, nil
}

func (s *Server) DoctrineDiff(a, b, section string) (string, string, []handlers.DoctrineDiffEntry, error) {
	all, err := builtin.LoadAll()
	if err != nil {
		return "", "", nil, fmt.Errorf("doctrine diff: load: %w", err)
	}
	schemaA, ok := all[a]
	if !ok {
		return "", "", nil, fmt.Errorf("a=%q: %w", a, doctrineerrors.ErrDoctrineNotFound)
	}
	schemaB, ok := all[b]
	if !ok {
		return "", "", nil, fmt.Errorf("b=%q: %w", b, doctrineerrors.ErrDoctrineNotFound)
	}
	diffs := diffSchemas(schemaA, schemaB, section)
	return a, b, diffs, nil
}

func diffSchemas(a, b *v1.Schema, section string) []handlers.DoctrineDiffEntry {
	out := []handlers.DoctrineDiffEntry{}
	aBuf := bytes.Buffer{}
	bBuf := bytes.Buffer{}
	_ = toml.NewEncoder(&aBuf).Encode(a)
	_ = toml.NewEncoder(&bBuf).Encode(b)
	mA := tomlLinesToMap(aBuf.String())
	mB := tomlLinesToMap(bBuf.String())
	seen := map[string]bool{}
	for k, va := range mA {
		seen[k] = true
		if section != "" && !strings.HasPrefix(k, section+".") && k != section {
			continue
		}
		if vb, ok := mB[k]; ok {
			if va != vb {
				out = append(out, handlers.DoctrineDiffEntry{
					Path:   k,
					From:   va,
					To:     vb,
					Status: "changed",
				})
			}
		} else {
			out = append(out, handlers.DoctrineDiffEntry{
				Path:   k,
				From:   va,
				To:     "",
				Status: "removed",
			})
		}
	}
	for k, vb := range mB {
		if seen[k] {
			continue
		}
		if section != "" && !strings.HasPrefix(k, section+".") && k != section {
			continue
		}
		out = append(out, handlers.DoctrineDiffEntry{
			Path:   k,
			From:   "",
			To:     vb,
			Status: "added",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func tomlLinesToMap(rendered string) map[string]string {
	m := map[string]string{}
	section := ""
	for _, l := range strings.Split(rendered, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") {
			section = strings.Trim(l, "[]")
			continue
		}
		if eq := strings.Index(l, "="); eq > 0 {
			key := strings.TrimSpace(l[:eq])
			val := strings.TrimSpace(l[eq+1:])
			path := key
			if section != "" {
				path = section + "." + key
			}
			m[path] = val
		}
	}
	return m
}

func (s *Server) DoctrineMigrate(tomlContent, fromSchemaVersion string) (string, string, []string, error) {
	sch := &v1.Schema{}
	if err := parser.ParseStrict([]byte(tomlContent), "http:/v1/doctrine/migrate", sch, parser.ParseOpts{}); err != nil {
		return "", "", nil, fmt.Errorf("parse: %w", err)
	}

	const currentSchemaVersion = "1.0"

	cloneForEncode := *sch
	cloneForEncode.Transverse = v1.TransverseConfig{}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(&cloneForEncode); err != nil {
		return "", "", nil, fmt.Errorf("re-encode migrated schema: %w", err)
	}
	return currentSchemaVersion, buf.String(), nil, nil
}

func (s *Server) DoctrineReinforce(req client.DoctrineV2ReinforceReq) (string, error) {
	if s.reinforceEngine == nil {
		return "", errors.New("reinforce engine not wired")
	}
	var sch *v1.Schema
	if req.ProjectAlias != "" {
		sch = safeFor(req.ProjectAlias)
	} else {
		sch = safeActive()
	}
	if sch == nil {
		return "", doctrineerrors.ErrDoctrineNotFound
	}
	name := doctrineNameForSchema(sch)
	if name == "" {

		name = "max-scope"
	}
	return s.reinforceEngine.Render(sch, &reinforcement.Vars{
		DoctrineName: name,
		TaskKind:     req.TaskKind,
		ProjectAlias: req.ProjectAlias,
		CurrentStage: req.Stage,
		CurrentPhase: req.Phase,
		PlanID:       req.PlanID,
	})
}

func (s *Server) DoctrineReload(path string) error {
	if s.bucketRegistry != nil {
		s.bucketRegistry.InvalidateAll()
	}
	if s.reloadWatcher == nil {
		return errors.New("reload: daemon has no active reload.Watcher (no doctrine files registered)")
	}
	return s.reloadWatcher.NotifyForce(path)
}

// DoctrineReloadEvents returns a fresh subscription to the reload event
// channel. Each call returns a new buffered channel; callers MUST consume
// or unsubscribe (the daemon's reload.Watcher.SubscribeReloadEvents
// returns a per-subscriber channel with non-blocking emit on full buffer).
func (s *Server) DoctrineReloadEvents() <-chan reload.DoctrineReloaded {
	if s.reloadWatcher == nil {
		return nil
	}
	return s.reloadWatcher.SubscribeReloadEvents()
}

func (s *Server) DoctrineReloadFailedEvents() <-chan reload.DoctrineReloadFailed {
	if s.reloadWatcher == nil {
		return nil
	}
	return s.reloadWatcher.SubscribeReloadFailedEvents()
}

func (s *Server) DoctrineReloadTimeout() time.Duration {
	return 5 * time.Second
}

func (s *Server) DoctrineUnsubscribeReloadEvents(ch <-chan reload.DoctrineReloaded) {
	if s.reloadWatcher == nil {
		return
	}
	s.reloadWatcher.UnsubscribeReloadEvents(ch)
}

func (s *Server) DoctrineUnsubscribeReloadFailedEvents(ch <-chan reload.DoctrineReloadFailed) {
	if s.reloadWatcher == nil {
		return
	}
	s.reloadWatcher.UnsubscribeReloadFailedEvents(ch)
}
