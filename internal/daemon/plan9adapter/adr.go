// SPDX-License-Identifier: MIT
package plan9adapter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/cbip-solutions/hades-system/internal/adr"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type ADRAdapterDeps struct {
	Dir         string
	SchemaPath  string
	Store       *store.Store
	DefaultPlan string
	Clock       func() string
	Now         func() time.Time
}

type ADRAdapter struct {
	mu          sync.Mutex
	dir         string
	indexer     *adr.Indexer
	store       *store.Store
	sink        adr.EventSink
	defaultPlan string
	clock       func() string
	now         func() time.Time
}

var _ handlers.ADRCtx = (*ADRAdapter)(nil)

var adrPlanTagPattern = regexp.MustCompile(`^plan-[0-9]+(-followup)?$`)

func NewADRAdapter(deps ADRAdapterDeps) (*ADRAdapter, error) {
	if strings.TrimSpace(deps.Dir) == "" {
		return nil, errors.New("plan9adapter: ADR Dir is required")
	}
	schemaPath := deps.SchemaPath
	if schemaPath == "" {
		schemaPath = filepath.Join(deps.Dir, "_schema.json")
	}
	validator, err := adr.NewValidator(schemaPath)
	if err != nil {
		return nil, err
	}
	clock := deps.Clock
	if clock == nil {
		clock = func() string { return time.Now().UTC().Format(time.RFC3339) }
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	defaultPlan := deps.DefaultPlan
	if defaultPlan == "" {
		defaultPlan = "HADES design"
	}
	a := &ADRAdapter{
		dir:         deps.Dir,
		indexer:     adr.NewIndexer(validator, clock),
		store:       deps.Store,
		defaultPlan: defaultPlan,
		clock:       clock,
		now:         now,
	}
	if deps.Store != nil {
		a.sink = &auditRawADREventSink{store: deps.Store, now: now}
	}
	return a, nil
}

func (a *ADRAdapter) Propose(ctx context.Context, topic, planRange string) (handlers.ADRDoc, error) {
	if err := ctx.Err(); err != nil {
		return handlers.ADRDoc{}, err
	}
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return handlers.ADRDoc{}, errors.New("plan9adapter: ADR topic is required")
	}
	if a.sink == nil {
		return handlers.ADRDoc{}, errors.New("plan9adapter: ADR event sink is required for Propose")
	}
	plan := strings.TrimSpace(planRange)
	if plan == "" {
		plan = a.defaultPlan
	}
	if !adrPlanTagPattern.MatchString(plan) {
		return handlers.ADRDoc{}, fmt.Errorf("plan9adapter: ADR plan %q does not match %s", plan, adrPlanTagPattern.String())
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	var id string
	var path string
	for {
		next, err := a.nextID(ctx)
		if err != nil {
			return handlers.ADRDoc{}, err
		}
		id = fmt.Sprintf("ADR-%04d", next)
		path = filepath.Join(a.dir, fmt.Sprintf("%04d-%s.md", next, slugify(topic)))
		body := a.renderProposedADR(id, topic, plan)
		if err := writeFileExclusive(path, []byte(body), 0o644); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return handlers.ADRDoc{}, fmt.Errorf("plan9adapter: write proposed ADR: %w", err)
		}
		break
	}
	now := a.now().UTC()
	if err := a.sink.Emit(adr.EvtADRProposed, adr.EventPayload{
		ADRID:      id,
		StatusTo:   adr.StatusProposed,
		Reason:     "proposed via HADES design ADR API",
		Timestamp:  now,
		OperatorID: "",
	}); err != nil {
		_ = os.Remove(path)
		return handlers.ADRDoc{}, fmt.Errorf("plan9adapter: emit ADR proposed event: %w", err)
	}
	return a.Show(ctx, id)
}

func (a *ADRAdapter) Show(ctx context.Context, id string) (handlers.ADRDoc, error) {
	path, parsed, ok, err := a.findADR(ctx, id)
	if err != nil || !ok {
		return handlers.ADRDoc{}, err
	}
	parsed.Path = path
	return adrDocFromParsed(parsed), nil
}

