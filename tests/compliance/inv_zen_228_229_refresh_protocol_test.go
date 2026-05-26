package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootRefresh(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("inv-zen-228/229: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("inv-zen-228/229: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

func readRefreshSrc(t *testing.T, root string, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		t.Fatalf("inv-zen-228/229: reading %v: %v", parts, err)
	}
	return string(b)
}

func TestInvZen228_RefreshUsesTokenEndpointWithClientID(t *testing.T) {
	root := repoRootRefresh(t)
	src := readRefreshSrc(t, root, "internal", "anthropic-bypass", "refresh.go")

	if !strings.Contains(src, `"https://console.anthropic.com/v1/oauth/token"`) {
		t.Error("inv-zen-228 VIOLATED: refresh.go does not target the /v1/oauth/token endpoint — a stale token never refreshes (the v0.17.8 429 root cause); the legacy /oauth/refresh path 404s.")
	}
	if !strings.Contains(src, "9d1c250a-e61b-44d9-88ed-5944d1962f5e") {
		t.Error("inv-zen-228 VIOLATED: refresh.go is missing the public Claude Code OAuth client_id — the token endpoint rejects a refresh body without it (400 'Invalid request format').")
	}
	if !strings.Contains(src, `"client_id"`) {
		t.Error("inv-zen-228 VIOLATED: the refresh request body does not include the client_id field.")
	}
}

func TestInvZen229_RefreshNowHandlerNotStub(t *testing.T) {
	root := repoRootRefresh(t)
	src := readRefreshSrc(t, root, "internal", "daemon", "handlers", "bypass.go")
	body, found := extractFuncBodyRefresh(src, "func BypassRefreshNow")
	if !found {
		t.Fatal("inv-zen-229: BypassRefreshNow not found in internal/daemon/handlers/bypass.go")
	}
	if !strings.Contains(body, "RefreshNow(") {
		t.Error("inv-zen-229 VIOLATED: BypassRefreshNow does not call the client's RefreshNow — it is a no-op stub (the doctor's `zen bypass refresh-now` would do nothing).")
	}
	if strings.Contains(body, "in-process scheduler") {
		t.Error("inv-zen-229 VIOLATED: BypassRefreshNow still returns the stub {ok:true, expires_in:in-process scheduler} lie.")
	}
}

func TestInvZen229_TierSwitchNotifiesWarnNotCritical(t *testing.T) {
	root := repoRootRefresh(t)

	notifSrc := readRefreshSrc(t, root, "internal", "daemon", "notifications.go")
	body, found := extractFuncBodyRefresh(notifSrc, "func (n *Notifier) OnTierSwitch")
	if !found {
		t.Fatal("inv-zen-229: Notifier.OnTierSwitch not found in internal/daemon/notifications.go")
	}
	if !strings.Contains(body, `"WARN"`) {
		t.Error("inv-zen-229 VIOLATED: Notifier.OnTierSwitch does not dispatch WARN — a CRITICAL tier-switch re-fires hourly via runRepeatLoop (the [REPEAT] storm).")
	}
	if strings.Contains(body, `"CRITICAL"`) {
		t.Error("inv-zen-229 VIOLATED: Notifier.OnTierSwitch dispatches CRITICAL (a routine failover must be WARN).")
	}

	bootSrc := readRefreshSrc(t, root, "cmd", "zen-swarm-ctld", "bootstrap.go")
	if !strings.Contains(bootSrc, "notifier.OnTierSwitch") {
		t.Error("inv-zen-229 VIOLATED: bootstrap does not delegate the tier-switch to notifier.OnTierSwitch — a divergent inline callback would reintroduce the CRITICAL repeat storm.")
	}
}

func extractFuncBodyRefresh(src, prefix string) (string, bool) {
	lines := strings.Split(src, "\n")
	var body strings.Builder
	depth := 0
	inFunc := false
	for _, line := range lines {
		if !inFunc {
			if strings.Contains(line, prefix) {
				inFunc = true
				depth = strings.Count(line, "{") - strings.Count(line, "}")
				body.WriteString(line)
				body.WriteByte('\n')
				if depth == 0 {
					break
				}
			}
			continue
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		body.WriteString(line)
		body.WriteByte('\n')
		if depth == 0 {
			break
		}
	}
	return body.String(), inFunc
}
