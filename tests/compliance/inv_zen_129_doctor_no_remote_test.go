package compliance

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var doctorSurfaceFiles = []string{
	"internal/cli/doctor.go",
	"internal/cli/probe.go",
	"internal/cli/doctor_knowledge.go",
	"internal/knowledge/prober.go",
}

func TestInvZen129_DoctorNoRemoteHTTP(t *testing.T) {
	root := repoRoot(t)

	args := []string{"-EnH", `(http\.Get|http\.Post|net\.Dial|http\.Client.*Do)`}
	for _, rel := range doctorSurfaceFiles {
		args = append(args, filepath.Join(root, rel))
	}

	cmd := exec.Command("grep", args...)
	out, err := cmd.CombinedOutput()
	hits := strings.TrimSpace(string(out))

	if err == nil {

		t.Errorf("inv-zen-129 doctor-surface violation — Plan 7 doctor must stay local; "+
			"http.Get/Post/net.Dial/Client.Do call sites found:\n%s", hits)
		return
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		switch exitErr.ExitCode() {
		case 1:

			return
		default:
			t.Fatalf("inv-zen-129: grep failed with exit %d: %v\noutput:\n%s",
				exitErr.ExitCode(), err, hits)
		}
	}

	t.Fatalf("inv-zen-129: grep launch failed: %v\noutput:\n%s", err, hits)
}

func TestInvZen130_DoctorExtensionHooksProbe(t *testing.T) {
	root := repoRoot(t)
	target := filepath.Join(root, "internal", "cli", "doctor_knowledge.go")

	cmd := exec.Command("grep", "-n", "knowledge.extension_hooks.null_default", target)
	out, err := cmd.CombinedOutput()

	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("inv-zen-130: grep launch failed: %v\n%s", err, string(out))
		}
		if exitErr.ExitCode() == 1 {
			t.Errorf("inv-zen-130 violation — probe name %q absent from %s; "+
				"the operator-facing extension-hooks probe contract is broken",
				"knowledge.extension_hooks.null_default", target)
			return
		}
		t.Fatalf("inv-zen-130: grep failed with exit %d: %v\n%s",
			exitErr.ExitCode(), err, string(out))
	}

	if !strings.Contains(string(out), "knowledge.extension_hooks.null_default") {
		t.Errorf("inv-zen-130: grep returned exit 0 but probe name not in output (anomalous):\n%s", out)
	}
}
