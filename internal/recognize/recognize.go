// SPDX-License-Identifier: MIT
// Package recognize — orchestrator.
//
// Implements the three-tier cascade per design contract=B:
// - Tier 1: manifest detection (highest confidence)
// - Tier 2: framework config detection (deps-disambiguated)
// - Tier 3: glob byte-ranking (linguist-filtered)
//
// Tier 1 short-circuits Tier 3 when a single ecosystem has confidence ≥ 0.8.
// Monorepo walk-UP + maturity probe always run when RootAbsPath is provided.
// Audit emit via the AuditEmitter seam (daemon glue wires concrete impl).
package recognize

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/chain"
	"github.com/cbip-solutions/hades-system/internal/recognize/config"
	"github.com/cbip-solutions/hades-system/internal/recognize/glob"
	"github.com/cbip-solutions/hades-system/internal/recognize/manifest"
	"github.com/cbip-solutions/hades-system/internal/recognize/maturity"
	"github.com/cbip-solutions/hades-system/internal/recognize/monorepo"
)

const shortCircuitThreshold = 0.8

const polyglotFloor = 0.80

const secondaryFloor = 0.50

const ambiguityWindow = 0.10

type threeTierRecognizer struct {
	opts    Options
	emitter AuditEmitter
}

func New(opts Options) Recognizer {
	r := &threeTierRecognizer{opts: opts}
	if !opts.NoAudit && opts.ChainStore != nil {
		r.emitter = &chainEmitter{store: opts.ChainStore, clock: systemClock{}}
	}
	return r
}

func newWithEmitter(opts Options, e AuditEmitter) *threeTierRecognizer {
	return &threeTierRecognizer{opts: opts, emitter: e}
}

type chainEmitter struct {
	store ChainStore
	clock clock
}

type clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (e *chainEmitter) Emit(ctx context.Context, eventType string, payload map[string]any) error {
	prevHash, err := e.store.GetChainTip(ctx)
	if err != nil {
		return fmt.Errorf("recognize chain emit: GetChainTip: %w", err)
	}

	now := e.clock.Now().UTC()
	timestampNano := now.UnixNano()
	timestampSeconds := now.Unix()

	eventID, err := generateEventID(timestampNano)
	if err != nil {

		return fmt.Errorf("recognize chain emit: generateEventID: %w", err)
	}

	payloadBytes, err := marshalCanonical(payload)
	if err != nil {

		return fmt.Errorf("recognize chain emit: marshal payload: %w", err)
	}

	partitionID := chain.PartitionID(timestampSeconds)
	recordHash, err := chain.Compute(prevHash, eventType, payloadBytes, timestampSeconds)
	if err != nil {
		return fmt.Errorf("recognize chain emit: chain.Compute %s: %w", eventID, err)
	}

	if err := e.store.UpdateChainColumns(ctx, eventID, prevHash, eventType, payloadBytes, timestampSeconds, recordHash, partitionID); err != nil {
		return fmt.Errorf("recognize chain emit: UpdateChainColumns %s: %w", eventID, err)
	}

	leafID, err := e.store.AppendTesseraLeaf(ctx, TesseraLeafInput{
		EventID:    eventID,
		EventType:  eventType,
		ProjectID:  "",
		Partition:  partitionID,
		Payload:    payloadBytes,
		RecordHash: recordHash,
	})
	if err != nil {
		return fmt.Errorf("recognize chain emit: AppendTesseraLeaf %s: %w", eventID, err)
	}

	if err := e.store.UpdateTesseraLeafID(ctx, eventID, leafID); err != nil {
		return fmt.Errorf("recognize chain emit: UpdateTesseraLeafID %s: %w", eventID, err)
	}
	return nil
}

func generateEventID(timestampNano int64) (string, error) {
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {

		return "", fmt.Errorf("rand.Read: %w", err)
	}
	return fmt.Sprintf("evt-%d-%s", timestampNano, hex.EncodeToString(rnd[:])), nil
}

