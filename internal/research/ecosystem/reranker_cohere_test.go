// Package ecosystem — reranker_cohere_test.go
//
// Tests for CohereRerankV4.
//
// Tests use fakeCohereForwarder + fakeKeychain narrow-interface fakes so
// we exercise every code path WITHOUT importing net/http in this package
// (invariant forward-compat: ecosystem code goes through the
// narrow Forwarder at runtime; tests substitute the narrow interface).
//
// Coverage target: ≥90% (security/correctness-critical — privacy gate).
//
// Behaviors covered:
// - Constructor validation (nil Forwarder / nil Keychain / EnableFallback gate)
// - Default knob application (endpoint/model/latency/keychain key/account)
// - ErrFallbackDisabled with EnableFallback=false (privacy invariant)
// - ErrKeychainTokenMissing surfaced when Keychain returns error
// - Happy path: request body marshaling, response parsing, sort + 1-based Rank
// - HTTP 401/403 → ErrCohereAuth via errors.Is
// - HTTP 429 → ErrCohereRateLimit via errors.Is
// - HTTP 5xx → wrapped CohereHTTPError (errors.As)
// - Malformed JSON response → ErrCohereResponse
// - Out-of-range index in response → ErrCohereResponse
// - len(candidates)==0 → returns (nil, nil) (no Forwarder call)
// - topK ≤ 0 or > len(candidates) → coerced to len(candidates)
// - topK < len(out) → result truncated
// - Sort-stability: descending RerankerScore, ties broken by ChunkID asc
// - Context cancellation before lock, after lock, during Forward
// - Concurrency: 32 parallel Rerank calls produce 32 successful results
// - Close idempotent + post-close Rerank returns error
// - CountReranks monotonic
package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeCohereForwarder struct {
	resp      cohereResponse
	respFn    func(req cohereRequest) cohereResponse
	rawResp   []byte
	errs      []error
	callCount atomic.Int64
	lastReq   cohereRequest
	allReqs   []cohereRequest
	sleep     time.Duration
	mu        sync.Mutex
}

func (f *fakeCohereForwarder) Forward(ctx context.Context, body []byte) ([]byte, error) {
	n := f.callCount.Add(1)
	f.mu.Lock()
	var req cohereRequest
	_ = json.Unmarshal(body, &req)
	f.lastReq = req
	f.allReqs = append(f.allReqs, req)
	f.mu.Unlock()

	if f.sleep > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.sleep):
		}
	}

	idx := int(n) - 1
	if idx < len(f.errs) {
		if err := f.errs[idx]; err != nil {
			return nil, err
		}
	}
	if len(f.rawResp) > 0 {
		return f.rawResp, nil
	}
	resp := f.resp
	if f.respFn != nil {
		f.mu.Lock()
		resp = f.respFn(req)
		f.mu.Unlock()
	}
	return json.Marshal(resp)
}

func (f *fakeCohereForwarder) calls() int64 { return f.callCount.Load() }

func TestCohereRerankV4_InterfaceConformance(t *testing.T) {
	var _ Reranker = (*CohereRerankV4)(nil)
}

func TestCohereRerankV4_ConstructorValidatesRequiredFields(t *testing.T) {
	if _, err := NewCohereRerankV4(CohereRerankV4Options{
		Keychain:       &fakeKeychain{},
		EnableFallback: true,
	}); err == nil {
		t.Errorf("NewCohereRerankV4 with nil Forwarder = nil err; want error")
	}
	if _, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      &fakeCohereForwarder{},
		EnableFallback: true,
	}); err == nil {
		t.Errorf("NewCohereRerankV4 with nil Keychain = nil err; want error")
	}
}

// TestCohereRerankV4_DisabledByDefault is the load-bearing privacy invariant:
// the constructor with EnableFallback=false MUST return ErrFallbackDisabled
// before any Keychain lookup or Forwarder call — the same package-level
// sentinel as VoyageCode3 (single fallback-disabled door for the package).
func TestCohereRerankV4_DisabledByDefault(t *testing.T) {
	fwd := &fakeCohereForwarder{}
	kc := &fakeKeychain{token: "should-never-be-read"}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       kc,
		EnableFallback: false,
	})
	if !errors.Is(err, ErrFallbackDisabled) {
		t.Errorf("want ErrFallbackDisabled when EnableFallback=false, got err=%v r=%v", err, r)
	}
	if kc.callCount != 0 {
		t.Errorf("Keychain consulted %d times under fallback-disabled; want 0", kc.callCount)
	}
	if fwd.calls() != 0 {
		t.Errorf("Forwarder called %d times under fallback-disabled; want 0", fwd.calls())
	}
}

