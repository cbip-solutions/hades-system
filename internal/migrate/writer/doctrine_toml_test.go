package writer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/migrate/mapping"
)

func TestWriteDoctrineTOML_OneToOnePreservation(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "doctrines", "imported-from-claude-code.toml")
	body := []byte(`{"allow":["Read(*)","Bash(make:*)","WebFetch(domain:github.com)"],"deny":["Write(.env)","Bash(sudo:*)"],"env":{"FOO":"bar"}}`)
	e := mapping.PlanEntry{
		Kind:       mapping.EntryKindDoctrine,
		SourcePath: "/x/settings.json",
		BodyBytes:  body,
	}
	if err := writeDoctrineTOML(path, e); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	if !strings.Contains(s, `schema_version = "1.0"`) {
		t.Errorf("missing schema_version: %s", s)
	}
	if !strings.Contains(s, `name = "imported-from-claude-code"`) {
		t.Errorf("missing name: %s", s)
	}
	if !strings.Contains(s, `inherits_from = "max-scope"`) {
		t.Errorf("missing inherits_from: %s", s)
	}

	for _, perm := range []string{"Read(*)", "Bash(make:*)", "WebFetch(domain:github.com)"} {
		if !strings.Contains(s, `"`+perm+`"`) {
			t.Errorf("allow %q dropped: %s", perm, s)
		}
	}

	for _, perm := range []string{"Write(.env)", "Bash(sudo:*)"} {
		if !strings.Contains(s, `"`+perm+`"`) {
			t.Errorf("deny %q dropped: %s", perm, s)
		}
	}

	if !strings.Contains(s, "[capa_firewall.tiers]") {
		t.Errorf("missing tiers block: %s", s)
	}

	if !strings.Contains(s, `FOO = "bar"`) {
		t.Errorf("env FOO dropped: %s", s)
	}
}

func TestWriteDoctrineTOML_Deterministic(t *testing.T) {
	t.Parallel()
	tmp1 := t.TempDir()
	tmp2 := t.TempDir()
	body := []byte(`{"allow":["a","b","c"],"deny":["d","e"],"env":{}}`)
	e := mapping.PlanEntry{Kind: mapping.EntryKindDoctrine, SourcePath: "/x", BodyBytes: body}
	p1 := filepath.Join(tmp1, "out.toml")
	p2 := filepath.Join(tmp2, "out.toml")
	if err := writeDoctrineTOML(p1, e); err != nil {
		t.Fatal(err)
	}
	if err := writeDoctrineTOML(p2, e); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(p1)
	b, _ := os.ReadFile(p2)
	if string(a) != string(b) {
		t.Errorf("non-deterministic:\n%s\n%s", a, b)
	}
}

func TestWriteDoctrineTOML_MalformedJSONErrors(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.toml")
	e := mapping.PlanEntry{Kind: mapping.EntryKindDoctrine, BodyBytes: []byte("not json")}
	if err := writeDoctrineTOML(path, e); err == nil {
		t.Errorf("expected parse error")
	}
}

func TestClassifyTier_KnownPrefixes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		perm string
		want string
	}{
		{"Write(.env)", "high"},
		{"filesystem.write(/etc/passwd)", "high"},
		{"postgres.execute(SELECT *)", "high"},
		{"github.write(repo)", "high"},
		{"mysql.write(table)", "high"},
		{"playwright.browse(url)", "medium"},
		{"sentry.query()", "medium"},
		{"openapi.invoke()", "medium"},
		{"graphql.query()", "medium"},
		{"webfetch(url)", "medium"},
		{"linear.update()", "medium"},
		{"sequential_thinking(...)", "low"},
		{"memory.recall()", "low"},
		{"gitnexus.read()", "low"},
		{"Read(*)", "low"},
		{"Bash(make)", "low"},
		{"unknown.tool", "medium"},
		{"completely.alien", "medium"},
	}
	for _, c := range cases {
		got := classifyTier(c.perm)
		if got != c.want {
			t.Errorf("%s: got %s, want %s", c.perm, got, c.want)
		}
	}
}

func TestClassifyTierStrict_UnknownReturnsEmpty(t *testing.T) {
	t.Parallel()
	if got := classifyTierStrict("completely.alien"); got != "" {
		t.Errorf("unknown should return empty under strict; got %s", got)
	}
	if got := classifyTierStrict("Read(*)"); got != "low" {
		t.Errorf("Read(*): got %s, want low", got)
	}
}

func TestImportDoctrineStrict_HaltsOnUnmappable(t *testing.T) {
	t.Parallel()
	err := ImportDoctrineStrict([]string{"completely.alien.opcode"}, nil)
	if !errors.Is(err, ErrUnknownPermissionStrict) {
		t.Errorf("err: got %v, want ErrUnknownPermissionStrict", err)
	}
}

func TestImportDoctrineStrict_HaltsOnDeny(t *testing.T) {
	t.Parallel()
	err := ImportDoctrineStrict(nil, []string{"alien.deny"})
	if !errors.Is(err, ErrUnknownPermissionStrict) {
		t.Errorf("err: got %v, want ErrUnknownPermissionStrict", err)
	}
}

func TestImportDoctrineStrict_AllowsKnown(t *testing.T) {
	t.Parallel()
	err := ImportDoctrineStrict(
		[]string{"Read(*)", "Bash(make:*)"},
		[]string{"Write(.env)", "filesystem.write(/etc)"},
	)
	if err != nil {
		t.Errorf("known permissions: %v", err)
	}
}

func TestWriteDoctrineTOML_DoubleQuoteEscaping(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.toml")
	body := []byte(`{"allow":["perm-with-\"quote\""],"deny":[],"env":{}}`)
	e := mapping.PlanEntry{Kind: mapping.EntryKindDoctrine, SourcePath: "/x", BodyBytes: body}
	if err := writeDoctrineTOML(path, e); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)

	if !strings.Contains(string(out), `\"`) {
		t.Errorf("quote not escaped: %s", out)
	}
}
