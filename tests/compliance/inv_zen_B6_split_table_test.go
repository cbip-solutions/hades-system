package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootB6(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for !fileExistsB6(filepath.Join(root, "go.mod")) {
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("could not find go.mod root")
		}
		root = parent
	}
	return root
}

func TestInvZenB6_SplitTablePrivateInvariantsAbsent(t *testing.T) {
	root := repoRootB6(t)

	fullyMigrated := []string{"053", "054", "055", "058", "059", "060", "071", "243", "247"}
	compRoot := filepath.Join(root, "tests", "compliance")
	for _, inv := range fullyMigrated {
		matches, _ := filepath.Glob(filepath.Join(compRoot, "inv_zen_"+inv+"_*.go"))
		if len(matches) != 0 {
			t.Errorf("inv-zen-%s still present in public-snapshot perimeter at %v; must move to private zen-bypass-tier1 per decisión 17-a extended", inv, matches)
		}
	}

	devRetained := map[string]string{
		"242": "inv_zen_242_244_dev_repo_fingerprint",
		"246": "inv_zen_246_bootstrap_wires_munger",
	}
	for inv, allowedPrefix := range devRetained {
		matches, _ := filepath.Glob(filepath.Join(compRoot, "inv_zen_"+inv+"_*.go"))

		if len(matches) == 0 {
			t.Errorf("inv-zen-%s dev-repo fingerprint file missing (expected prefix %s_*); W7-B2 commit 592bebaa specified the dev-side scope-reduction residual", inv, allowedPrefix)
			continue
		}
		for _, m := range matches {
			base := filepath.Base(m)
			if !strings.HasPrefix(base, allowedPrefix) {
				t.Errorf("inv-zen-%s residual file %s does not match expected W7-B2 prefix %s_*; unexpected file in public-snapshot perimeter", inv, base, allowedPrefix)
			}
		}
	}

	inv244Matches, _ := filepath.Glob(filepath.Join(compRoot, "inv_zen_244_*.go"))
	for _, m := range inv244Matches {
		base := filepath.Base(m)
		if !strings.HasPrefix(base, "inv_zen_244_") {
			continue
		}

		t.Errorf("inv-zen-244 standalone file %s present; expected ONLY transitive coverage via inv_zen_242_244_dev_repo_fingerprint per W7-B2", base)
	}
}

func TestInvZenB6_SplitTablePublicInvariantsPresent(t *testing.T) {
	root := repoRootB6(t)
	bypassFile := filepath.Join(root, "tests", "compliance", "bypass_invariants_test.go")
	body, err := os.ReadFile(bypassFile)
	if err != nil {
		t.Fatalf("bypass_invariants_test.go missing (must host public invariants 051/052/056/057/061 per decisión 17-a): %v", err)
	}
	bodyStr := string(body)
	publicInvariants := []string{"051", "052", "056", "057"}
	for _, inv := range publicInvariants {

		markers := []string{
			"inv-zen-" + inv,
			"TestInvZen" + inv,
		}
		var found bool
		for _, m := range markers {
			if strings.Contains(bodyStr, m) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("inv-zen-%s missing from public-snapshot perimeter at bypass_invariants_test.go; must stay public per decisión 17-a (markers checked: %v)", inv, markers)
		}
	}
}

func TestInvZenB6_BoundaryScanPresent(t *testing.T) {
	root := repoRootB6(t)
	bypassFile := filepath.Join(root, "tests", "compliance", "bypass_invariants_test.go")
	body, err := os.ReadFile(bypassFile)
	if err != nil {
		t.Fatalf("bypass_invariants_test.go missing (must host public inv-zen-061 boundary scan per decisión 17-a): %v", err)
	}
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "inv-zen-061") {
		t.Error("inv-zen-061 boundary scan missing from bypass_invariants_test.go; required PUBLIC per decisión 17-a (commit hook = BOTH repos, boundary scan = PUBLIC)")
	}

	if !strings.Contains(bodyStr, "pre-commit-bypass-token-scan") {
		t.Error("inv-zen-061 boundary-scan substance missing in bypass_invariants_test.go: no reference to .githooks/pre-commit-bypass-token-scan")
	}
}

func TestInvZenB6_HookInstalled(t *testing.T) {
	root := repoRootB6(t)
	hook := filepath.Join(root, ".githooks", "pre-commit-bypass-token-scan")
	info, err := os.Stat(hook)
	if err != nil {
		t.Fatalf("hook missing: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("hook not executable")
	}
	body, _ := os.ReadFile(hook)
	if !strings.Contains(string(body), "sk-ant") {
		t.Error("hook missing sk-ant pattern (token-scan substance check)")
	}
}

func fileExistsB6(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
