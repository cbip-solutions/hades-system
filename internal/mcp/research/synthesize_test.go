package research

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSynthesizerHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "msg_001",
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{{
				"type": "text",
				"text": "Synthesis of findings.",
			}},
		})
	}))
	defer srv.Close()
	s := NewSynthesizer(SynthesizerOptions{
		DaemonURL: srv.URL,
	})
	out, err := s.Synthesize(context.Background(), SynthesizeInput{
		RawFindings: []any{map[string]any{"url": "https://x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Report, "Synthesis") {
		t.Errorf("report = %q", out.Report)
	}
}

func TestSynthesizerNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", 503)
	}))
	defer srv.Close()
	s := NewSynthesizer(SynthesizerOptions{DaemonURL: srv.URL})
	_, err := s.Synthesize(context.Background(), SynthesizeInput{
		RawFindings: []any{map[string]any{"url": "https://placeholder.test/"}},
	})
	if err == nil {
		t.Fatal("expected 503 error")
	}
}

func TestSynthesizerAuthHeader(t *testing.T) {
	gotAuth := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
		})
	}))
	defer srv.Close()
	s := NewSynthesizer(SynthesizerOptions{
		DaemonURL: srv.URL,
		AuthToken: "test-token",
	})
	if _, err := s.Synthesize(context.Background(), SynthesizeInput{RawFindings: []any{map[string]any{"url": "https://placeholder.test/"}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotAuth, "test-token") {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestSynthesizerProfileHeader(t *testing.T) {
	gotProfile := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProfile = r.Header.Get("X-Zen-Profile")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
		})
	}))
	defer srv.Close()
	s := NewSynthesizer(SynthesizerOptions{DaemonURL: srv.URL})
	if _, err := s.Synthesize(context.Background(), SynthesizeInput{RawFindings: []any{map[string]any{"url": "https://placeholder.test/"}}}); err != nil {
		t.Fatal(err)
	}
	if gotProfile != "research-synthesize" {
		t.Errorf("profile = %q", gotProfile)
	}
}

func TestSynthesizerNoFindingsErrors(t *testing.T) {
	s := NewSynthesizer(SynthesizerOptions{DaemonURL: "http://unused"})
	if _, err := s.Synthesize(context.Background(), SynthesizeInput{}); err == nil {
		t.Fatal("expected nil findings error")
	}
}

func TestSynthesizerNoDaemonURLErrors(t *testing.T) {
	s := NewSynthesizer(SynthesizerOptions{})
	if _, err := s.Synthesize(context.Background(), SynthesizeInput{RawFindings: []any{map[string]any{"url": "https://placeholder.test/"}}}); err == nil {
		t.Fatal("expected DaemonURL error")
	}
}

func TestSynthesizerStructuredJSONExtractsCitations(t *testing.T) {
	body := "Per the sources, X is true.\n\n```json\n{\"citations\":[{\"source_id\":\"s1\",\"url\":\"https://x\",\"title\":\"X\"}]}\n```"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": body}},
		})
	}))
	defer srv.Close()
	s := NewSynthesizer(SynthesizerOptions{DaemonURL: srv.URL})
	out, err := s.Synthesize(context.Background(), SynthesizeInput{RawFindings: []any{map[string]any{"url": "https://placeholder.test/"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Citations) != 1 {
		t.Fatalf("citations = %d, want 1", len(out.Citations))
	}
	if out.Citations[0].URL != "https://x" {
		t.Errorf("url = %q", out.Citations[0].URL)
	}
}

func TestSynthesizerCitationsBlockMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "no citations block here"}},
		})
	}))
	defer srv.Close()
	s := NewSynthesizer(SynthesizerOptions{DaemonURL: srv.URL})
	out, err := s.Synthesize(context.Background(), SynthesizeInput{RawFindings: []any{map[string]any{"url": "https://placeholder.test/"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Citations) != 0 {
		t.Errorf("expected 0 citations, got %d", len(out.Citations))
	}
	if out.Report == "" {
		t.Errorf("expected report present")
	}
}

func TestSynthesizerEmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"content":[]}`))
	}))
	defer srv.Close()
	s := NewSynthesizer(SynthesizerOptions{DaemonURL: srv.URL})
	out, err := s.Synthesize(context.Background(), SynthesizeInput{RawFindings: []any{map[string]any{"url": "https://placeholder.test/"}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Report != "" {
		t.Errorf("report = %q", out.Report)
	}
}

func TestSynthesizerBadResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()
	s := NewSynthesizer(SynthesizerOptions{DaemonURL: srv.URL})
	if _, err := s.Synthesize(context.Background(), SynthesizeInput{RawFindings: []any{map[string]any{"url": "https://placeholder.test/"}}}); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestExtractCitationsFromTextDirect(t *testing.T) {
	in := "blah\n```json\n{\"citations\":[{\"source_id\":\"a\",\"url\":\"https://b\",\"title\":\"T\"}]}\n```\nmore"
	got := extractCitationsFromText(in)
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].URL != "https://b" {
		t.Errorf("url = %q", got[0].URL)
	}
}

func TestExtractCitationsFromTextNoMatch(t *testing.T) {
	if got := extractCitationsFromText("no fence here"); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestExtractCitationsFromTextBadJSON(t *testing.T) {
	in := "```json\n{\"citations\":not-json}\n```"
	if got := extractCitationsFromText(in); got != nil {
		t.Errorf("expected nil on parse error, got %v", got)
	}
}

func TestSynthesizerOverridePrompt(t *testing.T) {
	gotPrompt := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		body = body[:n]
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		if s, ok := parsed["system"].(string); ok {
			gotPrompt = s
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
		})
	}))
	defer srv.Close()
	s := NewSynthesizer(SynthesizerOptions{DaemonURL: srv.URL})
	if _, err := s.Synthesize(context.Background(), SynthesizeInput{
		RawFindings: []any{map[string]any{"url": "https://placeholder.test/"}},
		Prompt:      "CUSTOM PROMPT",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPrompt, "CUSTOM PROMPT") {
		t.Errorf("prompt = %q, want CUSTOM PROMPT", gotPrompt)
	}
}
