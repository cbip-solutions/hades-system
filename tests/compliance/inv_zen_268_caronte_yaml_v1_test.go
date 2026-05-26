package compliance

import (
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
)

var roster247 = []string{"client-app", "auth-svc", "billing-svc", "shipping-svc"}

func fixturePath247(sub string) string {
	return "../../internal/caronte/contract/yaml/fixtures/" + sub + "/caronte.yaml"
}

func TestInvZen268_HappyPathLoads(t *testing.T) {
	m, err := yaml.Load(fixturePath247("happy"), roster247)
	if err != nil {
		t.Fatalf("Load(happy) = %v; want nil", err)
	}
	if m.SchemaVersion != 1 || len(m.Services) != 3 || m.UnresolvedPolicy != yaml.PolicySurface {
		t.Errorf("happy manifest shape drift: %+v", m)
	}
}

func TestInvZen268_RefusesMissingSchemaVersion(t *testing.T) {
	_, err := yaml.Load(fixturePath247("missing_schema_version"), roster247)
	if !errors.Is(err, yaml.ErrMissingSchemaVersion) {
		t.Errorf("err = %v; want ErrMissingSchemaVersion", err)
	}
}

func TestInvZen268_RefusesMultipleBaseURLVariants(t *testing.T) {
	_, err := yaml.Load(fixturePath247("multiple_base_url_variants"), roster247)
	if !errors.Is(err, yaml.ErrMultipleBaseURLVariants) {
		t.Errorf("err = %v; want ErrMultipleBaseURLVariants", err)
	}
}

func TestInvZen268_RefusesUnknownTargetRepo(t *testing.T) {
	_, err := yaml.Load(fixturePath247("unknown_target_repo"), roster247)
	if !errors.Is(err, yaml.ErrUnknownTargetRepo) {
		t.Errorf("err = %v; want ErrUnknownTargetRepo", err)
	}
}

func TestInvZen268_RefusesInlineSecretAllVariants(t *testing.T) {
	cases := []string{
		"inline_secret_snake",
		"inline_secret_kebab",
		"inline_secret_camel",
		"inline_secret_uppercase",
	}
	for _, c := range cases {
		_, err := yaml.Load(fixturePath247(c), roster247)
		if !errors.Is(err, yaml.ErrInlineSecret) {
			t.Errorf("Load(%s) = %v; want ErrInlineSecret", c, err)
		}
	}
}

func TestInvZen268_RefusesPatternTooLong(t *testing.T) {
	_, err := yaml.Load(fixturePath247("pattern_too_long"), roster247)
	if !errors.Is(err, yaml.ErrPatternTooLong) {
		t.Errorf("err = %v; want ErrPatternTooLong", err)
	}
}

func TestInvZen268_RefusesPatternRegexDoS(t *testing.T) {
	_, err := yaml.Load(fixturePath247("pattern_regex_dos"), roster247)
	if !errors.Is(err, yaml.ErrPatternRegexDoS) {
		t.Errorf("err = %v; want ErrPatternRegexDoS", err)
	}
}

func TestInvZen268_RefusesInvalidUnresolvedPolicy(t *testing.T) {
	_, err := yaml.Load(fixturePath247("invalid_unresolved_policy"), roster247)
	if !errors.Is(err, yaml.ErrInvalidUnresolvedPolicy) {
		t.Errorf("err = %v; want ErrInvalidUnresolvedPolicy", err)
	}
}
