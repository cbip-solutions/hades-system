package testharness

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestLLMFakeBasicSynthesize(t *testing.T) {
	fake := NewLLMFake(LLMFakeOptions{
		SynthesizeText: "Synthesized findings: X is true because Y.",
	})
	defer fake.Close()

	body := `{"model":"opus-via-bypass","messages":[{"role":"user","content":"Synthesize the findings"}]}`
	req, err := http.NewRequest("POST", fake.URL()+"/v1/messages", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Zen-Profile", "research-synthesize")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(got), "Synthesized findings") {
		t.Fatalf("body = %s", got)
	}
}

func TestLLMFakeGapDetectionTrue(t *testing.T) {
	fake := NewLLMFake(LLMFakeOptions{
		GapDetected:    true,
		FollowupQuery:  "deep dive into Y",
		SynthesizeText: "ignored",
	})
	defer fake.Close()

	body := `{"model":"opus","messages":[{"role":"user","content":"Are there gaps?"}]}`
	req, _ := http.NewRequest("POST", fake.URL()+"/v1/messages", bytes.NewReader([]byte(body)))
	req.Header.Set("X-Zen-Profile", "research-gap-detection")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatal(err)
	}
	content := parsed["content"].([]any)
	first := content[0].(map[string]any)
	text := first["text"].(string)
	if !strings.Contains(text, "deep dive into Y") {
		t.Fatalf("expected followup query in response: %s", text)
	}
}

func TestLLMFakeReturnsHallucinatedURL(t *testing.T) {
	fake := NewLLMFake(LLMFakeOptions{
		SynthesizeText: "Per https://nonexistent-domain-zenswarm-test.invalid/paper, X is true.",
	})
	defer fake.Close()
	resp, err := http.Post(fake.URL()+"/v1/messages", "application/json",
		bytes.NewReader([]byte(`{"messages":[{"role":"user","content":"q"}]}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(got), "nonexistent-domain-zenswarm-test.invalid") {
		t.Fatalf("expected hallucinated URL in body: %s", got)
	}
}

func TestLLMFakeProfileEcho(t *testing.T) {
	fake := NewLLMFake(LLMFakeOptions{SynthesizeText: "ok"})
	defer fake.Close()
	for _, p := range []string{"research-synthesize", "research-gap-detection"} {
		req, _ := http.NewRequest("POST", fake.URL()+"/v1/messages",
			bytes.NewReader([]byte(`{"messages":[]}`)))
		req.Header.Set("X-Zen-Profile", p)
		_, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
	}
	got := fake.SeenProfiles()
	if len(got) != 2 || got[0] != "research-synthesize" || got[1] != "research-gap-detection" {
		t.Fatalf("seen profiles = %v", got)
	}
}

func TestLLMFakeCapStatusBlocked(t *testing.T) {
	fake := NewLLMFake(LLMFakeOptions{AllowBudget: false, SynthesizeText: "x"})
	defer fake.Close()
	resp, err := http.Get(fake.URL() + "/v1/budget/cap_status?axis=stage&value=design")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	var p map[string]any
	if err := json.Unmarshal(got, &p); err != nil {
		t.Fatal(err)
	}
	if allowed, _ := p["allowed"].(bool); allowed {
		t.Fatalf("expected blocked, got allowed=true: %s", got)
	}
}

func TestLLMFakeCapStatusAllowed(t *testing.T) {
	fake := NewLLMFake(LLMFakeOptions{})
	defer fake.Close()
	resp, err := http.Get(fake.URL() + "/v1/budget/cap_status?axis=op&value=research")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	var p map[string]any
	if err := json.Unmarshal(got, &p); err != nil {
		t.Fatal(err)
	}
	if allowed, _ := p["allowed"].(bool); !allowed {
		t.Fatalf("expected allowed, got %s", got)
	}
}

func TestLLMFakeCacheGetMiss(t *testing.T) {
	fake := NewLLMFake(LLMFakeOptions{})
	defer fake.Close()
	resp, err := http.Get(fake.URL() + "/v1/research/cache/get?hash=" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestLLMFakeCacheRoundTrip(t *testing.T) {
	hash := strings.Repeat("b", 64)
	fake := NewLLMFake(LLMFakeOptions{
		CacheSeed: map[string][]byte{hash: []byte(`{"hello":"world"}`)},
	})
	defer fake.Close()
	resp, err := http.Get(fake.URL() + "/v1/research/cache/get?hash=" + hash)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(got), "hello") {
		t.Fatalf("expected seed data, got %s", got)
	}
}

func TestLLMFakeCacheSet(t *testing.T) {
	fake := NewLLMFake(LLMFakeOptions{})
	defer fake.Close()
	hash := strings.Repeat("c", 64)
	body := `{"hash":"` + hash + `","response":"{\"ok\":true}","ttl_secs":3600}`
	resp, err := http.Post(fake.URL()+"/v1/research/cache/set", "application/json",
		bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set status = %d, want 204", resp.StatusCode)
	}
	if got := fake.SeenCacheSets(); len(got) != 1 {
		t.Fatalf("expected 1 set, got %d", len(got))
	}
}

func TestLLMFakeAuditEmit(t *testing.T) {
	fake := NewLLMFake(LLMFakeOptions{})
	defer fake.Close()
	resp, err := http.Post(fake.URL()+"/v1/audit/emit", "application/json",
		bytes.NewReader([]byte(`{"type":"x","payload":{}}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("emit status = %d, want 204", resp.StatusCode)
	}
	if got := fake.SeenAuditEmits(); len(got) != 1 {
		t.Fatalf("expected 1 emit, got %d", len(got))
	}
}

func TestLLMFakeAuditEmitFails(t *testing.T) {
	fake := NewLLMFake(LLMFakeOptions{FailEmitWithStatus: 503})
	defer fake.Close()
	resp, err := http.Post(fake.URL()+"/v1/audit/emit", "application/json",
		bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestLLMFakeBudgetRecord(t *testing.T) {
	fake := NewLLMFake(LLMFakeOptions{})
	defer fake.Close()
	resp, err := http.Post(fake.URL()+"/v1/budget/record", "application/json",
		bytes.NewReader([]byte(`{"cost_id":"x","amount_usd":0.01}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("record status = %d, want 204", resp.StatusCode)
	}
	if got := fake.SeenBudgetRecords(); len(got) != 1 {
		t.Fatalf("expected 1 record, got %d", len(got))
	}
}

func TestLLMFakeSeenMessages(t *testing.T) {
	fake := NewLLMFake(LLMFakeOptions{SynthesizeText: "ok"})
	defer fake.Close()
	body := `{"messages":[{"role":"user","content":"hello"}]}`
	if _, err := http.Post(fake.URL()+"/v1/messages", "application/json",
		bytes.NewReader([]byte(body))); err != nil {
		t.Fatal(err)
	}
	if got := fake.SeenMessages(); len(got) != 1 {
		t.Fatalf("expected 1 seen message, got %d", len(got))
	}
}