func marshalCanonical(m map[string]any) ([]byte, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(m[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (r *threeTierRecognizer) Recognize(ctx context.Context, root fs.FS) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	res := Result{
		SchemaVersion: SchemaVersion,
		Rationale:     []string{},
		Languages:     []LanguageEvidence{},
		Ecosystems:    []EcosystemEvidence{},
		Frameworks:    []FrameworkEvidence{},
	}

	ecosystems, manifestErr := manifest.Detect(root)
	if manifestErr != nil {

		res.Rationale = append(res.Rationale, fmt.Sprintf("Tier 1 partial: %v", manifestErr))
	}
	for _, ev := range ecosystems {
		res.Ecosystems = append(res.Ecosystems, EcosystemEvidence{
			Ecosystem:  ev.Ecosystem,
			Evidence:   ev.Path + " parsed",
			Confidence: ev.Confidence,
		})
	}

	for _, ev := range ecosystems {
		for name, version := range ev.Dependencies {
			if res.ManifestDeps == nil {
				res.ManifestDeps = map[string]string{}
			}
			res.ManifestDeps[name] = version
		}
	}

	var tier1Primary *manifest.Evidence
	distinctEcos := map[string]bool{}
	for i := range ecosystems {
		ev := ecosystems[i]
		distinctEcos[ev.Ecosystem] = true
		if ev.Confidence >= shortCircuitThreshold && tier1Primary == nil {
			tier1Primary = &ev
		}
	}
	tier1ShortCircuit := tier1Primary != nil && len(distinctEcos) == 1

	if tier1ShortCircuit {
		res.Rationale = append(res.Rationale, fmt.Sprintf(
			"Tier 1 short-circuit: %s confidence %.2f single-ecosystem",
			tier1Primary.Ecosystem, tier1Primary.Confidence,
		))
		res.PrimaryLanguage = tier1Primary.Language
		res.PrimaryConfidence = tier1Primary.Confidence

		res.Languages = append(res.Languages, LanguageEvidence{
			Language:   tier1Primary.Language,
			Bytes:      0,
			Files:      0,
			Confidence: tier1Primary.Confidence,
		})
	}

	fwcs, fwErr := config.Detect(root)
	if fwErr != nil {
		res.Rationale = append(res.Rationale, fmt.Sprintf("Tier 2 partial: %v", fwErr))
	}
	for _, fw := range fwcs {
		res.Frameworks = append(res.Frameworks, FrameworkEvidence{
			Framework:  fw.Framework,
			ConfigPath: fw.ConfigPath,
			Confidence: fw.Confidence,
		})
	}

	configFilesSeen := map[string]bool{}
	for _, fw := range fwcs {
		if fw.ConfigPath == "" || configFilesSeen[fw.ConfigPath] {
			continue
		}
		configFilesSeen[fw.ConfigPath] = true
		res.ConfigFiles = append(res.ConfigFiles, fw.ConfigPath)
	}

	for _, candidate := range smartDefaultConfigCandidates {
		if _, err := fs.Stat(root, candidate); err == nil && !configFilesSeen[candidate] {
			configFilesSeen[candidate] = true
			res.ConfigFiles = append(res.ConfigFiles, candidate)
		}
	}

	if !tier1ShortCircuit {
		stats, globErr := glob.Walk(ctx, root, glob.WalkOptions{
			MaxBytesPerFile: r.opts.MaxBytesPerFile,
			Workers:         r.opts.Workers,
		})
		if globErr != nil {
			if errors.Is(globErr, context.Canceled) || errors.Is(globErr, context.DeadlineExceeded) {
				return Result{}, globErr
			}
			res.Rationale = append(res.Rationale, fmt.Sprintf("Tier 3 partial: %v", globErr))
		}

		var total int64
		for _, s := range stats {
			total += s.Bytes
		}
		for _, s := range stats {
			confidence := 0.0
			if total > 0 {
				confidence = float64(s.Bytes) / float64(total)
			}
			res.Languages = append(res.Languages, LanguageEvidence{
				Language:   s.Language,
				Bytes:      s.Bytes,
				Files:      s.Files,
				Confidence: confidence,
			})
		}

		if len(res.Languages) > 0 {
			primary := res.Languages[0]
			res.PrimaryLanguage = primary.Language
			res.PrimaryConfidence = primary.Confidence
			switch {
			case primary.Confidence >= polyglotFloor:
				res.Rationale = append(res.Rationale, fmt.Sprintf(
					"Tier 3: %s dominant (%.0f%% bytes)", primary.Language, primary.Confidence*100,
				))
			case primary.Confidence >= secondaryFloor:
				res.Rationale = append(res.Rationale, fmt.Sprintf(
					"Tier 3: %s primary (%.0f%%) + secondaries", primary.Language, primary.Confidence*100,
				))
			}
			if len(res.Languages) >= 2 {
				gap := res.Languages[0].Confidence - res.Languages[1].Confidence
				if gap < ambiguityWindow {
					res.Ambiguous = true
					res.Rationale = append(res.Rationale, fmt.Sprintf(
						"Tier 3 ambiguous: top-2 within %.0f%% (%s %.0f%% vs %s %.0f%%)",
						ambiguityWindow*100,
						res.Languages[0].Language, res.Languages[0].Confidence*100,
						res.Languages[1].Language, res.Languages[1].Confidence*100,
					))
				}
			}
		}
	}

	if r.opts.RootAbsPath != "" {
		if ws, err := monorepo.WalkUp(r.opts.RootAbsPath); err == nil && ws.Tool != "" {
			res.Monorepo = &MonorepoInfo{
				Tool:       ws.Tool,
				Root:       ws.Root,
				ConfigPath: ws.ConfigPath,
			}
			res.Rationale = append(res.Rationale, "Monorepo detected: "+ws.Tool+" at "+ws.Root)
		}
	}

	if r.opts.RootAbsPath != "" {
		m, _ := maturity.Probe(ctx, r.opts.RootAbsPath)
		res.Maturity = MaturityInfo{
			CommitCount:       m.CommitCount,
			LastCommitISO8601: m.LastCommitISO8601,
			HasCI:             m.HasCI,
			CIPlatform:        m.CIPlatform,
		}
	} else {

		res.Maturity = MaturityInfo{CommitCount: -1}
	}

	for _, envFile := range envFileCandidates {
		names, err := scanEnvVarNames(root, envFile)
		if err != nil {

			continue
		}
		for _, name := range names {
			if res.EnvVars == nil {
				res.EnvVars = map[string]string{}
			}
			res.EnvVars[name] = ""
		}
	}

	res.Doctrine = inferDoctrine(root)

	if !r.opts.NoAudit {
		if r.emitter != nil {
			payload := map[string]any{
				"schemaVersion":     SchemaVersion,
				"primaryLanguage":   res.PrimaryLanguage,
				"primaryConfidence": res.PrimaryConfidence,
				"ecosystemsCount":   len(res.Ecosystems),
				"frameworksCount":   len(res.Frameworks),
				"ambiguous":         res.Ambiguous,
				"tier1ShortCircuit": tier1ShortCircuit,
			}
			if err := r.emitter.Emit(ctx, "evt.recognize.run", payload); err != nil {

				res.Rationale = append(res.Rationale, fmt.Sprintf("audit emit partial: %v", err))
			}
		} else {

			res.Rationale = append(res.Rationale, "audit emit skipped: no chain store wired")
		}
	}

	return res, nil
}

var smartDefaultConfigCandidates = []string{

	"sentry.client.config.ts",
	"sentry.client.config.js",
	"sentry.server.config.ts",
	"sentry.server.config.js",
	"sentry.edge.config.ts",
	"sentry.edge.config.js",
	"sentry.config.ts",
	"sentry.config.js",
	"sentry.py",
	"sentry.js",
	"sentry.ts",

	".linear.yml",
	".linear.yaml",
}

var envFileCandidates = []string{
	".env",
	".env.example",
	".env.local",
	".env.development",
	".env.production",
	".env.sample",
}

const envVarMaxBytes int64 = 50 * 1024

func scanEnvVarNames(root fs.FS, path string) ([]string, error) {
	f, err := root.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, envVarMaxBytes))
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(buf))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		if name == "" || seen[name] || !isValidEnvVarName(name) {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

func isValidEnvVarName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c == '_':
		case (c >= '0' && c <= '9') && i > 0:
		default:
			return false
		}
	}
	return true
}

