package compliance

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

const inv221GoldenFixtureRel = "tests/compliance/testdata/status_schema_v1.json"

// inv221RequiredFieldNames enumerates the 8 field names that MUST exist
// in the "fields" top-level key. These are the schema-v1 stability anchors
// per spec §Q5 + Phase C status.py _render_json.
var inv221RequiredFieldNames = []string{
	"daemon",
	"model",
	"cascade",
	"bypass",
	"cost_24h",
	"context",
	"profile",
	"cwd",
}

func TestInvZen221StatusSchemaStability(t *testing.T) {
	root := repoRoot(t)
	fixturePath := filepath.Join(root, inv221GoldenFixtureRel)

	fixtureBody, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("inv-zen-221: cannot read golden fixture %s: %v", fixturePath, err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(fixtureBody, &fixture); err != nil {
		t.Fatalf("inv-zen-221: fixture JSON decode: %v", err)
	}

	schemaVersion, ok := fixture["schema_version"]
	if !ok {
		t.Fatalf("inv-zen-221: schema_version field missing from golden fixture %s", inv221GoldenFixtureRel)
	}
	v, ok := schemaVersion.(float64)
	if !ok || int(v) != 1 {
		t.Errorf("inv-zen-221: schema_version expected 1, got %v (%T)", schemaVersion, schemaVersion)
	}

	rawFields, ok := fixture["fields"]
	if !ok {
		t.Fatalf("inv-zen-221: top-level 'fields' key missing from golden fixture")
	}
	fields, ok := rawFields.(map[string]any)
	if !ok {
		t.Fatalf("inv-zen-221: 'fields' is not a JSON object; got %T", rawFields)
	}

	var missingFields []string
	for _, name := range inv221RequiredFieldNames {
		if _, exists := fields[name]; !exists {
			missingFields = append(missingFields, name)
		}
	}
	if len(missingFields) > 0 {
		sort.Strings(missingFields)
		t.Errorf("inv-zen-221: required field(s) missing from golden fixture 'fields': %v\n"+
			"Remediation: update Phase C status.py to include the missing fields + update the golden fixture.", missingFields)
	}

	for name, rawField := range fields {
		field, ok := rawField.(map[string]any)
		if !ok {
			t.Errorf("inv-zen-221: field %q is not a JSON object; got %T", name, rawField)
			continue
		}
		if _, hasState := field["state"]; !hasState {
			t.Errorf("inv-zen-221: field %q missing required 'state' sub-key (each schema-v1 field must declare its degradation state)", name)
		}
	}

	requiredSet := map[string]bool{}
	for _, name := range inv221RequiredFieldNames {
		requiredSet[name] = true
	}
	var surprises []string
	for name := range fields {
		if !requiredSet[name] {
			surprises = append(surprises, name)
		}
	}
	if len(surprises) > 0 {
		sort.Strings(surprises)
		t.Errorf("inv-zen-221: surprise extra fields in golden fixture 'fields': %v\n"+
			"Remediation: update inv221RequiredFieldNames to include the new fields + bump schema_version in status.py if the change is intentional.", surprises)
	}
}

func TestInvZen221StatusSchemaAssembly(t *testing.T) {
	root := repoRoot(t)
	fixturePath := filepath.Join(root, inv221GoldenFixtureRel)

	fixtureBody, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("inv-zen-221: cannot read golden fixture %s: %v", fixturePath, err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(fixtureBody, &fixture); err != nil {
		t.Fatalf("inv-zen-221: fixture JSON decode: %v", err)
	}
	fixtureFields, _ := fixture["fields"].(map[string]any)

	ts := inv221NewStubServer(t)
	defer ts.Close()

	assembled, err := inv221AssembleStatusFields(ts.URL)
	if err != nil {
		t.Fatalf("inv-zen-221: assemble status fields: %v", err)
	}

	inv221FilterEphemeralFields(assembled)

	var driftKeys []string
	for _, name := range inv221RequiredFieldNames {
		fixtureField, fixtureOK := fixtureFields[name].(map[string]any)
		assembledField, assembledOK := assembled[name].(map[string]any)
		if !fixtureOK || !assembledOK {

			continue
		}
		if !inv221MapKeysEqual(fixtureField, assembledField) {
			driftKeys = append(driftKeys, name)
			t.Errorf("inv-zen-221: field %q keys mismatch:\n  fixture keys: %v\n  assembled keys: %v",
				name, inv221SortedKeys(fixtureField), inv221SortedKeys(assembledField))
		}
	}
	if len(driftKeys) > 0 {
		t.Errorf("inv-zen-221: schema drift detected in %d field(s): %v\n"+
			"Remediation: update Phase C status.py _render_json + the golden fixture + bump schema_version.",
			len(driftKeys), driftKeys)
	}
}

func inv221NewStubServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeInvJSON(w, map[string]any{
			"status":         "ok",
			"version":        "0.17.0",
			"uptime_seconds": 60,
			"pid":            84821,
			"uds_path":       "/tmp/zen-swarm.sock",
			"active_model":   "opus-4-7",
		})
	})

	mux.HandleFunc("/v1/cascade/state", func(w http.ResponseWriter, _ *http.Request) {
		writeInvJSON(w, map[string]any{
			"active_tier":    1,
			"tier_name":      "anthropic-paygo",
			"provider_count": 12,
		})
	})

	mux.HandleFunc("/v1/bypass/status", func(w http.ResponseWriter, _ *http.Request) {
		writeInvJSON(w, map[string]any{
			"status":           "live",
			"success_rate_24h": 1.0,
		})
	})

	mux.HandleFunc("/v1/cost/24h", func(w http.ResponseWriter, _ *http.Request) {
		writeInvJSON(w, map[string]any{
			"spend_24h_usd":     0.043,
			"spend_session_usd": 0.041,
		})
	})

	mux.HandleFunc("/v1/context/used", func(w http.ResponseWriter, _ *http.Request) {
		writeInvJSON(w, map[string]any{
			"used_tokens": 24200,
			"max_tokens":  100000,
		})
	})

	mux.HandleFunc("/v1/profile/active", func(w http.ResponseWriter, _ *http.Request) {
		writeInvJSON(w, map[string]any{
			"profile_name": "max-scope",
			"kind":         "doctrine",
		})
	})

	mux.HandleFunc("/v1/cwd", func(w http.ResponseWriter, _ *http.Request) {
		writeInvJSON(w, map[string]any{
			"cwd": "/path/to/projects/hades-system",
		})
	})

	return httptest.NewServer(mux)
}

func writeInvJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func inv221AssembleStatusFields(baseURL string) (map[string]any, error) {
	endpoints := []string{
		"/v1/health",
		"/v1/cascade/state",
		"/v1/bypass/status",
		"/v1/cost/24h",
		"/v1/context/used",
		"/v1/profile/active",
		"/v1/cwd",
	}
	responses := map[string]map[string]any{}
	for _, ep := range endpoints {
		resp, err := http.Get(baseURL + ep)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			return nil, err
		}
		responses[ep] = data
	}

	health := responses["/v1/health"]
	cascade := responses["/v1/cascade/state"]
	bypass := responses["/v1/bypass/status"]
	cost := responses["/v1/cost/24h"]
	ctx := responses["/v1/context/used"]
	profile := responses["/v1/profile/active"]
	cwd := responses["/v1/cwd"]

	state := func(m map[string]any) string {
		if m == nil {
			return "error"
		}
		return "ok"
	}

	daemonField := map[string]any{"state": state(health)}
	if health != nil {
		daemonField["pid"] = health["pid"]
		daemonField["uds_path"] = health["uds_path"]
	}

	modelField := map[string]any{"state": state(health)}
	if health != nil {
		modelField["active_model"] = health["active_model"]
	}

	cascadeField := map[string]any{"state": state(cascade)}
	if cascade != nil {
		cascadeField["active_tier"] = cascade["active_tier"]
		cascadeField["tier_name"] = cascade["tier_name"]
		cascadeField["provider_count"] = cascade["provider_count"]
	}

	bypassField := map[string]any{"state": state(bypass)}
	if bypass != nil {
		bypassField["status"] = bypass["status"]
		bypassField["success_rate_24h"] = bypass["success_rate_24h"]
	}

	costField := map[string]any{"state": state(cost)}
	if cost != nil {
		costField["spend_24h_usd"] = cost["spend_24h_usd"]
		costField["spend_session_usd"] = cost["spend_session_usd"]
	}

	contextField := map[string]any{"state": state(ctx)}
	if ctx != nil {
		contextField["used_tokens"] = ctx["used_tokens"]
		contextField["max_tokens"] = ctx["max_tokens"]
	}

	profileField := map[string]any{"state": state(profile)}
	if profile != nil {
		profileField["profile_name"] = profile["profile_name"]
		profileField["kind"] = profile["kind"]
	}

	cwdField := map[string]any{"state": state(cwd)}
	if cwd != nil {
		cwdField["cwd"] = cwd["cwd"]
	}

	return map[string]any{
		"daemon":   daemonField,
		"model":    modelField,
		"cascade":  cascadeField,
		"bypass":   bypassField,
		"cost_24h": costField,
		"context":  contextField,
		"profile":  profileField,
		"cwd":      cwdField,
	}, nil
}

func inv221FilterEphemeralFields(fields map[string]any) {
	if daemon, ok := fields["daemon"].(map[string]any); ok {
		if _, hasPID := daemon["pid"]; hasPID {
			daemon["pid"] = "<filtered>"
		}
	}
	if cwdField, ok := fields["cwd"].(map[string]any); ok {
		if _, hasCWD := cwdField["cwd"]; hasCWD {
			cwdField["cwd"] = "<filtered>"
		}
	}
}

func inv221MapKeysEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func inv221SortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
