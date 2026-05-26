//go:build property

package ecosystem_test

import (
	"context"
	"strings"
	"testing"
	"testing/quick"

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func TestVersionDetectorDeterminism_Property(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	property := func(queryStr string, versionStr string, ecoIdx uint8) bool {
		eco := ecosystem.AllEcosystems[int(ecoIdx)%len(ecosystem.AllEcosystems)]
		req := ecosystem.QueryRequest{
			Query:     sanitizePropertyString(queryStr),
			Version:   sanitizeVersionString(versionStr),
			Ecosystem: eco,
		}

		v1, l1, err1 := vd.Detect(ctx, req)
		v2, l2, err2 := vd.Detect(ctx, req)

		if (err1 == nil) != (err2 == nil) {
			return false
		}
		if v1 != v2 {
			t.Logf("non-deterministic version: req=%+v v1=%q v2=%q", req, v1, v2)
			return false
		}
		if l1 != l2 {
			t.Logf("non-deterministic layer: req=%+v l1=%d l2=%d", req, l1, l2)
			return false
		}
		return true
	}

	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("inv-zen-192 violated: %v", err)
	}
}

func TestVersionDetectorLayerBounds_Property(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	property := func(queryStr string, versionStr string, ecoIdx uint8) bool {
		eco := ecosystem.AllEcosystems[int(ecoIdx)%len(ecosystem.AllEcosystems)]
		req := ecosystem.QueryRequest{
			Query:     sanitizePropertyString(queryStr),
			Version:   sanitizeVersionString(versionStr),
			Ecosystem: eco,
		}
		_, layer, err := vd.Detect(ctx, req)
		if err != nil {

			t.Logf("unexpected error: req=%+v err=%v", req, err)
			return false
		}
		return layer >= 1 && layer <= 5
	}

	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("inv-zen-192 layer bounds violated: %v", err)
	}
}

func TestVersionDetectorVersionNonEmpty_Property(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	property := func(queryStr string, ecoIdx uint8) bool {
		eco := ecosystem.AllEcosystems[int(ecoIdx)%len(ecosystem.AllEcosystems)]
		req := ecosystem.QueryRequest{
			Query:     sanitizePropertyString(queryStr),
			Ecosystem: eco,
		}
		version, _, err := vd.Detect(ctx, req)
		if err != nil {
			return false
		}

		return version != ""
	}

	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("inv-zen-192 non-empty version violated: %v", err)
	}
}

func TestVersionDetectorExplicitVersion_Property(t *testing.T) {
	vd := ecosystem.NewVersionDetector(ecosystem.VersionDetectorOptions{
		SkipLLMDetection: true,
	})
	ctx := context.Background()

	property := func(queryStr string, versionStr string, ecoIdx uint8) bool {
		v := sanitizeVersionString(versionStr)
		if v == "" {

			return true
		}
		eco := ecosystem.AllEcosystems[int(ecoIdx)%len(ecosystem.AllEcosystems)]
		req := ecosystem.QueryRequest{
			Query:     sanitizePropertyString(queryStr),
			Version:   v,
			Ecosystem: eco,
		}
		version, layer, err := vd.Detect(ctx, req)
		if err != nil {
			return false
		}

		return layer == 1 && version == v
	}

	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(property, cfg); err != nil {
		t.Errorf("inv-zen-192 Layer1 explicit-version property violated: %v", err)
	}
}

func sanitizePropertyString(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 32 && r != 127 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sanitizeVersionString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 0 || s[0] < '0' || s[0] > '9' {
		return ""
	}

	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
