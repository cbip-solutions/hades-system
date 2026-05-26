// SPDX-License-Identifier: MIT
package manifest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const FreshnessThreshold = 7 * 24 * time.Hour

const CompensationWindow = 7 * 24 * time.Hour

type ChainAnchoredEvent struct {
	Type      string
	Timestamp time.Time
}

type DiffReport struct {
	AutoDriftPaths []string

	FreshnessExceeded bool

	Recommendations []string

	StoredAge time.Duration
}

func (r DiffReport) IsFailure() bool {
	return len(r.AutoDriftPaths) > 0 || r.FreshnessExceeded
}

type Differ struct {
	schema      *Schema
	regenerator *Regenerator
}

func NewDiffer(schema *Schema, r *Regenerator) *Differ {
	if schema == nil {
		panic("manifest.NewDiffer: nil schema")
	}
	if r == nil {
		panic("manifest.NewDiffer: nil regenerator")
	}
	return &Differ{schema: schema, regenerator: r}
}

func (d *Differ) Verify(
	ctx context.Context,
	fresh Manifest,
	manifestPath string,
	now time.Time,
	recentEvents []ChainAnchoredEvent,
) (DiffReport, error) {
	report := DiffReport{}

	merged, err := d.regenerator.Regenerate(ctx, fresh, manifestPath)
	if err != nil {
		return report, fmt.Errorf("differ regenerate: %w", err)
	}

	storedBody, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {

			report.AutoDriftPaths = []string{"<file-missing>"}
			report.Recommendations = append(report.Recommendations,
				"docs/system-state.toml is missing; run `zen state regenerate` to create it.")
			return report, nil
		}
		return report, fmt.Errorf("differ read manifest: %w", err)
	}

	var stored Manifest
	if _, decErr := toml.NewDecoder(bytes.NewReader(storedBody)).Decode(&stored); decErr != nil {

		report.AutoDriftPaths = []string{"<malformed-toml>"}
		report.Recommendations = append(report.Recommendations,
			"docs/system-state.toml fails TOML decode; restore from VCS or run `zen state regenerate`.")
		return report, nil
	}

	autoSources, err := d.schema.DiscoverAutoSources()
	if err != nil {
		return report, fmt.Errorf("differ discover auto sources: %w", err)
	}
	for _, src := range autoSources {
		if !equalManifestPath(stored, merged, src.Path) {
			report.AutoDriftPaths = append(report.AutoDriftPaths, src.Path)
		}
	}
	sort.Strings(report.AutoDriftPaths)
	if len(report.AutoDriftPaths) > 0 {
		report.Recommendations = append(report.Recommendations,
			fmt.Sprintf("auto-derive drift detected on paths: %s; run `zen state regenerate`.",
				strings.Join(report.AutoDriftPaths, ", ")))
	}

	report.StoredAge = now.Sub(stored.Provenance.LastRegenerate)
	if report.StoredAge > FreshnessThreshold {
		if !hasCompensatingEvent(recentEvents, now) {
			report.FreshnessExceeded = true
			report.Recommendations = append(report.Recommendations,
				fmt.Sprintf(
					"docs/system-state.toml is %s old (threshold %s); run `zen state regenerate` or pin a manual field with --reason to extend the freshness lease.",
					report.StoredAge.Truncate(time.Hour),
					FreshnessThreshold,
				))
		}
	}

	return report, nil
}

func equalManifestPath(a, b Manifest, path string) bool {
	va := manifestValueAtPath(a, path)
	vb := manifestValueAtPath(b, path)
	if !va.IsValid() && !vb.IsValid() {
		return true
	}
	if !va.IsValid() || !vb.IsValid() {
		return false
	}
	return reflect.DeepEqual(va.Interface(), vb.Interface())
}

func manifestValueAtPath(m Manifest, path string) reflect.Value {
	dotIdx := strings.IndexByte(path, '.')
	if dotIdx < 0 {

		return reflect.Value{}
	}
	sectionName := path[:dotIdx]
	leafName := path[dotIdx+1:]

	section := sectionByTOMLName(reflect.ValueOf(m), sectionName)
	if !section.IsValid() {
		return reflect.Value{}
	}
	return fieldByTOMLName(section, leafName)
}

func hasCompensatingEvent(events []ChainAnchoredEvent, now time.Time) bool {
	for _, e := range events {
		if e.Type != "state.manual_field_changed" {
			continue
		}
		if now.Sub(e.Timestamp) <= CompensationWindow {
			return true
		}
	}
	return false
}
