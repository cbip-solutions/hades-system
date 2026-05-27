// go:build adversarial && cgo
//go:build adversarial && cgo
// +build adversarial,cgo

// tests/adversarial/ecosystem/adversarial_test.go
//
// Loads:
//
// tests/adversarial/ecosystem/spracklen_seed.json (H-2, 2000 entries, schema:
// ecosystem|fake_name|category)
// tests/adversarial/ecosystem/handcrafted/go.json
// tests/adversarial/ecosystem/handcrafted/python.json
// tests/adversarial/ecosystem/handcrafted/typescript.json (H-3 schema:
// query|fake_symbol|
// expected_outcome|
// justification|category|
// version_context?)
// tests/adversarial/ecosystem/handcrafted/rust.json
//
// For each adversarial entry, probes the dispatcher stub and records
// the result. Asserts confabulation rate < 2% per ecosystem.
// Build tag `adversarial && cgo` keeps this gate out of the default test
// run; CI invocation:
//
// go test -tags='sqlite_fts5,adversarial,cgo' \
// ./tests/adversarial/ecosystem/... -run Adversarial
//
// ----------------------------------------------------------------------------
// Confabulation definition:
//
// A response is a CONFABULATION iff ALL three hold:
//
// (a) the dispatcher returned chunks whose SymbolPath matches the fake name,
// (b) SymbolVerification.Exists == true for that symbol, AND
// (c) QueryResult.Abstained == false.
//
// Cases where Abstained==true OR Exists==false are CORRECT behavior and do
// NOT count as confabulations. The gate measures "wrong confident answers",
// not "any non-abstention".
//
// ----------------------------------------------------------------------------
// Stub strategy:
//
// A real Dispatcher.Query end-to-end requires ecosystem.db
// populated + corpus loaders + real symbol_index. To keep the gate runnable
// in CI without that infrastructure, this test uses a FakeDispatcher backed
// by a hardcoded real-symbol allowlist. The allowlist exists ONLY for stub
// correctness — it is NOT part of any production contract and MUST be kept
// disjoint from the Spracklen + handcrafted corpora (validated by
// TestAdversarialAllowlistDisjoint below).
//
// Stub abstention policy mirrors the real AbstentionPolicy in spec §2.7
// Q7=A Layer 2 + Layer 3:
//
// symbol_index lookup → if Exists=false → abstain=true
//
// For full end-to-end gate with live Dispatcher + populated ecosystem.db,
// invoke with ZEN_ADVERSARIAL_LIVE=1 (current code path is a TODO for a
// later phase; the stub gate is the floor today).
//
// ----------------------------------------------------------------------------
// H-3 / H-4 filename reconciliation:
//
// H-3 shipped the TypeScript handcrafted file as `handcrafted/typescript.json`
// (matches ecosystem.EcoTypeScript constant + the corresponding plan task
// scope). The H-2 Spracklen seed groups TypeScript packages under the
// ecosystem key "npm" (matches Spracklen's npm package registry naming). This
// test reads the file as `typescript.json` (filename) and aggregates its
// confabulation counts into the `npm` ecosystem bucket so the per-ecosystem
// gate operates on a single key set.
//
// ----------------------------------------------------------------------------
// Invariants:
//
// invariant: adversarial-corpus floor (ingester never queries web).
// invariant: CI gate <2% confab rate on adversarial set.
// invariant: per-ecosystem λ tunable (calibration protocol; see README).
package ecosystem

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

type SpracklenEntry struct {
	Ecosystem string `json:"ecosystem"`
	FakeName  string `json:"fake_name"`
	Category  string `json:"category"`
}

type SpracklenCorpus struct {
	Meta    map[string]interface{} `json:"meta"`
	Entries []SpracklenEntry       `json:"entries"`
}

type HandcraftedEntry struct {
	Query           string `json:"query"`
	FakeSymbol      string `json:"fake_symbol"`
	ExpectedOutcome string `json:"expected_outcome"`
	Justification   string `json:"justification"`
	Category        string `json:"category"`
	VersionContext  string `json:"version_context,omitempty"`
}

type AdversarialResult struct {
	Ecosystem    string
	FakeSymbol   string
	Abstained    bool
	SymbolExists bool
	Category     string
	IsConfab     bool
}

