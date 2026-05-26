package sshexec

import (
	"os"
	"strings"
	"testing"
)

func TestValidationResultZeroValueIsRefuse(t *testing.T) {
	var r ValidationResult
	if r.OK {
		t.Fatal("zero ValidationResult.OK = true; want false (fail-closed)")
	}
	rr := Refuse("")
	if rr.OK || rr.Reason == "" {
		t.Fatalf("Refuse(\"\") = %#v, want OK=false + sentinel reason", rr)
	}
}

func TestValidateForbiddenChars(t *testing.T) {
	allowlist := []string{"alembic *"}
	forbidden := []byte{';', '&', '|', '$', '`', '<', '>', '(', ')', '{', '}', '[', ']', '"', '\''}
	for _, c := range forbidden {
		cmd := "alembic upgrade " + string(c) + "head"
		r := Validate(cmd, allowlist)
		if r.OK {
			t.Errorf("forbidden char %q allowed in %q; want rejected", c, cmd)
		}
		if !strings.Contains(r.Reason, "forbidden character") {
			t.Errorf("reason for %q = %q, want contains %q", cmd, r.Reason, "forbidden character")
		}
	}
}

func TestValidateStrictPrefix(t *testing.T) {
	allowlist := []string{
		"alembic *",
		"pytest tests/integration/*",
		"psql --version",
	}
	cases := []struct {
		cmd     string
		wantOK  bool
		wantSub string
	}{
		{"alembic upgrade head", true, ""},
		{"alembic", true, ""},
		{"alembic-tool other", false, "not in allowlist"},
		{"alembicX", false, "not in allowlist"},
		{"pytest tests/integration/test_foo.py", true, ""},
		{"pytest tests/unit/foo.py", false, "not in allowlist"},
		{"psql --version", true, ""},
		{"psql --version --extra", false, "not in allowlist"},
		{"psql", false, "not in allowlist"},
		{"", false, "empty command"},
	}
	for _, tc := range cases {
		r := Validate(tc.cmd, allowlist)
		if r.OK != tc.wantOK {
			t.Errorf("Validate(%q).OK = %v, want %v (reason=%q)", tc.cmd, r.OK, tc.wantOK, r.Reason)
		}
		if tc.wantSub != "" && !strings.Contains(r.Reason, tc.wantSub) {
			t.Errorf("Validate(%q).Reason = %q, want contains %q", tc.cmd, r.Reason, tc.wantSub)
		}
	}
}

func TestValidateEmptyAllowlistRejectsAll(t *testing.T) {
	cases := []string{"ls", "alembic upgrade head", ""}
	for _, cmd := range cases {
		r := Validate(cmd, nil)
		if r.OK {
			t.Errorf("Validate(%q, nil).OK = true; want false (fail-closed)", cmd)
		}
	}
}

func TestValidateAdversarialCorpus(t *testing.T) {
	allowlist := []string{
		"alembic *",
		"pytest *",
		"psql *",
		"docker compose -f docker/docker-compose.yml *",
		"git status",
		"git log",
	}
	payloads := loadAdversarialCorpus(t)
	if len(payloads) < 50 {
		t.Fatalf("adversarial corpus has %d entries; want >=50", len(payloads))
	}
	for _, p := range payloads {
		r := Validate(p, allowlist)
		if r.OK {
			t.Errorf("adversarial payload accepted: %q (reason should reject)", p)
		}
	}
}

func TestValidateAllowlistPatternsAreCanonical(t *testing.T) {
	allowlist := []string{"git status"}
	r := Validate("git status", allowlist)
	if !r.OK {
		t.Errorf("strict-equality match rejected: %q", r.Reason)
	}
	r = Validate("git status --porcelain", allowlist)
	if r.OK {
		t.Errorf("strict-equality should not allow extra args")
	}
}

func TestValidateWhitespaceOnlyEmpty(t *testing.T) {
	cases := []string{"   ", "\t\t", "\n", " \t \n "}
	for _, cmd := range cases {
		r := Validate(cmd, []string{"alembic *"})
		if r.OK {
			t.Errorf("whitespace-only cmd %q accepted", cmd)
		}
		if !strings.Contains(r.Reason, "empty command") {
			t.Errorf("reason for %q = %q, want 'empty command'", cmd, r.Reason)
		}
	}
}

func TestRefuseCustomReason(t *testing.T) {
	r := Refuse("custom reason")
	if r.OK {
		t.Errorf("Refuse(...).OK = true")
	}
	if r.Reason != "custom reason" {
		t.Errorf("Refuse(\"custom reason\").Reason = %q", r.Reason)
	}
}

func loadAdversarialCorpus(t *testing.T) []string {
	t.Helper()
	const path = "../../../tests/adversarial/payloads/cmd_injection.txt"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read payload corpus %q: %v", path, err)
	}
	out := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}
