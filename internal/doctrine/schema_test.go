package doctrine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestSchemaZeroValueStable(t *testing.T) {
	var s Schema
	if s.SchemaVersion != 0 {
		t.Errorf("zero SchemaVersion = %d, want 0", s.SchemaVersion)
	}

	if got := s.Future["plan_99"]; got != nil {
		t.Errorf("Future[plan_99] on zero value = %v, want nil", got)
	}
}

func TestSchemaAllAxesPresent(t *testing.T) {
	required := []string{
		"SchemaVersion",
		"Name",
		"Research",
		"Subprocess",
		"Reviewer",
		"Budget",
		"Workforce",
		"Apply",
		"Watcher",
		"Future",
	}
	v := reflect.TypeOf(Schema{})
	got := map[string]bool{}
	for i := 0; i < v.NumField(); i++ {
		got[v.Field(i).Name] = true
	}
	for _, name := range required {
		if !got[name] {
			t.Errorf("Schema missing required top-level field %q", name)
		}
	}
}

func TestSchemaResearchFields(t *testing.T) {
	r := ResearchAxis{
		CadencePerStage: map[string]string{"design": "always", "build": "on-demand"},
		Depth:           "deep",
		Sources:         []string{"web_search", "arxiv", "github_search", "code_graph"},
		CacheTTL:        Duration(7 * 24 * time.Hour),
		AgenticMaxIter:  5,
	}
	if r.CadencePerStage["design"] != "always" {
		t.Errorf("CadencePerStage missing")
	}
	if r.Depth != "deep" {
		t.Errorf("Depth = %q, want %q", r.Depth, "deep")
	}
	if len(r.Sources) != 4 {
		t.Errorf("Sources len = %d, want 4", len(r.Sources))
	}
	if r.CacheTTL != Duration(7*24*time.Hour) {
		t.Errorf("CacheTTL = %v, want 7d", time.Duration(r.CacheTTL))
	}
	if r.AgenticMaxIter != 5 {
		t.Errorf("AgenticMaxIter = %d, want 5", r.AgenticMaxIter)
	}
}

func TestSchemaSubprocessFields(t *testing.T) {
	s := SubprocessAxis{
		EphemeralDefaultTimeout: Duration(30 * time.Minute),
		PersistentTTLSliding:    Duration(8 * time.Hour),
		PreWarmPoolSize:         3,
	}
	if s.PreWarmPoolSize != 3 {
		t.Errorf("PreWarmPoolSize = %d, want 3", s.PreWarmPoolSize)
	}
	if time.Duration(s.PersistentTTLSliding) != 8*time.Hour {
		t.Errorf("PersistentTTLSliding = %v, want 8h", time.Duration(s.PersistentTTLSliding))
	}
}

func TestSchemaReviewerFields(t *testing.T) {
	r := ReviewerAxis{
		FamilyDisjointPool: []string{"anthropic", "google", "deepseek", "local-qwen"},
		CriteriaDefault:    "default",
	}
	if len(r.FamilyDisjointPool) < 2 {
		t.Errorf("FamilyDisjointPool too small: %d", len(r.FamilyDisjointPool))
	}
}

func TestSchemaBudgetFields(t *testing.T) {
	b := BudgetAxis{
		Caps: BudgetCaps{
			Project:   Money("50.00 USD"),
			Doctrine:  Money("100.00 USD"),
			Stage:     map[string]Money{"design": "5.00 USD"},
			Task:      map[string]Money{"complex": "2.00 USD"},
			Operation: map[string]Money{"audit_review": "0.10 USD"},
		},
		PauseMode:         "descriptive",
		AnomalyZThreshold: 4.0,
		AnomalyWindowSize: 60,
	}
	if b.Caps.Project != "50.00 USD" {
		t.Errorf("Caps.Project = %q", b.Caps.Project)
	}
	if b.Caps.Stage["design"] != "5.00 USD" {
		t.Errorf("Caps.Stage[design] = %q", b.Caps.Stage["design"])
	}
	if b.PauseMode != "descriptive" {
		t.Errorf("PauseMode = %q", b.PauseMode)
	}
	if b.AnomalyZThreshold != 4.0 {
		t.Errorf("AnomalyZThreshold = %f", b.AnomalyZThreshold)
	}
	if b.AnomalyWindowSize != 60 {
		t.Errorf("AnomalyWindowSize = %d", b.AnomalyWindowSize)
	}
}

func TestSchemaWorkforceFields(t *testing.T) {
	w := WorkforceAxis{
		WritablePathsPolicy:                  "non-overlapping",
		DoctrineReinforcementTemplatePointer: "templates/doctrine/max-scope.txt",
	}
	if w.WritablePathsPolicy != "non-overlapping" {
		t.Errorf("WritablePathsPolicy = %q", w.WritablePathsPolicy)
	}
}

func TestSchemaApplyReservedForPlan6(t *testing.T) {
	a := ApplyAxis{
		MergeStrategy:    "three-way",
		ConflictHandling: "manual",
	}
	if a.MergeStrategy != "three-way" {
		t.Errorf("MergeStrategy = %q", a.MergeStrategy)
	}
	if a.ConflictHandling != "manual" {
		t.Errorf("ConflictHandling = %q", a.ConflictHandling)
	}
}

