// internal/orchestrator/apply/merge_engine_skip_test.go
//
// No build tag — runs in every test invocation (default + `-tags test`).
// Documents the canonical t.Skip pattern that + + future
// test suites MUST adopt for any scenario requiring real
// MergeEngine. Visible in the default build because the documentation
// (and the discipline of skipping cross-worker tests) applies even when
// the fake is not compiled in.
package apply_test

import "testing"

// TestMergeEngine_CrossWorker_NotYetShipped is the canonical skip-pattern
// reference. Any test that would normally exercise a real cross-worker
// merge winner-decision MUST guard with the same message —
// first commit greps for it and removes the guards once
// MergeEngineRealEngine is wired.
//
// In the default build (no -tags test), MergeEngineFake is not
// compiled in, so even the type reference here would not link. We
// therefore keep this test deliberately bare: it documents the pattern
// without depending on the fake.
func TestMergeEngine_CrossWorker_NotYetShipped(t *testing.T) {
	t.Skip("Plan 6 not yet shipped — MergeEngine fake-only mode")
}
