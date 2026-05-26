package link

import (
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func newManifest(t *testing.T, services []yaml.Service) *yaml.Manifest {
	t.Helper()
	m := &yaml.Manifest{
		SchemaVersion:    1,
		Services:         services,
		UnresolvedPolicy: yaml.PolicySurface,
	}
	return m
}

func TestResolveTargetRepoEnvHit(t *testing.T) {
	m := newManifest(t, []yaml.Service{
		{BaseURLEnv: "AUTH_SVC_URL", TargetRepo: "auth-svc"},
	})
	repo, method, err := resolveTargetRepo(store.APICall{BaseURLRef: "AUTH_SVC_URL"}, m)
	if err != nil || repo != "auth-svc" || method != LinkCaronteYAML {
		t.Errorf("env-hit = (%q,%q,%v); want (auth-svc, caronte_yaml, nil)", repo, method, err)
	}
}

func TestResolveTargetRepoLiteralHit(t *testing.T) {
	m := newManifest(t, []yaml.Service{
		{BaseURL: "http://billing-svc", TargetRepo: "billing-svc"},
	})
	repo, _, err := resolveTargetRepo(store.APICall{BaseURLRef: "http://billing-svc"}, m)
	if err != nil || repo != "billing-svc" {
		t.Errorf("literal-hit = (%q,_,%v); want (billing-svc, _, nil)", repo, err)
	}
}

func TestResolveTargetRepoPatternHit(t *testing.T) {

	m, err := yaml.Load(testFixture(t, "happy"), []string{"client-app", "auth-svc", "billing-svc", "shipping-svc"})
	if err != nil {
		t.Fatalf("yaml.Load(happy) = %v", err)
	}
	repo, _, err := resolveTargetRepo(store.APICall{BaseURLRef: "https://shipping-eu1.internal/"}, m)
	if err != nil || repo != "shipping-svc" {
		t.Errorf("pattern-hit = (%q,_,%v); want (shipping-svc, _, nil)", repo, err)
	}
}

func TestResolveTargetRepoMiss(t *testing.T) {
	m := newManifest(t, []yaml.Service{
		{BaseURLEnv: "AUTH_SVC_URL", TargetRepo: "auth-svc"},
	})
	_, _, err := resolveTargetRepo(store.APICall{BaseURLRef: "UNKNOWN_URL"}, m)
	if !errors.Is(err, ErrNoManifestEntry) {
		t.Errorf("miss = %v; want ErrNoManifestEntry", err)
	}
}

func TestResolveTargetRepoEmptyBaseURLRefIsMiss(t *testing.T) {

	m := newManifest(t, []yaml.Service{
		{BaseURLEnv: "AUTH_SVC_URL", TargetRepo: "auth-svc"},
	})
	_, _, err := resolveTargetRepo(store.APICall{BaseURLRef: ""}, m)
	if !errors.Is(err, ErrNoManifestEntry) {
		t.Errorf("empty ref = %v; want ErrNoManifestEntry", err)
	}
}

func TestResolveTargetRepoNilManifestIsMiss(t *testing.T) {
	_, _, err := resolveTargetRepo(store.APICall{BaseURLRef: "anything"}, nil)
	if !errors.Is(err, ErrNoManifestEntry) {
		t.Errorf("nil manifest = %v; want ErrNoManifestEntry", err)
	}
}

func TestResolveTargetRepoAmbiguousIsRefused(t *testing.T) {

	m, err := yaml.Load(testFixture(t, "happy"), []string{"client-app", "auth-svc", "billing-svc", "shipping-svc"})
	if err != nil {
		t.Fatalf("yaml.Load: %v", err)
	}

	if err := injectPatternService(m, `^https://shipping-[a-z0-9]+\.internal/`, "billing-svc"); err != nil {
		t.Fatal(err)
	}
	_, _, err = resolveTargetRepo(store.APICall{BaseURLRef: "https://shipping-eu1.internal/"}, m)
	if !errors.Is(err, ErrAmbiguousResolution) {
		t.Errorf("ambiguous = %v; want ErrAmbiguousResolution", err)
	}
}

func TestResolveTargetRepoTwoMatchesSameRepoCollapses(t *testing.T) {

	m, err := yaml.Load(testFixture(t, "happy"), []string{"client-app", "auth-svc", "billing-svc", "shipping-svc"})
	if err != nil {
		t.Fatalf("yaml.Load: %v", err)
	}

	if err := injectPatternService(m, `^https://shipping-[a-z0-9]+\.internal/`, "shipping-svc"); err != nil {
		t.Fatal(err)
	}
	repo, _, err := resolveTargetRepo(store.APICall{BaseURLRef: "https://shipping-eu1.internal/"}, m)
	if err != nil {
		t.Errorf("redundant-but-same-repo = %v; want nil (dedup collapse)", err)
	}
	if repo != "shipping-svc" {
		t.Errorf("repo = %q; want shipping-svc", repo)
	}
}

func testFixture(t *testing.T, sub string) string {
	t.Helper()
	return "../yaml/fixtures/" + sub + "/caronte.yaml"
}

func injectPatternService(m *yaml.Manifest, pattern, targetRepo string) error {
	return m.AddPatternService(pattern, targetRepo)
}