const doctrineSignalCap int64 = 50 * 1024

func inferDoctrine(root fs.FS) string {

	if buf, ok := readCappedFile(root, ".hades/doctrine.toml", doctrineSignalCap); ok {
		if name := extractTOMLName(buf); name != "" {
			return name
		}
	}

	if buf, ok := readCappedFile(root, "project instructions", doctrineSignalCap); ok {
		return findCanonicalDoctrineName(buf)
	}
	return ""
}

func readCappedFile(root fs.FS, path string, capBytes int64) ([]byte, bool) {
	f, err := root.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, capBytes))
	if err != nil {
		return nil, false
	}
	return buf, true
}

func extractTOMLName(buf []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(buf))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "name") {
			continue
		}

		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		value := strings.TrimSpace(line[idx+1:])

		if hashIdx := strings.Index(value, "#"); hashIdx >= 0 {
			value = strings.TrimSpace(value[:hashIdx])
		}

		value = strings.Trim(value, `"'`)
		if isCanonicalDoctrineName(value) {
			return value
		}
		return ""
	}
	return ""
}

func findCanonicalDoctrineName(buf []byte) string {
	s := string(buf)
	canonical := []string{"max-scope", "capa-firewall", "default"}
	for _, name := range canonical {

		if strings.Contains(s, "Doctrine: "+name) {
			return name
		}

		needle := "doctrine: " + name
		if i := strings.Index(s, needle); i >= 0 {
			j := i + len(needle)
			if j == len(s) || isLineBoundary(s[j]) {
				return name
			}
		}

		if strings.Contains(s, `doctrine: "`+name+`"`) ||
			strings.Contains(s, `doctrine = "`+name+`"`) {
			return name
		}
	}
	return ""
}

func isLineBoundary(c byte) bool {
	return c == '\n' || c == '\r' || c == '\t' || c == ' '
}

func isCanonicalDoctrineName(s string) bool {
	switch s {
	case "max-scope", "default", "capa-firewall":
		return true
	}
	return false
}