func (a *ADRAdapter) List(ctx context.Context, filter handlers.ADRListFilter) ([]handlers.ADRDoc, error) {
	adrs, err := a.scanADRs(ctx)
	if err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	out := make([]handlers.ADRDoc, 0, len(adrs))
	for _, parsed := range adrs {
		doc := adrDocFromParsed(parsed)
		if filter.Status != "" && doc.Status != filter.Status {
			continue
		}
		if filter.Plan != "" && doc.Plan != filter.Plan {
			continue
		}
		if filter.RiskLevel != "" && doc.RiskLevel != filter.RiskLevel {
			continue
		}
		out = append(out, doc)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (a *ADRAdapter) Graph(ctx context.Context, fromID string, depth int) (handlers.ADRGraph, error) {
	if depth <= 0 {
		depth = 1
	}
	graph, err := adr.WalkAndEmitGraph(ctx, a.dir, a.clock)
	if err != nil {
		return handlers.ADRGraph{}, err
	}
	dist := map[string]int{fromID: 0}
	queue := []string{fromID}
	neighbors := make(map[string][]string)
	for _, e := range graph.Edges {
		neighbors[e.From] = append(neighbors[e.From], e.To)
		neighbors[e.To] = append(neighbors[e.To], e.From)
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if dist[cur] >= depth {
			continue
		}
		for _, n := range neighbors[cur] {
			if _, seen := dist[n]; seen {
				continue
			}
			dist[n] = dist[cur] + 1
			queue = append(queue, n)
		}
	}
	out := handlers.ADRGraph{}
	for _, n := range graph.Nodes {
		if _, ok := dist[n.ID]; ok {
			out.Nodes = append(out.Nodes, handlers.ADRGraphNode{ID: n.ID, Status: string(n.Status)})
		}
	}
	for _, e := range graph.Edges {
		if _, ok := dist[e.From]; !ok {
			continue
		}
		if _, ok := dist[e.To]; !ok {
			continue
		}
		out.Edges = append(out.Edges, handlers.ADRGraphEdge{
			From: e.From,
			To:   e.To,
			Type: string(e.Kind),
		})
	}
	return out, nil
}

func (a *ADRAdapter) History(ctx context.Context, id string) ([]handlers.ADRTransition, error) {
	if a.store == nil {
		return nil, errors.New("plan9adapter: ADR history store is not configured")
	}
	rows, err := a.store.DB().QueryContext(ctx,
		`SELECT type, payload_json, emitted_at FROM audit_events_raw
		  WHERE type IN (?, ?, ?, ?, ?)
		  ORDER BY emitted_at ASC, rowid ASC`,
		string(adr.EvtADRProposed),
		string(adr.EvtADRAccepted),
		string(adr.EvtADRRejected),
		string(adr.EvtADRSuperseded),
		string(adr.EvtADRDeprecated),
	)
	if err != nil {
		return nil, fmt.Errorf("plan9adapter: query ADR history: %w", err)
	}
	defer rows.Close()
	var out []handlers.ADRTransition
	for rows.Next() {
		var typ, raw string
		var emittedAt int64
		if err := rows.Scan(&typ, &raw, &emittedAt); err != nil {
			return nil, fmt.Errorf("plan9adapter: scan ADR history: %w", err)
		}
		var payload adr.EventPayload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, fmt.Errorf("plan9adapter: decode ADR history payload: %w", err)
		}
		if payload.ADRID != id {
			continue
		}
		at := payload.Timestamp.Unix()
		if at == 0 {
			at = emittedAt
		}
		status := string(payload.StatusTo)
		if status == "" {
			status = typ
		}
		out = append(out, handlers.ADRTransition{
			ID:     payload.ADRID,
			Status: status,
			At:     at,
			Reason: payload.Reason,
		})
	}
	return out, rows.Err()
}

func (a *ADRAdapter) Accept(ctx context.Context, id, reason string) error {
	if a.sink == nil {
		return errors.New("plan9adapter: ADR event sink is required for Accept")
	}
	path, _, ok, err := a.findADR(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("plan9adapter: ADR %s not found", id)
	}
	return a.transitionWithRollback(path, func() error {
		return adr.Accept(ctx, path, "", reason, a.sink, a.now)
	})
}

func (a *ADRAdapter) Reject(ctx context.Context, id, reason string) error {
	if a.sink == nil {
		return errors.New("plan9adapter: ADR event sink is required for Reject")
	}
	path, _, ok, err := a.findADR(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("plan9adapter: ADR %s not found", id)
	}
	return a.transitionWithRollback(path, func() error {
		return adr.Reject(ctx, path, "", reason, a.sink, a.now)
	})
}

func (a *ADRAdapter) Supersede(ctx context.Context, oldID, newID, reason string) error {
	if a.sink == nil {
		return errors.New("plan9adapter: ADR event sink is required for Supersede")
	}
	path, _, ok, err := a.findADR(ctx, oldID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("plan9adapter: ADR %s not found", oldID)
	}
	return a.transitionWithRollback(path, func() error {
		return adr.Supersede(ctx, path, newID, "", reason, a.sink, a.now)
	})
}

func (a *ADRAdapter) RegenerateIndex(ctx context.Context, dryRun bool) (handlers.ADRManifest, error) {
	idx, graph, err := a.indexer.Generate(ctx, a.dir)
	if err != nil {
		return handlers.ADRManifest{}, err
	}
	idxRaw, err := adr.MarshalIndex(idx)
	if err != nil {
		return handlers.ADRManifest{}, err
	}
	graphRaw, err := adr.MarshalGraph(graph)
	if err != nil {
		return handlers.ADRManifest{}, err
	}
	if !dryRun {
		if err := adr.WriteIndex(filepath.Join(a.dir, "_index.json"), idx); err != nil {
			return handlers.ADRManifest{}, err
		}
		if err := adr.WriteGraph(filepath.Join(a.dir, "_graph.json"), graph); err != nil {
			return handlers.ADRManifest{}, err
		}
	}
	generatedAt, _ := time.Parse(time.RFC3339, idx.GeneratedAt)
	return handlers.ADRManifest{
		GeneratedAt: generatedAt.Unix(),
		ADRCount:    len(idx.Entries),
		Manifest:    string(idxRaw),
		Graph:       string(graphRaw),
	}, nil
}

func (a *ADRAdapter) renderProposedADR(id, topic, plan string) string {
	date := a.now().UTC().Format("2006-01-02")
	return fmt.Sprintf("---\nid: %s\ntitle: %s\nstatus: proposed\ndate: %q\nplan: %s\ntags: []\n---\n\n# %s\n",
		id, yamlQuote(topic), date, plan, topic)
}

func (a *ADRAdapter) nextID(ctx context.Context) (int, error) {
	paths, err := a.adrFilePaths(ctx)
	if err != nil {
		return 0, err
	}
	maxID := 0
	for _, path := range paths {
		if n, ok := adrNumberFromFilename(filepath.Base(path)); ok && n > maxID {
			maxID = n
		}
		parsed, err := adr.ParseFile(path)
		if err != nil {
			return 0, err
		}
		if !parsed.HasFrontmatter() {
			continue
		}
		id := strings.TrimPrefix(parsed.Frontmatter.ID, "ADR-")
		if n, err := strconv.Atoi(id); err == nil && n > maxID {
			maxID = n
		}
	}
	return maxID + 1, nil
}

func (a *ADRAdapter) transitionWithRollback(path string, transition func() error) error {
	before, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("plan9adapter: read ADR before transition: %w", err)
	}
	if err := transition(); err != nil {
		after, readErr := os.ReadFile(path)
		if readErr == nil && string(after) != string(before) {
			if rollbackErr := atomicWriteFile(path, before, 0o644); rollbackErr != nil {
				return fmt.Errorf("%w (rollback also failed: %v)", err, rollbackErr)
			}
		}
		return err
	}
	return nil
}