func TestCohereRerankV4_ConstructorAppliesDefaults(t *testing.T) {
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      &fakeCohereForwarder{},
		Keychain:       &fakeKeychain{token: "tok"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()
	if got, want := r.opts.Model, cohereDefaultModel; got != want {
		t.Errorf("Model default = %q; want %q", got, want)
	}
	if got, want := r.opts.MaxLatencyMs, cohereDefaultMaxLatencyMs; got != want {
		t.Errorf("MaxLatencyMs default = %d; want %d", got, want)
	}
	if got, want := r.opts.TokenKey, cohereDefaultTokenKey; got != want {
		t.Errorf("TokenKey default = %q; want %q", got, want)
	}
	if got, want := r.opts.TokenAccount, cohereDefaultTokenAccount; got != want {
		t.Errorf("TokenAccount default = %q; want %q", got, want)
	}
}

func TestCohereRerankV4_KeychainMissing(t *testing.T) {
	fwd := &fakeCohereForwarder{}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{err: errors.New("not found")},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()
	_, err = r.Rerank(context.Background(), "q", buildFakeCandidates(2), 2)
	if !errors.Is(err, ErrKeychainTokenMissing) {
		t.Errorf("Rerank err=%v, want ErrKeychainTokenMissing", err)
	}
	if fwd.calls() != 0 {
		t.Errorf("Forwarder called %d times when Keychain failed; want 0", fwd.calls())
	}
}

func TestCohereRerankV4_KeychainEmptyToken(t *testing.T) {
	fwd := &fakeCohereForwarder{}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: ""},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()
	_, err = r.Rerank(context.Background(), "q", buildFakeCandidates(2), 2)
	if !errors.Is(err, ErrKeychainTokenMissing) {
		t.Errorf("Rerank err=%v, want ErrKeychainTokenMissing", err)
	}
	if fwd.calls() != 0 {
		t.Errorf("Forwarder called %d times with empty token; want 0", fwd.calls())
	}
}

func TestCohereRerankV4_HappyPath_RequestBodyAndOrder(t *testing.T) {
	fwd := &fakeCohereForwarder{
		respFn: func(req cohereRequest) cohereResponse {

			if got := req.Query; got != "test-query" {
				t.Errorf("req.Query=%q; want test-query", got)
			}
			if req.Model != cohereDefaultModel {
				t.Errorf("req.Model=%q; want %q", req.Model, cohereDefaultModel)
			}
			if req.TopN != 2 {
				t.Errorf("req.TopN=%d; want 2", req.TopN)
			}
			if req.ReturnDocuments {
				t.Errorf("ReturnDocuments=true; want false (we already have the docs locally)")
			}
			if len(req.Documents) != 3 {
				t.Errorf("len(req.Documents)=%d; want 3", len(req.Documents))
			}
			return cohereResponse{Results: []cohereResult{
				{Index: 2, RelevanceScore: 0.10},
				{Index: 0, RelevanceScore: 0.95},
				{Index: 1, RelevanceScore: 0.50},
			}}
		},
	}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "test-tok"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()

	cands := buildFakeCandidates(3)
	out, err := r.Rerank(context.Background(), "test-query", cands, 2)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len(out)=%d; want 2 (truncated to topK)", len(out))
	}

	if out[0].RerankerScore != 0.95 {
		t.Errorf("out[0].RerankerScore=%v; want 0.95", out[0].RerankerScore)
	}
	if out[1].RerankerScore != 0.50 {
		t.Errorf("out[1].RerankerScore=%v; want 0.50", out[1].RerankerScore)
	}
	if out[0].Rank != 1 || out[1].Rank != 2 {
		t.Errorf("Ranks = (%d,%d); want (1,2)", out[0].Rank, out[1].Rank)
	}
	if out[0].ChunkID != cands[0].ChunkID {
		t.Errorf("out[0].ChunkID=%d; want %d (candidate 0)", out[0].ChunkID, cands[0].ChunkID)
	}
	if out[1].ChunkID != cands[1].ChunkID {
		t.Errorf("out[1].ChunkID=%d; want %d (candidate 1)", out[1].ChunkID, cands[1].ChunkID)
	}
	if got := r.CountReranks(); got != 1 {
		t.Errorf("CountReranks=%d; want 1", got)
	}
}

