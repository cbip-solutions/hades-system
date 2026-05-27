// go:build chaos

package failpoints

import (
	"testing"
)

func TestDispatcherCancelMidFlightSiteRegistered(t *testing.T) {
	site := SiteByName("dispatcherCancelMidFlight")
	if site == nil {
		t.Fatal("Site dispatcherCancelMidFlight missing from catalogue")
	}
	if site.Package != "github.com/cbip-solutions/hades-system/internal/daemon/dispatcher" {
		t.Errorf("Package = %q", site.Package)
	}
}

func TestDispatcherCancelMidFlightActivation(t *testing.T) {
	term := Term{Name: "dispatcherCancelMidFlight", Mode: ModeReturn, Arg: `"cancelled"`}
	restore := Activate(term)
	defer restore()
	if !envContains(term.String()) {
		t.Errorf("env var missing %q after Activate", term)
	}
}

func TestBreakerTransitionRaceSiteRegistered(t *testing.T) {
	site := SiteByName("breakerTransitionRace")
	if site == nil {
		t.Fatal("Site breakerTransitionRace missing from catalogue")
	}
}

func TestBreakerTransitionRaceActivation(t *testing.T) {
	term := Term{Name: "breakerTransitionRace", Mode: ModeSleep, Arg: "1ms"}
	restore := Activate(term)
	defer restore()
	if !envContains(term.String()) {
		t.Errorf("env var missing %q after Activate", term)
	}
}

func TestPluginRPCBoundarySiteRegistered(t *testing.T) {
	site := SiteByName("pluginRPCBoundary")
	if site == nil {
		t.Fatal("Site pluginRPCBoundary missing from catalogue")
	}
}

func TestPluginRPCBoundaryActivation(t *testing.T) {
	term := Term{Name: "pluginRPCBoundary", Mode: ModeReturn, Arg: `"plugin_down"`}
	restore := Activate(term)
	defer restore()
	if !envContains(term.String()) {
		t.Errorf("env var missing %q after Activate", term)
	}
}

func TestSidecarRPCBoundarySiteRegistered(t *testing.T) {
	site := SiteByName("sidecarRPCBoundary")
	if site == nil {
		t.Fatal("Site sidecarRPCBoundary missing from catalogue")
	}
}

func TestSidecarRPCBoundaryActivation(t *testing.T) {
	term := Term{Name: "sidecarRPCBoundary", Mode: ModeReturn, Arg: `"sidecar_down"`}
	restore := Activate(term)
	defer restore()
	if !envContains(term.String()) {
		t.Errorf("env var missing %q after Activate", term)
	}
}

func TestDispatcherFailpointsBatchActivation(t *testing.T) {
	terms := []Term{
		{Name: "dispatcherCancelMidFlight", Mode: ModeReturn, Arg: `"cancelled"`},
		{Name: "breakerTransitionRace", Mode: ModeSleep, Arg: "1ms"},
		{Name: "pluginRPCBoundary", Mode: ModeReturn, Arg: `"plugin_down"`},
		{Name: "sidecarRPCBoundary", Mode: ModeReturn, Arg: `"sidecar_down"`},
	}
	restore := ActivateAll(terms...)
	defer restore()
	for _, term := range terms {
		if !envContains(term.String()) {
			t.Errorf("env var missing %q after ActivateAll", term)
		}
	}
}