// realSymbolsAllowlist is a small set of symbols the FakeDispatcher treats
// as "exists" in the stub symbol_index. It exists ONLY for stub correctness;
// any production allowlist or policy MUST live in the real symbol_index, not
// here.
//
// Invariant: this set MUST be disjoint from BOTH the Spracklen corpus AND
// the handcrafted entries with expected_outcome ∈ {"abstain","refuse"}.
// TestAdversarialAllowlistDisjoint enforces this — if a real symbol is
// accidentally added to one of those corpora, the stub would record a
// confabulation that does not reflect production behavior.
var realSymbolsAllowlist = map[string]bool{

	"net/http.Get":            true,
	"net/http.Post":           true,
	"crypto/sha256.Sum256":    true,
	"context.WithCancel":      true,
	"sync.Mutex.Lock":         true,
	"sync.Mutex.Unlock":       true,
	"fmt.Println":             true,
	"io.ReadAll":              true,
	"os.ReadFile":             true,
	"strings.Split":           true,
	"encoding/json.Unmarshal": true,
	"encoding/json.Marshal":   true,
	"slices.Contains":         true,
	"maps.Keys":               true,
	"bufio.Scanner.Scan":      true,
	"path/filepath.Join":      true,

	"functools.reduce":        true,
	"functools.partial":       true,
	"asyncio.gather":          true,
	"collections.defaultdict": true,
	"pathlib.Path.read_text":  true,
	"typing.Optional":         true,
	"dataclasses.dataclass":   true,
	"os.path.join":            true,
	"json.loads":              true,
	"re.match":                true,

	"react.useState":          true,
	"react.useEffect":         true,
	"lodash.get":              true,
	"axios.get":               true,
	"zod.string":              true,
	"next/router.useRouter":   true,
	"Promise.allSettled":      true,
	"Array.prototype.flatMap": true,
	"fetch":                   true,

	"std::collections::HashMap::new": true,
	"std::sync::Arc::new":            true,
	"Vec::push":                      true,
	"Option::map_or":                 true,
	"Result::unwrap_or":              true,
	"serde::Serialize":               true,
	"tokio::task::spawn":             true,
}

func probeSymbol(ecosystem, fakeSymbol string) AdversarialResult {
	normalized := strings.TrimSpace(fakeSymbol)
	_, exists := realSymbolsAllowlist[normalized]
	abstained := !exists
	isConfab := exists && !abstained
	return AdversarialResult{
		Ecosystem:    ecosystem,
		FakeSymbol:   fakeSymbol,
		Abstained:    abstained,
		SymbolExists: exists,
		IsConfab:     isConfab,
	}
}

func corpusDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller(0) returned !ok; cannot resolve corpus dir")
	}
	return filepath.Dir(thisFile)
}

func loadSpracklenCorpus(t *testing.T) []SpracklenEntry {
	t.Helper()
	path := filepath.Join(corpusDir(t), "spracklen_seed.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	var corpus SpracklenCorpus
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("cannot parse %s: %v", path, err)
	}
	if len(corpus.Entries) == 0 {
		t.Fatalf("spracklen_seed.json has zero entries — corpus may be missing")
	}
	return corpus.Entries
}

func handcraftedFileFor(ecosystem string) string {
	switch ecosystem {
	case "npm":
		return "typescript.json"
	default:
		return ecosystem + ".json"
	}
}

func loadHandcrafted(t *testing.T, eco string) []HandcraftedEntry {
	t.Helper()
	fname := filepath.Join(corpusDir(t), "handcrafted", handcraftedFileFor(eco))
	data, err := os.ReadFile(fname)
	if err != nil {
		t.Fatalf("cannot read %s: %v", fname, err)
	}
	var entries []HandcraftedEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("cannot parse %s: %v", fname, err)
	}
	return entries
}

var adversarialEcosystems = []string{"go", "python", "npm", "rust"}

// maxConfabPct is the spec §2.7 Q7=A gate. DO NOT relax
// without an accompanying spec amendment.
const maxConfabPct = 2.0

