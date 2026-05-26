// tests/compliance/inv_zen_090_substrate_separation_test.go
//
// Compliance gate for inv-zen-090: bidirectional substrate separation
// between Plan 5's event log (state record) and Plan 4's workforce
// queue (transient messaging) per Q5 C hybrid (Plan 4 = messages,
//
// Two scans, both compile-time:
//  1. internal/orchestrator/eventlog/... MUST NOT import any package
//     under internal/workforce/queue/
//  2. internal/workforce/queue/... MUST NOT import any package under
//     internal/orchestrator/eventlog/
//
// Mixing the two collapses the two-substrate hybrid (Q5 C) into the
// rejected option B (single-table eventlog-as-queue) and would make
// the eventlog's append-only durability story leak into a transient
// messaging surface that is allowed to drop / TTL / re-order.
//
// Implementation: shells out to `go list -deps -json` against each
// subtree, decodes the package stream, and asserts neither subtree
// names the other in its Imports or Deps closure.
package compliance

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const (
	eventlogPrefix = "github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	queuePrefix    = "github.com/cbip-solutions/hades-system/internal/workforce/queue"
)

func TestInvZen090EventlogQueueSeparation(t *testing.T) {
	root := repoRoot(t)

	t.Run("eventlog_does_not_import_queue", func(t *testing.T) {
		pkgs := importsUnderPrefix(t, root, "./internal/orchestrator/eventlog/...", eventlogPrefix)
		if len(pkgs) == 0 {
			t.Fatalf("inv-zen-090 scan inconclusive: 0 eventlog packages observed (sentinel)")
		}
		for path, deps := range pkgs {
			for _, d := range deps {
				if isUnderPrefix(d, queuePrefix) {
					t.Errorf("inv-zen-090 VIOLATED: %s imports %s (eventlog must not see workforce/queue)", path, d)
				}
			}
		}
	})

	t.Run("queue_does_not_import_eventlog", func(t *testing.T) {
		pkgs := importsUnderPrefix(t, root, "./internal/workforce/queue/...", queuePrefix)
		if len(pkgs) == 0 {
			t.Fatalf("inv-zen-090 scan inconclusive: 0 queue packages observed (sentinel)")
		}
		for path, deps := range pkgs {
			for _, d := range deps {
				if isUnderPrefix(d, eventlogPrefix) {
					t.Errorf("inv-zen-090 VIOLATED (reverse): %s imports %s (workforce/queue must not see eventlog)", path, d)
				}
			}
		}
	})
}

func importsUnderPrefix(t *testing.T, root, pattern, filterPrefix string) map[string][]string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-json", pattern)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {

		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		t.Fatalf("go list -deps -json %s: %v\nstderr: %s", pattern, err, stderr)
	}
	res := map[string][]string{}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var pkg struct {
			ImportPath string
			Imports    []string
			Deps       []string
		}
		if err := dec.Decode(&pkg); err != nil {
			t.Fatalf("decode go list output for %s: %v", pattern, err)
		}
		if !isUnderPrefix(pkg.ImportPath, filterPrefix) {
			continue
		}
		all := make([]string, 0, len(pkg.Imports)+len(pkg.Deps))
		all = append(all, pkg.Imports...)
		all = append(all, pkg.Deps...)
		res[pkg.ImportPath] = all
	}
	return res
}

func isUnderPrefix(dep, prefix string) bool {
	if dep == prefix {
		return true
	}
	return strings.HasPrefix(dep, prefix+"/")
}