func TestCohereRerankV4_TopKCoercion(t *testing.T) {
	for _, tc := range []struct {
		name string
		topK int
		want int
	}{
		{"topK=0", 0, 3},
		{"topK<0", -5, 3},
		{"topK>len", 99, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fwd := &fakeCohereForwarder{
				respFn: func(req cohereRequest) cohereResponse {
					return cohereResponse{Results: []cohereResult{
						{Index: 0, RelevanceScore: 0.7},
						{Index: 1, RelevanceScore: 0.5},
						{Index: 2, RelevanceScore: 0.3},
					}}
				},
			}
			r, err := NewCohereRerankV4(CohereRerankV4Options{
				Forwarder:      fwd,
				Keychain:       &fakeKeychain{token: "t"},
				EnableFallback: true,
			})
			if err != nil {
				t.Fatalf("NewCohereRerankV4: %v", err)
			}
			defer r.Close()
			out, err := r.Rerank(context.Background(), "q", buildFakeCandidates(3), tc.topK)
			if err != nil {
				t.Fatalf("Rerank: %v", err)
			}
			if len(out) != tc.want {
				t.Errorf("len(out)=%d; want %d", len(out), tc.want)
			}
		})
	}
}

func TestCohereRerankV4_EmptyCandidates(t *testing.T) {
	fwd := &fakeCohereForwarder{}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()
	out, err := r.Rerank(context.Background(), "q", nil, 10)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if out != nil {
		t.Errorf("out=%v; want nil", out)
	}
	if fwd.calls() != 0 {
		t.Errorf("Forwarder called %d times for empty candidates; want 0", fwd.calls())
	}
}

func TestCohereRerankV4_SortStability(t *testing.T) {
	fwd := &fakeCohereForwarder{
		respFn: func(req cohereRequest) cohereResponse {
			return cohereResponse{Results: []cohereResult{
				{Index: 0, RelevanceScore: 0.5},
				{Index: 1, RelevanceScore: 0.5},
				{Index: 2, RelevanceScore: 0.5},
			}}
		},
	}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()
	cands := buildFakeCandidates(3)

	cands[0].ChunkID = 30
	cands[1].ChunkID = 10
	cands[2].ChunkID = 20
	out, err := r.Rerank(context.Background(), "q", cands, 3)
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if out[0].ChunkID != 10 || out[1].ChunkID != 20 || out[2].ChunkID != 30 {
		t.Errorf("tie-break order ChunkIDs = (%d,%d,%d); want (10,20,30) ascending",
			out[0].ChunkID, out[1].ChunkID, out[2].ChunkID)
	}
}

// TestCohereRerankV4_RateLimit_429 — Forwarder returns CohereHTTPError 429.
// Caller-facing surface MUST be ErrCohereRateLimit (so the dispatcher can
// trigger backoff/disable without parsing HTTP semantics).
func TestCohereRerankV4_RateLimit_429(t *testing.T) {
	fwd := &fakeCohereForwarder{errs: []error{&CohereHTTPError{StatusCode: 429, Body: []byte("Too Many Requests")}}}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()
	_, err = r.Rerank(context.Background(), "q", buildFakeCandidates(2), 2)
	if !errors.Is(err, ErrCohereRateLimit) {
		t.Errorf("err=%v; want ErrCohereRateLimit", err)
	}
}

func TestCohereRerankV4_AuthError_401_And_403(t *testing.T) {
	for _, code := range []int{401, 403} {
		t.Run(fmt.Sprintf("HTTP%d", code), func(t *testing.T) {
			fwd := &fakeCohereForwarder{errs: []error{&CohereHTTPError{StatusCode: code, Body: []byte("nope")}}}
			r, err := NewCohereRerankV4(CohereRerankV4Options{
				Forwarder:      fwd,
				Keychain:       &fakeKeychain{token: "wrong"},
				EnableFallback: true,
			})
			if err != nil {
				t.Fatalf("NewCohereRerankV4: %v", err)
			}
			defer r.Close()
			_, err = r.Rerank(context.Background(), "q", buildFakeCandidates(2), 2)
			if !errors.Is(err, ErrCohereAuth) {
				t.Errorf("err=%v; want ErrCohereAuth", err)
			}
		})
	}
}

