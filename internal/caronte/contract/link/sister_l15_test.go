//go:build cgo

package link

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestSisterClaim_Surface_PolicyFail_ReturnsErrNoManifestEntry(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	us := &fakeUnresolvedStore{}
	audit := &fakeAuditEmitter{}
	s := &unresolvedSurfacer{store: us, audit: audit, workspaceID: "ws-test"}

	err := s.Surface(context.Background(), store.APICall{
		CallID:     "c-sister-fail",
		Repo:       "client",
		BaseURLRef: "UNKNOWN",
	}, yaml.PolicyFail, "no manifest entry")
	if err == nil {
		t.Fatal("Surface(PolicyFail) returned nil err; want errors.Is ErrNoManifestEntry true")
	}
	if !errors.Is(err, ErrNoManifestEntry) {
		t.Errorf("Surface(PolicyFail) err = %v; want errors.Is ErrNoManifestEntry true", err)
	}
}

func TestSisterClaim_Surface_PolicySilent_DropsWithoutAudit(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	us := &fakeUnresolvedStore{}
	audit := &fakeAuditEmitter{}
	s := &unresolvedSurfacer{store: us, audit: audit, workspaceID: "ws-test"}

	err := s.Surface(context.Background(), store.APICall{
		CallID:     "c-sister-silent",
		Repo:       "client",
		BaseURLRef: "UNKNOWN",
	}, yaml.PolicySilent, "no manifest entry")
	if err != nil {
		t.Errorf("Surface(PolicySilent) err = %v; want nil (silent drop)", err)
	}
	if len(us.inserted) != 0 {
		t.Errorf("Surface(PolicySilent) persisted %d UnresolvedRows; want 0 (silent drop)", len(us.inserted))
	}
	if len(audit.events) != 0 {
		t.Errorf("Surface(PolicySilent) emitted %d audit events; want 0 (silent drop)", len(audit.events))
	}
}

func TestSisterClaim_ConfidenceTierTable_LegalPairs(t *testing.T) {

	tierTable := []struct {
		conf       Confidence
		confString string
		method     LinkMethod
		mthString  string
	}{
		{ConfExactProtoImport, "exact_proto_import", LinkArtifact, "artifact"},
		{ConfSpecArtifact, "spec_artifact", LinkArtifact, "artifact"},
		{ConfStaticPath, "static_path", LinkStatic, "static"},
		{ConfFuzzyPath, "fuzzy_path", LinkFuzzy, "fuzzy"},
	}
	for _, row := range tierTable {
		if string(row.conf) != row.confString {
			t.Errorf("Confidence constant drift: %s = %q, want %q (C-5 frozen)",
				row.confString, string(row.conf), row.confString)
		}
		if string(row.method) != row.mthString {
			t.Errorf("LinkMethod constant drift: %s = %q, want %q (C-5 frozen)",
				row.mthString, string(row.method), row.mthString)
		}
	}

	if string(LinkCaronteYAML) != "caronte_yaml" {
		t.Errorf("LinkCaronteYAML drift: got %q, want \"caronte_yaml\"", string(LinkCaronteYAML))
	}
}
