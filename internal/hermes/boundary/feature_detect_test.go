// SPDX-License-Identifier: MIT
package boundary_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/hermes/boundary"
)

func TestCapabilitiesForKnownVersions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		version boundary.HermesVersion
		want    boundary.Capabilities
	}{
		{
			name:    "v0.13.0 — substrate-pivot target; G5 inert per empirical grep",
			version: boundary.HermesV0_13_0,
			want:    boundary.Capabilities{},
		},
		{
			name:    "v0.13.2 — post-Plan-15 pinned; same shape as v0.13.0",
			version: boundary.HermesV0_13_2,
			want:    boundary.Capabilities{},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := boundary.CapabilitiesFor(tc.version)
			if got != tc.want {
				t.Errorf("CapabilitiesFor(%q) = %+v; want %+v", tc.version, got, tc.want)
			}
		})
	}
}

func TestCapabilitiesForUnknownVersion(t *testing.T) {
	t.Parallel()
	got := boundary.CapabilitiesFor("99.99.99")
	if got != (boundary.Capabilities{}) {
		t.Errorf("CapabilitiesFor(unknown) should be zero; got %+v", got)
	}
}

func TestIsKnownVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		version boundary.HermesVersion
		want    bool
	}{
		{boundary.HermesV0_13_0, true},
		{boundary.HermesV0_13_2, true},
		{"0.14.0", false},
		{"99.99.99", false},
		{"", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.version), func(t *testing.T) {
			t.Parallel()
			if got := boundary.IsKnownVersion(tc.version); got != tc.want {
				t.Errorf("IsKnownVersion(%q) = %v; want %v", tc.version, got, tc.want)
			}
		})
	}
}