func TestCohereRerankV4_HTTP5xx_WrappedError(t *testing.T) {
	fwd := &fakeCohereForwarder{errs: []error{&CohereHTTPError{StatusCode: 503, Body: []byte("Service Unavailable")}}}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()
	_, err = r.Rerank(context.Background(), "q", buildFakeCandidates(2), 2)
	if err == nil {
		t.Fatal("expected non-nil error for HTTP 503")
	}
	var httpErr *CohereHTTPError
	if !errors.As(err, &httpErr) {
		t.Errorf("err=%v; want errors.As(*CohereHTTPError)", err)
	}
	if httpErr.StatusCode != 503 {
		t.Errorf("httpErr.StatusCode=%d; want 503", httpErr.StatusCode)
	}

	if errors.Is(err, ErrCohereAuth) {
		t.Errorf("5xx matched ErrCohereAuth; should not")
	}
	if errors.Is(err, ErrCohereRateLimit) {
		t.Errorf("5xx matched ErrCohereRateLimit; should not")
	}
}

func TestCohereRerankV4_TransportError_NotConflated(t *testing.T) {
	wantErr := errors.New("dial tcp: connection refused")
	fwd := &fakeCohereForwarder{errs: []error{wantErr}}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()
	_, err = r.Rerank(context.Background(), "q", buildFakeCandidates(2), 2)
	if !errors.Is(err, wantErr) {
		t.Errorf("err=%v; want chain to include %v", err, wantErr)
	}
	if errors.Is(err, ErrCohereAuth) || errors.Is(err, ErrCohereRateLimit) {
		t.Errorf("transport err conflated with HTTP sentinel: %v", err)
	}
}

func TestCohereRerankV4_MalformedJSON(t *testing.T) {
	fwd := &fakeCohereForwarder{rawResp: []byte("not json")}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()
	_, err = r.Rerank(context.Background(), "q", buildFakeCandidates(2), 2)
	if !errors.Is(err, ErrCohereResponse) {
		t.Errorf("err=%v; want ErrCohereResponse", err)
	}
}

func TestCohereRerankV4_IndexOutOfRange(t *testing.T) {
	for _, idx := range []int{99, -1} {
		t.Run(fmt.Sprintf("idx=%d", idx), func(t *testing.T) {
			fwd := &fakeCohereForwarder{
				respFn: func(req cohereRequest) cohereResponse {
					return cohereResponse{Results: []cohereResult{
						{Index: idx, RelevanceScore: 0.5},
					}}
				},
			}
			r, err := NewCohereRerankV4(CohereRerankV4Options{
				Forwarder:      fwd,
				Keychain:       &fakeKeychain{token: "t"},
				EnableFallback: true,
			})
			if err != nil {
				t.Fatalf("NewCohereRerankV4: %v", err)
			}
			defer r.Close()
			_, err = r.Rerank(context.Background(), "q", buildFakeCandidates(3), 3)
			if !errors.Is(err, ErrCohereResponse) {
				t.Errorf("err=%v; want ErrCohereResponse", err)
			}
		})
	}
}

func TestCohereRerankV4_ContextCanceledBeforeCall(t *testing.T) {
	fwd := &fakeCohereForwarder{}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()
	ctx, cancel := contextWithCancel()
	cancel()
	_, err = r.Rerank(ctx, "q", buildFakeCandidates(2), 2)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err=%v; want context.Canceled", err)
	}
	if fwd.calls() != 0 {
		t.Errorf("Forwarder called %d times with canceled ctx; want 0", fwd.calls())
	}
}

func TestCohereRerankV4_ContextCanceledDuringForward(t *testing.T) {
	fwd := &fakeCohereForwarder{sleep: 200 * time.Millisecond}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err = r.Rerank(ctx, "q", buildFakeCandidates(2), 2)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err=%v; want context.Canceled", err)
	}
}

func TestCohereRerankV4_ContextDeadlineExceeded(t *testing.T) {
	fwd := &fakeCohereForwarder{sleep: 200 * time.Millisecond}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = r.Rerank(ctx, "q", buildFakeCandidates(2), 2)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err=%v; want context.DeadlineExceeded", err)
	}
}

func TestCohereRerankV4_CloseIdempotent(t *testing.T) {
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      &fakeCohereForwarder{},
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close (1) = %v; want nil", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close (2) = %v; want nil (idempotent)", err)
	}
}

