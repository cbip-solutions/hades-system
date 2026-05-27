// go:build integration && cgo

package ecosystem_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func TestJinaCodeEmbeddings_CrossPackage_Shim(t *testing.T) {
	scriptPath := findRepoScriptPath(t)
	emb, err := ecosystem.NewJinaCodeEmbeddings(ecosystem.JinaCodeEmbeddingsOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		ShimMode:   true,
		BatchSize:  8,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings cross-package: %v", err)
	}
	defer emb.Close()

	var iface ecosystem.Embedder = emb

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bin, fp32, err := iface.EmbedBoth(ctx, "fn main() { println!(\"hi\"); }")
	if err != nil {
		t.Fatalf("EmbedBoth via interface: %v", err)
	}
	if len(bin) != 32 {
		t.Errorf("bin len=%d, want 32", len(bin))
	}
	if len(fp32) != 1536 {
		t.Errorf("fp32 len=%d, want 1536", len(fp32))
	}
}

func TestJinaCodeEmbeddings_CrossPackage_Batch_Shim(t *testing.T) {
	scriptPath := findRepoScriptPath(t)
	emb, err := ecosystem.NewJinaCodeEmbeddings(ecosystem.JinaCodeEmbeddingsOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		ShimMode:   true,
		BatchSize:  3,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings: %v", err)
	}
	defer emb.Close()

	texts := []string{"a", "b", "c", "d", "e", "f", "g"}
	bins, fp32s, err := emb.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(bins) != len(texts) {
		t.Fatalf("bins len=%d, want %d", len(bins), len(texts))
	}
	if len(fp32s) != len(texts) {
		t.Fatalf("fp32s len=%d, want %d", len(fp32s), len(texts))
	}
	for i, b := range bins {
		if len(b) != 32 {
			t.Errorf("bins[%d] len=%d, want 32", i, len(b))
		}
	}
}

func TestJinaCodeEmbeddings_CrossPackage_RealModel(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	if err := exec.Command("python3", "-c", "import sentence_transformers").Run(); err != nil {
		t.Skip("sentence-transformers not installed")
	}
	scriptPath := findRepoScriptPath(t)
	emb, err := ecosystem.NewJinaCodeEmbeddings(ecosystem.JinaCodeEmbeddingsOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		ShimMode:   false,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings real: %v", err)
	}
	defer emb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	bin, _, err := emb.EmbedBoth(ctx, "real model probe")
	if err != nil {
		t.Fatalf("EmbedBoth real: %v", err)
	}
	if len(bin) != 32 {
		t.Errorf("real bin len=%d, want 32", len(bin))
	}
}

type httpServerForwarder struct {
	server *httptest.Server
	token  string
	calls  int64
}