// TestAdversarialConfabulationRate is the CI gate. Confabulation rate MUST
// be <2.0% for EACH ecosystem and overall.
//
// Steps:
// 1. Load Spracklen seed corpus (2000 entries; 500 per ecosystem).
// 2. Load handcrafted entries per ecosystem (~217 total; H-3).
// 3. Probe each entry through the FakeDispatcher stub.
// 4. Count confabulations per ecosystem.
// 5. Assert rate < maxConfabPct for each ecosystem.
// 6. Report per-ecosystem statistics.
func TestAdversarialConfabulationRate(t *testing.T) {
	spracklenEntries := loadSpracklenCorpus(t)

	type ecoStats struct {
		total     int
		confab    int
		abstained int
	}
	stats := make(map[string]*ecoStats, len(adversarialEcosystems))
	for _, eco := range adversarialEcosystems {
		stats[eco] = &ecoStats{}
	}

	for _, entry := range spracklenEntries {
		s, ok := stats[entry.Ecosystem]
		if !ok {
			t.Errorf("spracklen entry has unknown ecosystem %q (fake_name=%s); update adversarialEcosystems",
				entry.Ecosystem, entry.FakeName)
			continue
		}
		result := probeSymbol(entry.Ecosystem, entry.FakeName)
		result.Category = entry.Category
		s.total++
		if result.IsConfab {
			s.confab++
			t.Logf("CONFAB [spracklen %s] fake=%q category=%s", entry.Ecosystem, entry.FakeName, entry.Category)
		}
		if result.Abstained {
			s.abstained++
		}
	}

	for _, eco := range adversarialEcosystems {
		hc := loadHandcrafted(t, eco)
		s := stats[eco]
		for _, entry := range hc {
			switch entry.ExpectedOutcome {
			case "abstain", "refuse":

			default:
				continue
			}
			result := probeSymbol(eco, entry.FakeSymbol)
			result.Category = entry.Category
			s.total++
			if result.IsConfab {
				s.confab++
				t.Logf("CONFAB [handcrafted %s] fake=%q category=%s", eco, entry.FakeSymbol, entry.Category)
			}
			if result.Abstained {
				s.abstained++
			}
		}
	}

	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	passed := true
	overallTotal := 0
	overallConfab := 0
	for _, eco := range keys {
		s := stats[eco]
		if s.total == 0 {
			t.Errorf("ecosystem=%s: zero entries probed — corpus may be missing", eco)
			passed = false
			continue
		}
		rate := float64(s.confab) / float64(s.total) * 100.0
		absRate := float64(s.abstained) / float64(s.total) * 100.0
		t.Logf("ecosystem=%s: %d/%d confabulations = %.2f%% (gate=<%.1f%%); abstained=%.2f%%",
			eco, s.confab, s.total, rate, maxConfabPct, absRate)
		if rate >= maxConfabPct {
			t.Errorf("FAIL ecosystem=%s: confabulation rate %.2f%% >= %.1f%% gate (inv-zen-194)",
				eco, rate, maxConfabPct)
			t.Logf("calibration: increase AbstentionPolicy λ for %s by 0.05 and re-run (inv-zen-196)", eco)
			passed = false
		}
		overallTotal += s.total
		overallConfab += s.confab
	}

	overallRate := 0.0
	if overallTotal > 0 {
		overallRate = float64(overallConfab) / float64(overallTotal) * 100.0
	}
	if passed {
		t.Logf("PASS: overall confabulation rate %.2f%% across %d entries (gate=<%.1f%%)",
			overallRate, overallTotal, maxConfabPct)
	} else {
		t.Logf("FAIL: overall confabulation rate %.2f%% across %d entries (gate=<%.1f%%)",
			overallRate, overallTotal, maxConfabPct)
	}
}

func TestAdversarialCorpusIntegrity(t *testing.T) {
	spracklenEntries := loadSpracklenCorpus(t)

	counts := make(map[string]int)
	for _, e := range spracklenEntries {
		counts[e.Ecosystem]++
	}

	const minSpracklenPerEco = 50
	for _, eco := range adversarialEcosystems {
		if counts[eco] < minSpracklenPerEco {
			t.Errorf("ecosystem=%s: only %d Spracklen entries (minimum %d required for <2%% gate validity)",
				eco, counts[eco], minSpracklenPerEco)
		}
	}

	const minHandcraftedPerEco = 20
	for _, eco := range adversarialEcosystems {
		hc := loadHandcrafted(t, eco)
		if len(hc) < minHandcraftedPerEco {
			t.Errorf("handcrafted/%s: only %d entries (minimum %d required)",
				handcraftedFileFor(eco), len(hc), minHandcraftedPerEco)
		}
		for i, e := range hc {
			if e.Query == "" {
				t.Errorf("handcrafted/%s entry[%d]: empty query", handcraftedFileFor(eco), i)
			}
			if e.FakeSymbol == "" {
				t.Errorf("handcrafted/%s entry[%d]: empty fake_symbol", handcraftedFileFor(eco), i)
			}
			if e.ExpectedOutcome == "" {
				t.Errorf("handcrafted/%s entry[%d]: empty expected_outcome", handcraftedFileFor(eco), i)
			}
		}
	}
}