func TestSchemaWatcherFields(t *testing.T) {
	w := WatcherAxis{
		Cadence:   Duration(15 * time.Minute),
		CPUBudget: 0.05,
	}
	if time.Duration(w.Cadence) != 15*time.Minute {
		t.Errorf("Cadence = %v", time.Duration(w.Cadence))
	}
	if w.CPUBudget != 0.05 {
		t.Errorf("CPUBudget = %f", w.CPUBudget)
	}
}

func TestSchemaFutureFieldsMap(t *testing.T) {
	s := Schema{
		Future: map[string]map[string]any{
			"plan_6":  {"apply_strategy_alpha": "three-way"},
			"plan_11": {"wizard_default_doctrine": "default"},
		},
	}
	if s.Future["plan_6"]["apply_strategy_alpha"] != "three-way" {
		t.Errorf("Future[plan_6] not preserved")
	}
}

func TestMoneyFormat(t *testing.T) {
	cases := []struct {
		in    string
		valid bool
	}{
		{"50.00 USD", true},
		{"0.10 USD", true},
		{"800 USD", true},
		{"USD 50", false},
		{"50", false},
		{"", false},
		{"50.00", false},
	}
	for _, c := range cases {
		got := Money(c.in).Valid()
		if got != c.valid {
			t.Errorf("Money(%q).Valid() = %v, want %v", c.in, got, c.valid)
		}
	}
}

func TestMoneyParse(t *testing.T) {
	amt, cur, err := Money("50.00 USD").Parse()
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if amt != 50.00 || cur != "USD" {
		t.Errorf("Parse: amt=%f cur=%q", amt, cur)
	}
}

func TestMoneyParseInvalid(t *testing.T) {
	if _, _, err := Money("garbage").Parse(); err == nil {
		t.Error("Parse(garbage) returned nil error")
	}
}

func TestMoneyParseBadAmount(t *testing.T) {
	if _, _, err := Money("abc USD").Parse(); err == nil {
		t.Error("Parse('abc USD') returned nil error")
	}
}

func TestMoneyParseLowercaseCurrency(t *testing.T) {
	if _, _, err := Money("10.00 usd").Parse(); err == nil {
		t.Error("Parse('10.00 usd') returned nil error")
	}
}

func TestDurationMarshalUnmarshalTOML(t *testing.T) {
	d := Duration(30 * time.Minute)
	b, err := d.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	if !strings.Contains(string(b), "30") {
		t.Errorf("MarshalText = %q", string(b))
	}
	var d2 Duration
	if err := d2.UnmarshalText([]byte("30m")); err != nil {
		t.Fatalf("UnmarshalText: %v", err)
	}
	if time.Duration(d2) != 30*time.Minute {
		t.Errorf("UnmarshalText = %v, want 30m", time.Duration(d2))
	}
}

func TestDurationUnmarshalInvalid(t *testing.T) {
	var d Duration
	if err := d.UnmarshalText([]byte("not-a-duration")); err == nil {
		t.Error("UnmarshalText(garbage) returned nil error")
	}
}

func TestDurationUnmarshalEmpty(t *testing.T) {
	var d Duration
	if err := d.UnmarshalText([]byte("")); err == nil {
		t.Error("UnmarshalText('') returned nil error")
	}
}

func TestSchemaCapacityCeiling(t *testing.T) {
	s := Schema{
		Budget: BudgetAxis{Caps: BudgetCaps{Project: "100.00 USD"}},
	}
	c := s.Ceiling()
	if c.Budget.Caps.Project != "100.00 USD" {
		t.Errorf("Ceiling().Budget.Caps.Project = %q", c.Budget.Caps.Project)
	}
}

func TestSchemaTOMLTagsPresent(t *testing.T) {
	v := reflect.TypeOf(Schema{})
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Tag.Get("toml") == "" {
			t.Errorf("field %s missing toml tag", f.Name)
		}
	}
}

func TestSchemaTOMLRoundTrip(t *testing.T) {
	cases := map[string]Schema{
		"max-scope":     MaxScopeBuiltin(),
		"default":       DefaultBuiltin(),
		"capa-firewall": CapaFirewallBuiltin(),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			body, err := toml.Marshal(&in)
			if err != nil {
				t.Fatalf("toml.Marshal: %v", err)
			}
			var out Schema
			if _, err := toml.Decode(string(body), &out); err != nil {
				t.Fatalf("toml.Decode: %v\nbody:\n%s", err, body)
			}
			normaliseSchema(&in)
			normaliseSchema(&out)
			if !reflect.DeepEqual(in, out) {
				t.Errorf("round-trip drift; in=%+v, out=%+v", in, out)
			}
		})
	}
}

func TestSchemaJSONRoundTrip(t *testing.T) {
	cases := map[string]Schema{
		"max-scope":     MaxScopeBuiltin(),
		"default":       DefaultBuiltin(),
		"capa-firewall": CapaFirewallBuiltin(),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(&in)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			var out Schema
			if err := json.Unmarshal(body, &out); err != nil {
				t.Fatalf("json.Unmarshal: %v\nbody:\n%s", err, body)
			}
			normaliseSchema(&in)
			normaliseSchema(&out)
			if !reflect.DeepEqual(in, out) {
				t.Errorf("round-trip drift; in=%+v, out=%+v", in, out)
			}
		})
	}
}

func normaliseSchema(s *Schema) {
	if len(s.Future) == 0 {
		s.Future = map[string]map[string]any{}
	}
}