func (a *ADRAdapter) findADR(ctx context.Context, id string) (string, *adr.ADR, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, false, err
	}
	paths, err := a.adrFilePaths(ctx)
	if err != nil {
		return "", nil, false, err
	}
	for _, path := range paths {
		parsed, err := adr.ParseFile(path)
		if err != nil {
			return "", nil, false, err
		}
		if parsed.HasFrontmatter() && parsed.Frontmatter.ID == id {
			return path, parsed, true, nil
		}
	}
	return "", nil, false, nil
}

func (a *ADRAdapter) scanADRs(ctx context.Context) ([]*adr.ADR, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	paths, err := a.adrFilePaths(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*adr.ADR, 0, len(paths))
	for _, path := range paths {
		parsed, err := adr.ParseFile(path)
		if err != nil {
			return nil, err
		}
		if !parsed.HasFrontmatter() {
			continue
		}
		parsed.Path = path
		out = append(out, parsed)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Frontmatter.ID < out[j].Frontmatter.ID
	})
	return out, nil
}

func (a *ADRAdapter) adrFilePaths(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dirs := []string{a.dir, filepath.Join(a.dir, "proposed"), filepath.Join(a.dir, "rejected")}
	var paths []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && dir != a.dir {
				continue
			}
			return nil, fmt.Errorf("plan9adapter: read ADR dir %s: %w", dir, err)
		}
		for _, e := range entries {
			if !isADRMarkdownFile(e) {
				continue
			}
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

type auditRawADREventSink struct {
	store *store.Store
	now   func() time.Time
}

func (s *auditRawADREventSink) Emit(t adr.EventType, p adr.EventPayload) error {
	if s == nil || s.store == nil {
		return errors.New("plan9adapter: nil ADR event store")
	}
	if p.Timestamp.IsZero() {
		p.Timestamp = s.now().UTC()
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("plan9adapter: marshal ADR event: %w", err)
	}
	id, err := randomHexID()
	if err != nil {
		return err
	}
	_, err = s.store.DB().Exec(
		`INSERT INTO audit_events_raw(id, project_id, type, payload_json, emitted_at)
		 VALUES (?, '', ?, ?, ?)`,
		id, string(t), string(raw), p.Timestamp.Unix(),
	)
	if err != nil {
		return fmt.Errorf("plan9adapter: insert ADR audit event: %w", err)
	}
	return nil
}

func adrDocFromParsed(parsed *adr.ADR) handlers.ADRDoc {
	fm := parsed.Frontmatter
	ts := unixDate(fm.Date)
	return handlers.ADRDoc{
		ID:          fm.ID,
		Status:      string(fm.Status),
		Topic:       fm.Title,
		Plan:        fm.Plan,
		RiskLevel:   string(fm.RiskLevel),
		Frontmatter: frontmatterMap(fm),
		Body:        parsed.Body,
		CreatedAt:   ts,
		UpdatedAt:   ts,
	}
}

func frontmatterMap(fm adr.Frontmatter) map[string]string {
	out := map[string]string{
		"id":     fm.ID,
		"title":  fm.Title,
		"status": string(fm.Status),
		"date":   fm.Date,
		"plan":   fm.Plan,
	}
	if fm.RiskLevel != "" {
		out["risk-level"] = string(fm.RiskLevel)
	}
	if fm.SupersededBy != "" {
		out["superseded-by"] = fm.SupersededBy
	}
	if len(fm.Tags) > 0 {
		out["tags"] = strings.Join(fm.Tags, ",")
	}
	if len(fm.Supersedes) > 0 {
		out["supersedes"] = strings.Join(fm.Supersedes, ",")
	}
	if len(fm.RelatesTo) > 0 {
		out["relates-to"] = strings.Join(fm.RelatesTo, ",")
	}
	return out
}

func isADRMarkdownFile(e os.DirEntry) bool {
	if !e.Type().IsRegular() {
		return false
	}
	name := e.Name()
	return strings.HasSuffix(strings.ToLower(name), ".md") && !strings.HasPrefix(name, "_")
}

func adrNumberFromFilename(name string) (int, bool) {
	if len(name) < 4 {
		return 0, false
	}
	n, err := strconv.Atoi(name[:4])
	if err != nil {
		return 0, false
	}
	return n, true
}

func writeFileExclusive(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("plan9adapter: mkdir for ADR write: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(body)
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	return nil
}

func unixDate(raw string) int64 {
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return 0
	}
	return t.Unix()
}

func randomHexID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("plan9adapter: random event id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func slugify(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "decision"
	}
	if len(out) > 80 {
		out = strings.Trim(out[:80], "-")
	}
	return out
}

func yamlQuote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}
