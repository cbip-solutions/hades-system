package yaml

import (
	"testing"

	yaml3 "gopkg.in/yaml.v3"
)

func TestManifestFieldSet(t *testing.T) {
	m := Manifest{
		SchemaVersion:    1,
		Services:         []Service{{BaseURL: "http://x", TargetRepo: "r"}},
		UnresolvedPolicy: PolicySurface,
	}
	if m.SchemaVersion == 0 || len(m.Services) == 0 || m.UnresolvedPolicy == "" {
		t.Fatal("Manifest field set incomplete")
	}
}

func TestServiceFieldSet(t *testing.T) {
	s := Service{
		BaseURL:        "http://x",
		BaseURLEnv:     "X_URL",
		BaseURLPattern: `^https?://`,
		TargetRepo:     "r",
		Notes:          "n",
	}
	if s.BaseURL == "" && s.BaseURLEnv == "" && s.BaseURLPattern == "" {
		t.Fatal("Service field set incomplete")
	}
}

func TestPolicyEnumValues(t *testing.T) {
	cases := []struct {
		got  UnresolvedPolicy
		want string
	}{
		{PolicySurface, "surface"},
		{PolicyFail, "fail"},
		{PolicySilent, "silent"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("UnresolvedPolicy = %q; want %q", string(c.got), c.want)
		}
	}
}

func TestDefaultUnresolvedPolicyIsSurface(t *testing.T) {
	if DefaultUnresolvedPolicy != PolicySurface {
		t.Errorf("DefaultUnresolvedPolicy = %q; want %q (doctrine-default, inv-zen-265)",
			DefaultUnresolvedPolicy, PolicySurface)
	}
}

func TestManifestYAMLTags(t *testing.T) {
	src := []byte(`schema_version: 1
services:
  - base_url_env: AUTH_SVC_URL
    target_repo: auth-svc
    notes: "personal-account auth gateway"
unresolved_policy: surface
`)
	var m Manifest
	if err := yaml3.Unmarshal(src, &m); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if m.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d; want 1 (yaml tag schema_version)", m.SchemaVersion)
	}
	if len(m.Services) != 1 {
		t.Fatalf("len(Services) = %d; want 1", len(m.Services))
	}
	if m.Services[0].BaseURLEnv != "AUTH_SVC_URL" {
		t.Errorf("BaseURLEnv = %q; want AUTH_SVC_URL (yaml tag base_url_env)", m.Services[0].BaseURLEnv)
	}
	if m.Services[0].TargetRepo != "auth-svc" {
		t.Errorf("TargetRepo = %q; want auth-svc (yaml tag target_repo)", m.Services[0].TargetRepo)
	}
	if m.Services[0].Notes != "personal-account auth gateway" {
		t.Errorf("Notes round-trip drift: %q", m.Services[0].Notes)
	}
	if string(m.UnresolvedPolicy) != "surface" {
		t.Errorf("UnresolvedPolicy = %q; want surface (yaml tag unresolved_policy)", m.UnresolvedPolicy)
	}
}
