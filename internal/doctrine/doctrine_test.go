package doctrine

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestGetReturnsCorrectDoctrines(t *testing.T) {
	cases := []struct {
		name        Name
		archive     string
		advisory    bool
		privacyLock bool
	}{
		{NameMaxScope, "merge-commit", false, false},
		{NameDefault, "squash", false, false},
		{NameCapaFirewall, "merge-commit", true, true},
	}
	for _, c := range cases {
		t.Run(string(c.name), func(t *testing.T) {
			d, err := Get(c.name)
			if err != nil {
				t.Fatalf("Get(%q): %v", c.name, err)
			}
			if d.Name() != c.name {
				t.Errorf("Name = %q, want %q", d.Name(), c.name)
			}
			if d.ArchiveStrategy() != c.archive {
				t.Errorf("ArchiveStrategy = %q, want %q", d.ArchiveStrategy(), c.archive)
			}
			if d.RequireAdvisoryDefault() != c.advisory {
				t.Errorf("RequireAdvisoryDefault = %v, want %v", d.RequireAdvisoryDefault(), c.advisory)
			}
			if d.PrivacyLocked() != c.privacyLock {
				t.Errorf("PrivacyLocked = %v, want %v", d.PrivacyLocked(), c.privacyLock)
			}
		})
	}
}

func TestCapaFirewallExtras(t *testing.T) {
	d, _ := Get(NameCapaFirewall)
	if len(d.PreFlightExtras()) == 0 {
		t.Error("CapaFirewall PreFlightExtras should be non-empty")
	}
	if len(d.PreArchiveExtras()) == 0 {
		t.Error("CapaFirewall PreArchiveExtras should be non-empty")
	}
}

func TestUnknownDoctrineErrors(t *testing.T) {
	_, err := Get("invalid")
	if err == nil {
		t.Error("expected error for unknown doctrine")
	}
}

func TestIsValidAcceptsCanonical(t *testing.T) {
	for _, n := range []Name{NameMaxScope, NameDefault, NameCapaFirewall} {
		if !IsValid(n) {
			t.Errorf("IsValid(%q) = false, want true", n)
		}
	}
}

func TestIsValidRejectsDrifted(t *testing.T) {
	for _, s := range []string{
		"",
		"MAX-SCOPE",
		"Max-Scope",
		"max_scope",
		"Default",
		"capafirewall",
		"max-scope ",
		" default",
		"capa firewall",
		"capa-fire-wall",
		"strictest",
		"max",
	} {
		if IsValid(Name(s)) {
			t.Errorf("IsValid(%q) = true, want false", s)
		}
	}
}

func TestPackageConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "z.toml")
	body := []byte(`
schema_version = 1
name = "max-scope"
[research]
depth = "deep"
[budget.caps]
project = "9999.00 EUR"
doctrine = "9999.00 USD"
`)
	if err := os.WriteFile(tomlPath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	const goroutines = 32
	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines*iterations)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {

				if _, err := LoadFile(tomlPath); err != nil {
					errs <- err
					return
				}

				r := Resolver{
					ChosenDoctrine: "max-scope",
					ProjectPath:    tomlPath,
				}
				if _, err := r.Resolve(); err != nil {
					errs <- err
					return
				}

				if _, err := Builtin("max-scope"); err != nil {
					errs <- err
					return
				}
				if _, err := Builtin("default"); err != nil {
					errs <- err
					return
				}
				if _, err := Builtin("capa-firewall"); err != nil {
					errs <- err
					return
				}

				if _, err := ValidateAdditive(additiveDiff, "feat(doctrine): add"); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}