func (f *httpServerForwarder) Forward(ctx context.Context, body []byte) ([]byte, error) {
	atomic.AddInt64(&f.calls, 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.server.URL+"/v1/embeddings", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {

		return nil, &ecosystem.VoyageHTTPError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	return respBody, nil
}

type integFakeKeychain struct {
	token string
	err   error
}

func (f integFakeKeychain) GetGenericPassword(_, _ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.token, nil
}

type integVoyageReq struct {
	Input           []string `json:"input"`
	Model           string   `json:"model"`
	OutputDimension int      `json:"output_dimension"`
	OutputDtype     string   `json:"output_dtype"`
	InputType       string   `json:"input_type"`
}

func makeIntegVoyageData(n, dim int) []map[string]interface{} {
	out := make([]map[string]interface{}, n)
	for i := range out {
		emb := make([]float32, dim)
		for j := range emb {
			emb[j] = float32((i+j)%200-100) / 100.0
		}
		out[i] = map[string]interface{}{
			"object":    "embedding",
			"embedding": emb,
			"index":     i,
		}
	}
	return out
}

func TestVoyageCode3_Integration_HappyPath_EmbedBoth(t *testing.T) {
	var lastReq integVoyageReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.Error(w, "404", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
			http.Error(w, "bad content-type", http.StatusBadRequest)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&lastReq); err != nil {
			http.Error(w, "decode: "+err.Error(), http.StatusBadRequest)
			return
		}
		resp := map[string]interface{}{
			"object": "list",
			"model":  "voyage-code-3",
			"data":   makeIntegVoyageData(len(lastReq.Input), lastReq.OutputDimension),
			"usage":  map[string]int{"total_tokens": 100 * len(lastReq.Input)},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	fwd := &httpServerForwarder{server: srv, token: "test-token"}
	v, err := ecosystem.NewVoyageCode3(ecosystem.VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       integFakeKeychain{token: "test-token"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()

	var iface ecosystem.Embedder = v

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bin, fp32, err := iface.EmbedBoth(ctx, "func main() { println(\"hi\") }")
	if err != nil {
		t.Fatalf("EmbedBoth: %v", err)
	}
	if len(bin) != 32 {
		t.Errorf("bin len=%d, want 32", len(bin))
	}
	if len(fp32) != 1536 {
		t.Errorf("fp32 len=%d, want 1536", len(fp32))
	}
	if lastReq.Model != "voyage-code-3" {
		t.Errorf("server saw model=%q, want voyage-code-3", lastReq.Model)
	}
	if lastReq.OutputDimension != 1536 {
		t.Errorf("server saw output_dimension=%d, want 1536", lastReq.OutputDimension)
	}
	if lastReq.OutputDtype != "float" {
		t.Errorf("server saw output_dtype=%q, want float", lastReq.OutputDtype)
	}
	if lastReq.InputType != "document" {
		t.Errorf("server saw input_type=%q, want document", lastReq.InputType)
	}
	if got := atomic.LoadInt64(&fwd.calls); got != 1 {
		t.Errorf("Forwarder calls=%d, want 1 (EmbedBoth single round-trip)", got)
	}
}

func TestVoyageCode3_Integration_EmbedBatch_MultiRoundTrip(t *testing.T) {
	var seenInputs [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req integVoyageReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		seenInputs = append(seenInputs, append([]string(nil), req.Input...))
		resp := map[string]interface{}{
			"object": "list",
			"model":  "voyage-code-3",
			"data":   makeIntegVoyageData(len(req.Input), req.OutputDimension),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	fwd := &httpServerForwarder{server: srv, token: "test-token"}
	v, err := ecosystem.NewVoyageCode3(ecosystem.VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       integFakeKeychain{token: "test-token"},
		EnableFallback: true,
		BatchSize:      2,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bins, fp32s, err := v.EmbedBatch(ctx, []string{"a", "b", "c", "d", "e"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(bins) != 5 || len(fp32s) != 5 {
		t.Errorf("bins=%d fp32s=%d, want 5 each", len(bins), len(fp32s))
	}
	if len(seenInputs) != 3 {
		t.Errorf("server batches=%d, want 3 (ceil(5/2))", len(seenInputs))
	}
	if got := atomic.LoadInt64(&fwd.calls); got != 3 {
		t.Errorf("Forwarder calls=%d, want 3", got)
	}
}

func TestVoyageCode3_Integration_FallbackDisabledShortcuts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server hit with EnableFallback=false; should never happen")
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()
	fwd := &httpServerForwarder{server: srv, token: "test-token"}
	v, err := ecosystem.NewVoyageCode3(ecosystem.VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       integFakeKeychain{token: "test-token"},
		EnableFallback: false,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	if _, err := v.EmbedBinary256d(context.Background(), "x"); err == nil {
		t.Fatal("expected ErrFallbackDisabled, got nil")
	}
	if got := atomic.LoadInt64(&fwd.calls); got != 0 {
		t.Errorf("Forwarder.Forward called %d times under fallback-disabled; want 0", got)
	}
}

func TestVoyageCode3_Integration_AuthRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	fwd := &httpServerForwarder{server: srv, token: "wrong-token"}
	v, err := ecosystem.NewVoyageCode3(ecosystem.VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       integFakeKeychain{token: "wrong-token"},
		EnableFallback: true,
		MaxRetries:     0,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	_, err = v.EmbedBinary256d(context.Background(), "x")
	if err == nil {
		t.Fatal("expected 401 error, got nil")
	}
	if got := atomic.LoadInt64(&fwd.calls); got != 1 {
		t.Errorf("Forwarder calls=%d, want 1 (no retry on 401)", got)
	}
}

func findRepoScriptPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	root := wd
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(root, "internal", "research", "ecosystem", "scripts", "zen_jina_embed.py")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	t.Fatalf("could not locate scripts/zen_jina_embed.py from %s", wd)
	return ""
}
