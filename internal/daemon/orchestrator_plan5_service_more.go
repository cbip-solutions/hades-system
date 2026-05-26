// SPDX-License-Identifier: MIT
// Package daemon — orchestrator_plan5_service_more.go (Plan 5 Phase N).
//
// Companion file to orchestrator_plan5_service.go split for readability.
// Hosts the doctrine + safetynet + doctor health surfaces, plus the
// shared adapter shims (safetynet.Emitter → eventlog.Log,
// autonomy.EventEmitter → eventlog.Log, file/exec dependency adapters).
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/safetynet"
)

var adrProposalRE = regexp.MustCompile(`^(\d{4})-(.+)\.md$`)

func (s *Plan5OrchestratorService) DoctrineProposeList() (client.DoctrineProposalList, error) {
	if s.decisionsDir == "" {
		return client.DoctrineProposalList{}, nil
	}
	var out []client.DoctrineProposal
	for _, sub := range []struct{ dir, status string }{
		{filepath.Join(s.decisionsDir, "proposed"), "proposed"},
		{s.decisionsDir, "applied"},
		{filepath.Join(s.decisionsDir, "rejected"), "denied"},
	} {
		entries, err := os.ReadDir(sub.dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return client.DoctrineProposalList{}, fmt.Errorf("doctrine list: read %s: %w", sub.dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !adrProposalRE.MatchString(e.Name()) {
				continue
			}
			path := filepath.Join(sub.dir, e.Name())
			st, err := os.Stat(path)
			if err != nil {
				continue
			}
			body, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			id := adrIDFromFilename(e.Name())
			out = append(out, client.DoctrineProposal{
				ID:           id,
				Title:        adrTitleFromBody(string(body), e.Name()),
				Status:       sub.status,
				ProposedAt:   st.ModTime().Unix(),
				BodyMarkdown: string(body),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return client.DoctrineProposalList{Proposals: out}, nil
}

func (s *Plan5OrchestratorService) DoctrineProposeShow(id string) (client.DoctrineProposal, error) {
	list, err := s.DoctrineProposeList()
	if err != nil {
		return client.DoctrineProposal{}, err
	}
	wanted := strings.ToUpper(strings.TrimSpace(id))
	for _, p := range list.Proposals {
		if strings.ToUpper(p.ID) == wanted {
			return p, nil
		}
	}
	return client.DoctrineProposal{}, fmt.Errorf("doctrine: ADR %q not found", id)
}

func (s *Plan5OrchestratorService) DoctrineAck(req client.DoctrineDecision) error {
	return s.recordDoctrineDecision(req, "ack")
}

func (s *Plan5OrchestratorService) DoctrineDeny(req client.DoctrineDecision) error {
	return s.recordDoctrineDecision(req, "deny")
}

func (s *Plan5OrchestratorService) DoctrineRevert(req client.DoctrineDecision) error {
	if s.reverter == nil {
		return errors.New("doctrine revert: RepoRoot not configured (Plan 5 N-cleanup-1 graceful-degradation)")
	}
	adrID, err := parseADRID(req.ID)
	if err != nil {
		return fmt.Errorf("doctrine revert: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	operator := req.Reason
	if operator == "" {
		operator = "operator"
	}
	if err := s.reverter.Revert(ctx, adrID, operator); err != nil {
		return fmt.Errorf("doctrine revert: %w", err)
	}
	return nil
}

func (s *Plan5OrchestratorService) recordDoctrineDecision(req client.DoctrineDecision, decision string) error {
	if req.ID == "" {
		return errors.New("doctrine: ID is required")
	}
	adrID, err := parseADRID(req.ID)
	if err != nil {
		return err
	}
	ctx := context.WithoutCancel(context.Background())
	ev := eventlog.Event{
		Type:      eventlog.EvtDoctrineAmendmentSuppressed,
		SessionID: "operator-action",
		ProjectID: "doctrine-amendment",
		Timestamp: s.cfg.Clock.Now(),
		Payload: map[string]any{
			"adr_id":   adrID,
			"decision": decision,
			"reason":   req.Reason,
		},
	}
	if _, err := s.eventLog.Append(ctx, ev); err != nil {
		s.adaptersClean.Store(false)
		return fmt.Errorf("doctrine %s: append event: %w", decision, err)
	}
	return nil
}

func parseADRID(s string) (int, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	s = strings.TrimPrefix(s, "ADR-")
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid ADR id %q: %w", s, err)
	}
	return n, nil
}

var validProposeCategories = map[string]struct{}{
	"cost":     {},
	"merge":    {},
	"recovery": {},
}

const (
	plan8ADRRangeMin = 50
	plan8ADRRangeMax = 59
)

func (s *Plan5OrchestratorService) DoctrinePropose(req client.DoctrineProposeRequest) (client.DoctrineProposeResponse, error) {

	if strings.TrimSpace(req.RulePath) == "" {
		return client.DoctrineProposeResponse{}, errors.New("missing_rule_path: rule_path cannot be empty")
	}
	if strings.TrimSpace(req.NewValue) == "" {
		return client.DoctrineProposeResponse{}, errors.New("missing_new_value: new_value cannot be empty")
	}
	if strings.TrimSpace(req.Justification) == "" {
		return client.DoctrineProposeResponse{}, errors.New("missing_justification: justification cannot be empty")
	}
	if strings.TrimSpace(req.Category) == "" {
		return client.DoctrineProposeResponse{}, errors.New("missing_category: category cannot be empty")
	}
	if _, ok := validProposeCategories[req.Category]; !ok {
		return client.DoctrineProposeResponse{}, fmt.Errorf("invalid_category: category %q not in {cost, merge, recovery}", req.Category)
	}
	if s.decisionsDir == "" {
		return client.DoctrineProposeResponse{}, errors.New("invalid_rule_path: decisions directory not configured (RepoRoot missing)")
	}

	used := map[int]bool{}
	for _, dir := range []string{
		filepath.Join(s.decisionsDir, "proposed"),
		s.decisionsDir,
		filepath.Join(s.decisionsDir, "rejected"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			m := adrProposalRE.FindStringSubmatch(e.Name())
			if m == nil {
				continue
			}
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			used[n] = true
		}
	}

	allocated := 0
	for i := plan8ADRRangeMin; i <= plan8ADRRangeMax; i++ {
		if !used[i] {
			allocated = i
			break
		}
	}
	if allocated == 0 {
		return client.DoctrineProposeResponse{}, fmt.Errorf("invalid_rule_path: Plan 8 ADR range %04d-%04d exhausted (inv-zen-103); operator should triage existing proposals before allocating new",
			plan8ADRRangeMin, plan8ADRRangeMax)
	}

	slug := strings.ReplaceAll(req.RulePath, ".", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	slug = strings.ToLower(slug)
	if len(slug) > 60 {
		slug = slug[:60]
	}
	filename := fmt.Sprintf("%04d-%s.md", allocated, slug)
	proposedDir := filepath.Join(s.decisionsDir, "proposed")
	if err := os.MkdirAll(proposedDir, 0o755); err != nil {
		return client.DoctrineProposeResponse{}, fmt.Errorf("invalid_rule_path: mkdir proposed/: %w", err)
	}
	absPath := filepath.Join(proposedDir, filename)
	now := s.cfg.Clock.Now().UTC()
	body := fmt.Sprintf(`# ADR-%04d — Operator manual amendment proposal

- **Status**: proposed
- **Source**: operator (/v1/doctrine/propose; Plan 8 K-4)
- **Rule path**: %s
- **New value**: %s
- **Category**: %s
- **Cooldown override**: %v
- **Proposed at**: %s

## Justification

%s

## Lifecycle

This proposal entered the doctrine amendment lifecycle (proposed → applied
| denied → reverted) via 'zen doctrine propose'. Use 'zen doctrine ack
ADR-%04d' to accept or 'zen doctrine deny ADR-%04d --reason ...' to reject.
On accept, Plan 5 Applier (extended by Plan 8 Phase H ApplyWithValidation
hook) validates tighten-only direction (inv-zen-140) before mutating the
on-disk doctrine TOML.

inv-zen-103: this ADR ID is allocated from Plan 8's reserved range
(0050-0059) for operator-initiated manual proposals. Telemetry-driven
proposals from Plan 5's TelemetrySubscriber use range 0020-0029.
`,
		allocated, req.RulePath, req.NewValue, req.Category, req.CooldownOverride,
		now.Format(time.RFC3339), strings.TrimSpace(req.Justification),
		allocated, allocated)
	if err := os.WriteFile(absPath, []byte(body), 0o644); err != nil {
		return client.DoctrineProposeResponse{}, fmt.Errorf("invalid_rule_path: write %s: %w", absPath, err)
	}

	ctx := context.WithoutCancel(context.Background())
	ev := eventlog.Event{
		Type:      eventlog.EvtDoctrineAmendmentSuppressed,
		SessionID: "operator-action",
		ProjectID: "doctrine-amendment",
		Timestamp: now,
		Payload: map[string]any{
			"adr_id":            fmt.Sprintf("ADR-%04d", allocated),
			"decision":          "propose",
			"rule_path":         req.RulePath,
			"new_value":         req.NewValue,
			"category":          req.Category,
			"cooldown_override": req.CooldownOverride,
			"justification":     req.Justification,
		},
	}
	if _, err := s.eventLog.Append(ctx, ev); err != nil {

		s.adaptersClean.Store(false)
	}

	return client.DoctrineProposeResponse{
		ID:              fmt.Sprintf("ADR-%04d", allocated),
		Status:          "proposed",
		RulePath:        req.RulePath,
		NewValue:        req.NewValue,
		Category:        req.Category,
		ProposedAt:      now.Unix(),
		Proposer:        "operator",
		AdrMarkdownPath: filepath.Join("docs", "decisions", "proposed", filename),
	}, nil
}

func adrIDFromFilename(name string) string {
	m := adrProposalRE.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	return "ADR-" + m[1]
}

func adrTitleFromBody(body, fallback string) string {
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(t, "# "))
		}
	}
	if m := adrProposalRE.FindStringSubmatch(fallback); m != nil {
		return strings.ReplaceAll(m[2], "-", " ")
	}
	return fallback
}

func (s *Plan5OrchestratorService) SafetynetStatus() (client.SafetynetStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var status client.SafetynetStatus
	if s.prev != nil {
		info, err := s.prev.Show(ctx)
		if err == nil {
			status.PrevBinaryInstalled = info.Installed
			status.PrevBinaryPath = info.Path
			status.PrevBinaryVersion = info.Version
		} else if errors.Is(err, safetynet.ErrPrevNotInstalled) {
			status.PrevBinaryPath = info.Path
		}
	}

	since7d := s.cfg.Clock.Now().Add(-7 * 24 * time.Hour)
	if rate, err := s.passRate(ctx, "substrate", since7d); err == nil {
		status.SubstratePassRate7d = rate
	}
	if rate, err := s.passRate(ctx, "operator", since7d); err == nil {
		status.OperatorPassRate7d = rate
	}

	since24h := s.cfg.Clock.Now().Add(-24 * time.Hour).UnixNano()
	count, _ := s.countEventsByType(ctx, eventlog.EvtSubstrateDriftDetected, since24h)
	status.DriftIncidents24h = count

	lastTS, lastClean := s.lastDivergenceFromEvents(ctx)
	status.LastDivergenceAt = lastTS
	status.LastDivergenceClean = lastClean

	return status, nil
}

func (s *Plan5OrchestratorService) passRate(ctx context.Context, author string, since time.Time) (float64, error) {
	rows, err := s.adapter.Recent(ctx, author, since)
	if err != nil {
		s.adaptersClean.Store(false)
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	var sum float64
	for _, r := range rows {
		sum += r.TestPassRate
	}
	return sum / float64(len(rows)), nil
}

func (s *Plan5OrchestratorService) countEventsByType(ctx context.Context, et eventlog.EventType, sinceNs int64) (int, error) {

	return s.adapter.CountEventsByType(ctx, et.String(), sinceNs)
}

func (s *Plan5OrchestratorService) lastDivergenceFromEvents(ctx context.Context) (int64, bool) {
	ts, err := s.adapter.LastEventByTypeUnix(ctx, eventlog.EvtConfigDivergenceDetected.String())
	if err != nil || ts == 0 {
		return 0, true
	}

	return ts, false
}

func (s *Plan5OrchestratorService) SafetynetPrevInstall() (map[string]string, error) {
	if s.prev == nil {
		return nil, errors.New("safetynet prev install: PrevBinaryPath not configured")
	}
	manifestPath := ""
	if s.repoRoot != "" {
		manifestPath = filepath.Join(s.repoRoot, "bin", "zen-prev-manifest.json")
	}
	if manifestPath == "" {
		return nil, errors.New("safetynet prev install: RepoRoot not configured")
	}
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("safetynet prev install: read manifest: %w", err)
	}
	var manifest safetynet.Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("safetynet prev install: parse manifest: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.prev.Install(ctx, manifest); err != nil {
		return nil, err
	}
	return map[string]string{
		"version": manifest.Version,
		"path":    s.cfg.PrevBinaryPath,
		"sha256":  manifest.Sha256,
	}, nil
}

func (s *Plan5OrchestratorService) SafetynetPrevShow() (map[string]string, error) {
	if s.prev == nil {
		return map[string]string{"installed": "false"}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info, err := s.prev.Show(ctx)
	if err != nil {
		if errors.Is(err, safetynet.ErrPrevNotInstalled) {
			return map[string]string{
				"installed": "false",
				"path":      info.Path,
			}, nil
		}
		return nil, err
	}
	return map[string]string{
		"installed": "true",
		"path":      info.Path,
		"version":   info.Version,
		"sha256":    info.Sha256,
	}, nil
}

func (s *Plan5OrchestratorService) SafetynetPrevExec(argv []string) (map[string]any, error) {
	if s.prev == nil {
		return nil, errors.New("safetynet prev exec: PrevBinaryPath not configured")
	}
	if len(argv) == 0 {
		return nil, errors.New("safetynet prev exec: argv must not be empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := s.prev.Exec(ctx, argv)
	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			return nil, err
		}
	}
	return map[string]any{
		"exit_code": exitCode,
		"stdout":    string(out),
	}, nil
}

func (s *Plan5OrchestratorService) SafetynetDivergenceRun() (client.DivergenceReport, error) {
	if s.repoRoot == "" {
		return client.DivergenceReport{}, errors.New("safetynet divergence run: RepoRoot not configured")
	}
	pathA := filepath.Join(s.repoRoot, "zenswarm.json")
	if _, err := os.Stat(pathA); os.IsNotExist(err) {

		return client.DivergenceReport{}, fmt.Errorf("safetynet divergence run: %s not found", pathA)
	}
	pathB := filepath.Join(s.repoRoot, ".zen-swarm", "captured.json")
	if _, err := os.Stat(pathB); err != nil {
		return client.DivergenceReport{}, fmt.Errorf("safetynet divergence run: substrate snapshot %s missing: %w", pathB, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rep, err := s.divergence.Compare(ctx, pathA, pathB)
	if err != nil {
		return client.DivergenceReport{}, err
	}
	now := s.cfg.Clock.Now().Unix()
	diffs := make([]string, 0, len(rep.OnlyInA)+len(rep.OnlyInB)+len(rep.Changed))
	for _, k := range rep.OnlyInA {
		diffs = append(diffs, "only_in_operator: "+k)
	}
	for _, k := range rep.OnlyInB {
		diffs = append(diffs, "only_in_substrate: "+k)
	}
	for _, c := range rep.Changed {
		diffs = append(diffs, fmt.Sprintf("changed: %s (operator=%v, substrate=%v)", c.Key, c.A, c.B))
	}
	return client.DivergenceReport{
		RanAt:       now,
		Differences: diffs,
		Clean:       rep.Equal,
	}, nil
}

func (s *Plan5OrchestratorService) SafetynetDivergenceHistory(since string) ([]client.DivergenceReport, error) {
	dur, err := parseSinceDuration(since, 7*24*time.Hour)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.adapter.QueryEventsByType(ctx, eventlog.EvtConfigDivergenceDetected.String(),
		s.cfg.Clock.Now().Add(-dur).Unix())
	if err != nil {
		s.adaptersClean.Store(false)
		return nil, err
	}
	out := make([]client.DivergenceReport, 0, len(rows))
	for _, r := range rows {
		out = append(out, client.DivergenceReport{
			RanAt:       r.EmittedAtUnix,
			Differences: divergenceDiffsFromPayload(r.PayloadJSON),
			Clean:       false,
		})
	}
	return out, nil
}

func divergenceDiffsFromPayload(payload []byte) []string {
	var p struct {
		OnlyInA []string `json:"only_in_a"`
		OnlyInB []string `json:"only_in_b"`
		Changed []struct {
			Key string `json:"Key"`
		} `json:"changed"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil
	}
	out := make([]string, 0, len(p.OnlyInA)+len(p.OnlyInB)+len(p.Changed))
	for _, k := range p.OnlyInA {
		out = append(out, "only_in_operator: "+k)
	}
	for _, k := range p.OnlyInB {
		out = append(out, "only_in_substrate: "+k)
	}
	for _, c := range p.Changed {
		out = append(out, "changed: "+c.Key)
	}
	return out
}

func (s *Plan5OrchestratorService) SafetynetRegressionQuery(author, since string) ([]client.RegressionMetric, error) {
	dur, err := parseSinceDuration(since, 7*24*time.Hour)
	if err != nil {
		return nil, err
	}
	if author == "" {

		var all []client.RegressionMetric
		for _, a := range []string{"substrate", "operator", "manual"} {
			rows, err := s.regressionRows(a, dur)
			if err != nil {
				return nil, err
			}
			all = append(all, rows...)
		}
		sort.Slice(all, func(i, j int) bool { return all[i].RecordedAt > all[j].RecordedAt })
		return all, nil
	}
	return s.regressionRows(author, dur)
}

func (s *Plan5OrchestratorService) regressionRows(author string, dur time.Duration) ([]client.RegressionMetric, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.regression.Query(ctx, author, s.cfg.Clock.Now().Add(-dur))
	if err != nil {
		s.adaptersClean.Store(false)
		return nil, err
	}
	out := make([]client.RegressionMetric, 0, len(rows))
	for _, r := range rows {
		out = append(out, client.RegressionMetric{
			CommitSHA:    r.CommitSHA,
			AuthoredBy:   r.AuthoredBy,
			TestPassRate: r.TestPassRate,
			TestTotal:    r.TestTotal,
			TestPassed:   r.TestPassed,
			RecordedAt:   r.RecordedAt,
		})
	}
	return out, nil
}

func (s *Plan5OrchestratorService) SafetynetDriftRun() ([]client.DriftFinding, error) {
	if s.repoRoot == "" {
		return nil, errors.New("safetynet drift run: RepoRoot not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rep, err := s.drift.Validate(ctx, 50)
	if err != nil {
		return nil, err
	}
	now := s.cfg.Clock.Now().Unix()
	out := make([]client.DriftFinding, 0, len(rep.Findings))
	for _, f := range rep.Findings {
		out = append(out, client.DriftFinding{
			CommitSHA:   f.CommitSHA,
			RanAt:       now,
			Rule:        f.Rule,
			Description: f.Detail,
		})
	}
	return out, nil
}

func (s *Plan5OrchestratorService) SafetynetDriftHistory(since string) ([]client.DriftFinding, error) {
	dur, err := parseSinceDuration(since, 7*24*time.Hour)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.adapter.QueryEventsByType(ctx, eventlog.EvtSubstrateDriftDetected.String(),
		s.cfg.Clock.Now().Add(-dur).Unix())
	if err != nil {
		s.adaptersClean.Store(false)
		return nil, err
	}
	out := make([]client.DriftFinding, 0, len(rows))
	for _, r := range rows {
		var p struct {
			CommitSHA string `json:"commit_sha"`
			Rule      string `json:"rule"`
			Detail    string `json:"detail"`
		}
		_ = json.Unmarshal(r.PayloadJSON, &p)
		out = append(out, client.DriftFinding{
			CommitSHA:   p.CommitSHA,
			RanAt:       r.EmittedAtUnix,
			Rule:        p.Rule,
			Description: p.Detail,
		})
	}
	return out, nil
}

func parseSinceDuration(s string, fallback time.Duration) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback, nil
	}

	if strings.HasSuffix(s, "d") {
		nstr := strings.TrimSuffix(s, "d")
		n, err := strconv.Atoi(nstr)
		if err != nil {
			return 0, fmt.Errorf("invalid since duration %q: %w", s, err)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid since duration %q: %w", s, err)
	}
	return d, nil
}

func (s *Plan5OrchestratorService) HealthEventLogWritable() (bool, int, error) {
	if s.healthSampler == nil {
		return false, 0, nil
	}
	d, _ := s.healthSampler.Current().Get("event_log_writable")
	return d.Up, 0, nil
}

func (s *Plan5OrchestratorService) SampleEventLogWritable(ctx context.Context) (bool, int, error) {
	probeSession := fmt.Sprintf("daemon-health-probe-%d", s.cfg.Clock.Now().UnixNano())
	if _, err := s.adapter.EmitRaw(ctx, "daemon-health", probeSession,
		int(eventlog.EvtOrchestratorStarted), []byte(`{"probe":true}`),
		s.cfg.Clock.Now().UnixNano()); err != nil {
		s.adaptersClean.Store(false)
		return false, int(eventlog.CorruptPayloadCount()), err
	}
	return true, int(eventlog.CorruptPayloadCount()), nil
}

func (s *Plan5OrchestratorService) HealthResearchMCPUp() (bool, error) {
	if s.healthSampler == nil {
		return false, nil
	}
	d, _ := s.healthSampler.Current().Get("research_mcp_up")
	return d.Up, nil
}

func (s *Plan5OrchestratorService) HealthCaronteUp() (bool, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := s.checkEngine.RunCheck(ctx, autonomy.RunInput{
		Doctrine:    s.cfg.Doctrine,
		ProjectRoot: s.repoRoot,
	})
	if err != nil {
		return false, 0, err
	}
	up := false
	hours := 0
	for _, r := range out.Results {
		if r.Name == "caronte_engine_up" {
			up = r.Status == autonomy.CheckPass
		}
		if r.Name == "caronte_index_currency" {

			hours = extractHoursFromReason(r.Reason)
		}
	}
	return up, hours, nil
}

var hoursDigitRE = regexp.MustCompile(`(\d+)h`)

func extractHoursFromReason(reason string) int {
	m := hoursDigitRE.FindStringSubmatch(reason)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func (s *Plan5OrchestratorService) HealthAdaptersClean() (bool, error) {
	return s.adaptersClean.Load(), nil
}

func (s *Plan5OrchestratorService) HealthLastSessionClean() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.adapter.QueryEventsByType(ctx, eventlog.EvtOrchestratorStopped.String(), 0)
	if err != nil {
		s.adaptersClean.Store(false)
		return false, err
	}
	if len(rows) == 0 {

		return true, nil
	}

	last := rows[len(rows)-1]
	var p struct {
		Outcome string `json:"outcome"`
	}
	_ = json.Unmarshal(last.PayloadJSON, &p)
	return p.Outcome == "success" || p.Outcome == "ok" || p.Outcome == "", nil
}

// safetynetEmitterShim adapts safetynet.Emitter (string-typed) to
// eventlog.Log.Append (typed event slot). Failures are best-effort —
// safetynet's Emit-error contract MUST NOT block the inspection result.
type safetynetEmitterShim struct {
	log      *eventlog.Log
	doctrine string
}

func (e *safetynetEmitterShim) Emit(ctx context.Context, ev safetynet.Event) error {
	t, ok := safetynetEventTypeMap[ev.Type]
	if !ok {
		return fmt.Errorf("unknown safetynet event type: %v", ev.Type)
	}
	auditCtx := context.WithoutCancel(ctx)
	_, err := e.log.Append(auditCtx, eventlog.Event{
		Type:      t,
		SessionID: "safetynet",
		ProjectID: e.doctrine,
		Timestamp: time.Now().UTC(),
		Payload:   ev.Payload,
	})
	return err
}

var safetynetEventTypeMap = map[safetynet.EventType]eventlog.EventType{
	safetynet.EventSubstrateDriftDetected:   eventlog.EvtSubstrateDriftDetected,
	safetynet.EventConfigDivergenceDetected: eventlog.EvtConfigDivergenceDetected,
	safetynet.EventRegressionBySelfAlarm:    eventlog.EvtRegressionBySelfAlarm,
	safetynet.EventSafetynetPrevMissing:     eventlog.EvtSafetynetPrevMissing,
}

type osFileStat struct{}

func (osFileStat) ModTime(path string) (time.Time, error) {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return st.ModTime(), nil
}

type osFileReader struct{}

func (osFileReader) ReadFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	const maxBytes = 1 << 20
	br := bufio.NewReader(io.LimitReader(f, maxBytes))
	return io.ReadAll(br)
}

const execMaxConcurrent = 4

var execSem = semaphore.NewWeighted(execMaxConcurrent)

type osExecer struct{}

func (osExecer) Run(ctx context.Context, name string, args ...string) (string, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := execSem.Acquire(ctx, 1); err != nil {
		return "", 0, err
	}
	defer execSem.Release(1)
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {

		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second

	stop := context.AfterFunc(ctx, func() { _ = cmd.Cancel() })
	defer stop()
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
			err = nil
		}
	}
	return string(out), exitCode, err
}

type gitCommitSource struct {
	repoRoot string
}

func (g *gitCommitSource) Recent(ctx context.Context, n int) ([]safetynet.Commit, error) {
	if g.repoRoot == "" || n <= 0 {
		return nil, nil
	}

	const unitSep = "\x1f"
	const recSep = "\x1e"
	cmd := exec.CommandContext(ctx, "git", "-C", g.repoRoot, "log",
		"-n", strconv.Itoa(n),
		"--pretty=format:%H"+unitSep+"%s"+unitSep+"%b"+recSep,
		"--no-merges")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git log: %w: %s", err, strings.TrimSpace(string(out)))
	}
	rows := strings.Split(strings.TrimSpace(string(out)), recSep)
	commits := make([]safetynet.Commit, 0, len(rows))
	for _, r := range rows {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		fields := strings.SplitN(r, unitSep, 3)
		if len(fields) < 3 {
			continue
		}
		commits = append(commits, safetynet.Commit{
			SHA:     strings.TrimSpace(fields[0]),
			Subject: strings.TrimSpace(fields[1]),
			Body:    strings.TrimSpace(fields[2]),
		})
	}
	return commits, nil
}

var (
	_ safetynet.Emitter      = (*safetynetEmitterShim)(nil)
	_ safetynet.CommitSource = (*gitCommitSource)(nil)
)

var _ = json.Marshal
