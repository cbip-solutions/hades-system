//go:build property && cgo

package ecosystem_property_test

import (
	"context"
	"strings"
	"testing"
	"testing/quick"

	_ "github.com/mattn/go-sqlite3"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func sanitizeQuery(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r < 0x7f {
			b.WriteRune(r)
		}
		if b.Len() >= 64 {
			break
		}
	}
	return b.String()
}

func TestVersionDetector_Property_DeterminismOver1000Cases(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	prop := func(query, version string, ecoIdx uint8) bool {
		ecos := []ecosystem.Ecosystem{
			ecosystem.EcoGo, ecosystem.EcoPython, ecosystem.EcoTypeScript, ecosystem.EcoRust,
		}
		eco := ecos[int(ecoIdx)%len(ecos)]
		req := ecosystem.QueryRequest{
			Query:     sanitizeQuery(query),
			Version:   sanitizeQuery(version),
			Ecosystem: eco,
		}
		v1, l1, err1 := vd.Detect(ctx, req)
		v2, l2, err2 := vd.Detect(ctx, req)
		if (err1 == nil) != (err2 == nil) {
			return false
		}
		if v1 != v2 || l1 != l2 {
			t.Logf("non-deterministic detect: req=%+v -> (v1=%q, l1=%d) vs (v2=%q, l2=%d)",
				req, v1, l1, v2, l2)
			return false
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-192: VersionDetector non-deterministic: %v", err)
	}
}

func TestVersionDetector_Property_ExplicitVersionAlwaysLayer1(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	prop := func(query, version string, ecoIdx uint8) bool {
		v := sanitizeQuery(version)
		if v == "" {
			return true
		}
		ecos := []ecosystem.Ecosystem{
			ecosystem.EcoGo, ecosystem.EcoPython, ecosystem.EcoTypeScript, ecosystem.EcoRust,
		}
		eco := ecos[int(ecoIdx)%len(ecos)]
		req := ecosystem.QueryRequest{
			Query:     sanitizeQuery(query),
			Version:   v,
			Ecosystem: eco,
		}
		gotV, gotLayer, err := vd.Detect(ctx, req)
		if err != nil {
			return false
		}
		return gotV == v && gotLayer == 1
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-192: Layer 1 (explicit version) does not always win: %v", err)
	}
}

func TestVersionDetector_Property_NoSignalLandsOnLayer5(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	queries := []string{
		"explain the http package",
		"how to use channels",
		"what is dependency injection",
		"installation steps please",
		"",
	}
	for _, q := range queries {
		for _, eco := range []ecosystem.Ecosystem{
			ecosystem.EcoGo, ecosystem.EcoPython, ecosystem.EcoTypeScript, ecosystem.EcoRust,
		} {
			req := ecosystem.QueryRequest{Query: q, Ecosystem: eco}
			v, layer, err := vd.Detect(ctx, req)
			if err != nil {
				t.Errorf("eco=%s query=%q: unexpected err=%v", eco, q, err)
				continue
			}
			if layer != 5 {
				t.Errorf("eco=%s query=%q: layer=%d; want 5", eco, q, layer)
			}
			if v != "latest_stable" {
				t.Errorf("eco=%s query=%q: v=%q; want latest_stable", eco, q, v)
			}
		}
	}
}
