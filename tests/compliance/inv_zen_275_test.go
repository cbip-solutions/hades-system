package compliance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/cli"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func readSourceForInv275(t *testing.T, relPath string) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), relPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestInvZen275_NoLegacyCaronteCalls(t *testing.T) {
	root := repoRoot(t)
	pattern := regexp.MustCompile(`/v1/caronte/(probe|health)`)
	dirs := []string{
		filepath.Join("internal", "client"),
		filepath.Join("internal", "cli"),
	}
	for _, dir := range dirs {
		full := filepath.Join(root, dir)
		entries, err := os.ReadDir(full)
		if err != nil {
			t.Fatalf("readdir %s: %v", full, err)
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(full, name)
			body, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("read %s: %v", path, err)
				continue
			}

			for lineno, line := range strings.Split(string(body), "\n") {
				trimmed := strings.TrimLeft(line, " \t")
				if strings.HasPrefix(trimmed, "//") {
					continue
				}
				if loc := pattern.FindStringIndex(line); loc != nil {
					t.Errorf("inv-zen-275 violated: %s:%d contains legacy URL %q in non-comment line: %s",
						path, lineno+1, line[loc[0]:loc[1]], strings.TrimSpace(line))
				}
			}
		}
	}
}

func TestInvZen275_HeaderPropagationSitesPresent(t *testing.T) {
	cases := []struct {
		file           string
		minOccurrences int
	}{

		{"internal/client/codegraph.go", 5},

		{"internal/client/codegraph_plan19.go", 5},
	}
	for _, c := range cases {
		src := readSourceForInv275(t, c.file)
		count := strings.Count(src, "projectAliasHeaders(")
		if count < c.minOccurrences {
			t.Errorf("inv-zen-275: %s has %d projectAliasHeaders( call sites; want >= %d",
				c.file, count, c.minOccurrences)
		}
	}
}

func TestInvZen275_HeaderLiteralPresent(t *testing.T) {
	src := readSourceForInv275(t, "internal/client/codegraph.go")
	if !strings.Contains(src, `"X-Zen-Project-ID"`) {
		t.Error("inv-zen-275: internal/client/codegraph.go missing canonical X-Zen-Project-ID literal in projectAliasHeaders helper")
	}
}

func TestInvZen275_CatalogCodeRegistered(t *testing.T) {
	src := readSourceForInv275(t, "internal/errors/codes.go")
	pattern := regexp.MustCompile(`CodeEndpointNotFound\s+Code\s*=\s*"daemon\.endpoint-not-found"`)
	if !pattern.MatchString(src) {
		t.Error("inv-zen-275 violated: internal/errors/codes.go missing CodeEndpointNotFound declaration with canonical string")
	}

	if !strings.Contains(src, `"daemon.endpoint-not-found":`) {
		t.Error("inv-zen-275 violated: internal/errors/codes.go missing catalog entry for daemon.endpoint-not-found")
	}
}

func TestInvZen275_404MappingPresent(t *testing.T) {
	src := readSourceForInv275(t, "internal/cli/codegraph.go")

	if !strings.Contains(src, "StatusNotFound") {
		t.Error("inv-zen-275: internal/cli/codegraph.go missing StatusNotFound branch (404 → CodeEndpointNotFound)")
	}
	if !strings.Contains(src, "CodeEndpointNotFound") {
		t.Error("inv-zen-275: internal/cli/codegraph.go missing CodeEndpointNotFound reference")
	}
}

func TestInvZen275_DoctorCaronteSurfaces404(t *testing.T) {
	src := readSourceForInv275(t, "internal/cli/doctor_caronte.go")
	if !strings.Contains(src, "StatusNotFound") {
		t.Error("inv-zen-275: internal/cli/doctor_caronte.go missing StatusNotFound branch")
	}
	if !strings.Contains(src, "CodeEndpointNotFound") {
		t.Error("inv-zen-275: internal/cli/doctor_caronte.go missing CodeEndpointNotFound reference (404 → catalog recovery hint)")
	}
}

// TestInvZen275_404MapsToEndpointNotFoundEndToEnd drives the CLI flow
// against an httptest.Server that returns 404 for /v1/mcpgateway/codegraph.
// The classifier MUST surface *CodedError with Code == CodeEndpointNotFound,
// NOT daemon.unreachable. invariant §B.
func TestInvZen275_404MapsToEndpointNotFoundEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	_, transportErr := c.CodegraphQuery(context.Background(), client.CodegraphQueryRequest{Query: "x"})
	if transportErr == nil {
		t.Fatal("expected HTTPError on 404; got nil")
	}
	classified := cli.ClassifyMCPGatewayErrorForTest(transportErr, "codegraph")
	if classified == nil {
		t.Fatal("classified error is nil")
	}
	if !ierrors.IsCode(classified, ierrors.CodeEndpointNotFound) {
		t.Errorf("404 did NOT map to CodeEndpointNotFound; got %v", classified)
	}
	if ierrors.IsCode(classified, "daemon.unreachable") {
		t.Error("404 collapsed into daemon.unreachable; inv-zen-275 distinction lost")
	}
}

func TestInvZen275_404IsNotDaemonUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	_, transportErr := c.CodegraphQuery(context.Background(), client.CodegraphQueryRequest{Query: "x"})
	classified := cli.ClassifyMCPGatewayErrorForTest(transportErr, "codegraph")
	if ierrors.IsCode(classified, "daemon.unreachable") {
		t.Fatal("404 surfaced as daemon.unreachable — the 404 → CodeEndpointNotFound branch is gone")
	}
}