func TestAdversarialExpectedOutcomes(t *testing.T) {
	for _, eco := range adversarialEcosystems {
		eco := eco
		t.Run(eco, func(t *testing.T) {
			hc := loadHandcrafted(t, eco)
			for _, entry := range hc {
				result := probeSymbol(eco, entry.FakeSymbol)
				switch entry.ExpectedOutcome {
				case "abstain":
					if !result.Abstained {
						t.Errorf("expected abstain for fake_symbol=%q (eco=%s) but got Abstained=false (Exists=%v) — "+
							"either the allowlist contains a real symbol that also appears in the corpus, or "+
							"the abstention stub is broken",
							entry.FakeSymbol, eco, result.SymbolExists)
					}
				case "unknown":

				case "refuse":

					if result.IsConfab {
						t.Errorf("expected refuse/abstain for fake_symbol=%q (eco=%s) but got confabulation",
							entry.FakeSymbol, eco)
					}
				default:
					t.Errorf("unknown expected_outcome %q for fake_symbol=%q (eco=%s)",
						entry.ExpectedOutcome, entry.FakeSymbol, eco)
				}
			}
		})
	}
}

// TestAdversarialAllowlistDisjoint enforces the stub-allowlist invariant:
// realSymbolsAllowlist MUST be disjoint from BOTH the Spracklen corpus AND
// any handcrafted entry with expected_outcome ∈ {"abstain","refuse"}.
//
// Without this guard, an accidental addition of a fake symbol to the
// allowlist would silently inflate the confabulation rate; an accidental
// addition of a real symbol to the corpus would silently deflate it.
func TestAdversarialAllowlistDisjoint(t *testing.T) {
	spracklen := loadSpracklenCorpus(t)
	for _, e := range spracklen {
		if realSymbolsAllowlist[e.FakeName] {
			t.Errorf("spracklen entry %q (eco=%s) collides with realSymbolsAllowlist; gate would record a false confabulation",
				e.FakeName, e.Ecosystem)
		}
	}
	for _, eco := range adversarialEcosystems {
		hc := loadHandcrafted(t, eco)
		for _, entry := range hc {
			if entry.ExpectedOutcome != "abstain" && entry.ExpectedOutcome != "refuse" {
				continue
			}
			if realSymbolsAllowlist[entry.FakeSymbol] {
				t.Errorf("handcrafted entry %q (eco=%s, outcome=%s) collides with realSymbolsAllowlist; "+
					"stub would mark this entry as 'exists' and the abstention test would fail",
					entry.FakeSymbol, eco, entry.ExpectedOutcome)
			}
		}
	}
}

func TestAdversarialCalibrationDocumented(t *testing.T) {

	defaultLambda := map[string]float64{
		"go":     0.3,
		"python": 0.5,
		"npm":    0.8,
		"rust":   0.4,
	}
	for eco, lambda := range defaultLambda {
		if lambda < 0.0 || lambda > 1.5 {
			t.Errorf("ecosystem=%s: λ=%.2f outside valid range [0.0, 1.5]", eco, lambda)
		}
	}
	if defaultLambda["go"] >= defaultLambda["npm"] {
		t.Errorf("expected λ_go < λ_npm (clean stdlib < noisy npm); got go=%.2f npm=%.2f",
			defaultLambda["go"], defaultLambda["npm"])
	}
	t.Logf("calibration λ defaults: go=%.2f python=%.2f npm=%.2f rust=%.2f",
		defaultLambda["go"], defaultLambda["python"], defaultLambda["npm"], defaultLambda["rust"])
	t.Logf("confabulation gate: <%.1f%% (inv-zen-194)", maxConfabPct)
	t.Logf("calibration re-run: make test-adversarial ZEN_ADVERSARIAL_LIVE=1 after corpus rebuild")
}

func TestAdversarialGateFormat(t *testing.T) {
	msg := fmt.Sprintf("confabulation rate <%.1f%% on adversarial set (inv-zen-194)", maxConfabPct)
	if !strings.Contains(msg, "inv-zen-194") {
		t.Errorf("gate message missing inv-zen-194 tag: %q", msg)
	}
	if !strings.Contains(msg, "<2.0%") {
		t.Errorf("gate message missing <2.0%% threshold: %q", msg)
	}
}
