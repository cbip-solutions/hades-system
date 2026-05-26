// SPDX-License-Identifier: MIT
package boundary_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/hermes/boundary"
)

type fakePreHook struct {
	calls    int
	mutateTo string
	err      error
}

func (f *fakePreHook) PreCompletion(_ context.Context, req *boundary.CompletionRequest) error {
	f.calls++
	if f.mutateTo != "" {
		req.Prompt = f.mutateTo
	}
	return f.err
}

type fakePostHook struct {
	calls    int
	mutateTo string
	err      error
}

func (f *fakePostHook) PostCompletion(_ context.Context, _ boundary.CompletionRequest, resp *boundary.CompletionResponse) error {
	f.calls++
	if f.mutateTo != "" {
		resp.Text = f.mutateTo
	}
	return f.err
}

func TestPreCompletionHookContract(t *testing.T) {
	t.Parallel()
	hook := &fakePreHook{mutateTo: "augmented"}
	req := boundary.CompletionRequest{Prompt: "original"}
	err := hook.PreCompletion(context.Background(), &req)
	if err != nil {
		t.Fatalf("PreCompletion: %v", err)
	}
	if req.Prompt != "augmented" {
		t.Errorf("hook mutation lost; req.Prompt = %q", req.Prompt)
	}
	if hook.calls != 1 {
		t.Errorf("hook calls = %d; want 1", hook.calls)
	}
}

func TestPreCompletionHookError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("veto")
	hook := &fakePreHook{err: wantErr}
	err := hook.PreCompletion(context.Background(), &boundary.CompletionRequest{})
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wantErr; got %v", err)
	}
}

func TestPostCompletionHookContract(t *testing.T) {
	t.Parallel()
	hook := &fakePostHook{mutateTo: "redacted"}
	req := boundary.CompletionRequest{Prompt: "hi"}
	resp := boundary.CompletionResponse{Text: "original"}
	err := hook.PostCompletion(context.Background(), req, &resp)
	if err != nil {
		t.Fatalf("PostCompletion: %v", err)
	}
	if resp.Text != "redacted" {
		t.Errorf("post mutation lost; resp.Text = %q", resp.Text)
	}
	if hook.calls != 1 {
		t.Errorf("hook calls = %d; want 1", hook.calls)
	}
}

func TestHookChainPreCompletion(t *testing.T) {
	t.Parallel()
	h1 := &fakePreHook{mutateTo: "step1"}
	h2 := &fakePreHook{mutateTo: "step2"}
	chain := boundary.NewHookChain[boundary.PreCompletionHook]([]boundary.PreCompletionHook{h1, h2})

	if chain.Len() != 2 {
		t.Fatalf("chain.Len() = %d; want 2", chain.Len())
	}
	if len(chain.Hooks()) != 2 {
		t.Fatalf("chain.Hooks() len = %d; want 2", len(chain.Hooks()))
	}

	req := boundary.CompletionRequest{Prompt: "in"}
	for _, h := range chain.Hooks() {
		if err := h.PreCompletion(context.Background(), &req); err != nil {
			t.Fatalf("hook chain: %v", err)
		}
	}
	if req.Prompt != "step2" {
		t.Errorf("chain order lost; final req.Prompt = %q (want step2)", req.Prompt)
	}
}

func TestHookChainPostCompletion(t *testing.T) {
	t.Parallel()
	h1 := &fakePostHook{mutateTo: "stage1"}
	h2 := &fakePostHook{mutateTo: "stage2"}
	chain := boundary.NewHookChain[boundary.PostCompletionHook]([]boundary.PostCompletionHook{h1, h2})

	if chain.Len() != 2 {
		t.Fatalf("chain.Len() = %d; want 2", chain.Len())
	}

	resp := boundary.CompletionResponse{Text: "out"}
	for _, h := range chain.Hooks() {
		if err := h.PostCompletion(context.Background(), boundary.CompletionRequest{}, &resp); err != nil {
			t.Fatalf("post chain: %v", err)
		}
	}
	if resp.Text != "stage2" {
		t.Errorf("chain order lost; final resp.Text = %q (want stage2)", resp.Text)
	}
}

func TestHookChainEmpty(t *testing.T) {
	t.Parallel()
	chain := boundary.NewHookChain[boundary.PreCompletionHook](nil)
	if chain.Len() != 0 {
		t.Errorf("empty chain Len() = %d; want 0", chain.Len())
	}
	if hooks := chain.Hooks(); hooks != nil {
		t.Errorf("empty chain Hooks() = %v; want nil", hooks)
	}
}