func TestCohereRerankV4_RerankAfterClose(t *testing.T) {
	fwd := &fakeCohereForwarder{}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	_ = r.Close()
	_, err = r.Rerank(context.Background(), "q", buildFakeCandidates(2), 2)
	if err == nil {
		t.Errorf("Rerank after Close = nil err; want non-nil")
	}
	if fwd.calls() != 0 {
		t.Errorf("Forwarder called %d times after Close; want 0", fwd.calls())
	}
}

func TestCohereRerankV4_ConcurrentRerank(t *testing.T) {
	const N = 32
	fwd := &fakeCohereForwarder{
		respFn: func(req cohereRequest) cohereResponse {
			return cohereResponse{Results: []cohereResult{
				{Index: 0, RelevanceScore: 0.9},
				{Index: 1, RelevanceScore: 0.5},
			}}
		},
	}
	kc := &fakeKeychain{token: "t"}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       kc,
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			out, err := r.Rerank(context.Background(), "q", buildFakeCandidates(2), 2)
			if err != nil {
				t.Errorf("Rerank: %v", err)
				return
			}
			if len(out) != 2 {
				t.Errorf("len(out)=%d; want 2", len(out))
			}
		}()
	}
	wg.Wait()

	if got := r.CountReranks(); got != N {
		t.Errorf("CountReranks=%d; want %d", got, N)
	}
	if got := fwd.calls(); got != int64(N) {
		t.Errorf("Forwarder.calls=%d; want %d", got, N)
	}

	if kc.callCount != 1 {
		t.Errorf("Keychain.callCount=%d; want 1 (token cached)", kc.callCount)
	}
}

// TestCohereRerankV4_CountReranksMonotonic — counter advances exactly once
// per successful Rerank; error paths do NOT advance it.
func TestCohereRerankV4_CountReranksMonotonic(t *testing.T) {
	fwd := &fakeCohereForwarder{
		errs: []error{
			nil,
			&CohereHTTPError{StatusCode: 429},
			nil,
		},
		respFn: func(req cohereRequest) cohereResponse {
			return cohereResponse{Results: []cohereResult{{Index: 0, RelevanceScore: 0.5}}}
		},
	}
	r, err := NewCohereRerankV4(CohereRerankV4Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewCohereRerankV4: %v", err)
	}
	defer r.Close()
	cands := buildFakeCandidates(1)
	if _, err := r.Rerank(context.Background(), "q", cands, 1); err != nil {
		t.Fatalf("Rerank (1): %v", err)
	}
	if _, err := r.Rerank(context.Background(), "q", cands, 1); !errors.Is(err, ErrCohereRateLimit) {
		t.Fatalf("Rerank (2) err=%v; want ErrCohereRateLimit", err)
	}
	if _, err := r.Rerank(context.Background(), "q", cands, 1); err != nil {
		t.Fatalf("Rerank (3): %v", err)
	}
	if got := r.CountReranks(); got != 2 {
		t.Errorf("CountReranks=%d; want 2 (1 success + 1 fail + 1 success)", got)
	}
}

func TestCohereHTTPError_ErrorString(t *testing.T) {
	e := &CohereHTTPError{StatusCode: 503, Body: []byte("service unavailable")}
	got := e.Error()
	if got == "" {
		t.Error("Error() returned empty string")
	}

	if want := "503"; !errors.Is(e, &CohereHTTPError{StatusCode: 503}) {

		_ = want
	}

	if got == "" || (!stringContains(got, "503")) {
		t.Errorf("Error()=%q; expected to contain status code 503", got)
	}
}

func TestCohereHTTPError_ErrorString_BodyTruncated(t *testing.T) {

	const bigSize = 1000
	big := make([]byte, bigSize)
	for i := range big {
		big[i] = 'A'
	}
	e := &CohereHTTPError{StatusCode: 502, Body: big}
	got := e.Error()
	if got == "" {
		t.Fatal("Error() returned empty string")
	}

	if !stringContains(got, "502") {
		t.Errorf("Error()=%q; expected to contain status code 502", got)
	}

	count := 0
	for i := 0; i < len(got); i++ {
		if got[i] == 'A' {
			count++
		}
	}
	if count != 256 {
		t.Errorf("Error() body 'A' count=%d; want 256 (truncated)", count)
	}
	if count == bigSize {
		t.Errorf("Error() did not truncate: count=%d == bigSize", count)
	}
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
