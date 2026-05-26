//go:build adversarial

package adversarial

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

func TestPlan20AdversarialConfidenceTierDowngradeInjection(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbPath := filepath.Join(t.TempDir(), "workspace.db")
	wsdb, err := federation.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	defer wsdb.Close()

	const (
		wsID  = "ws-adv-tier"
		owner = "svc-a"
	)
	if err := wsdb.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID:   wsID,
		OwningProject: owner,
		PolicyLocked:  false,
		CreatedAt:     time.Now().Unix(),
		SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	if err := wsdb.AddMember(ctx, federation.MemberRow{
		WorkspaceID:  wsID,
		ProjectID:    owner,
		RegisteredAt: time.Now().Unix(),
	}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	ls := wsdb.LinkStore()
	mkRow := func(suffix, confidence, linkMethod string) federation.LinkRow {
		return federation.LinkRow{
			CallID:       "call-" + suffix,
			CallRepo:     owner,
			EndpointID:   "ep-" + suffix,
			EndpointRepo: owner,
			Confidence:   confidence,
			WorkspaceID:  wsID,
			ResolvedAt:   time.Now().Unix(),
			LinkMethod:   linkMethod,
		}
	}

	hostileConfidences := []string{
		"forged_high_tier",
		"EXACT_PROTO_IMPORT",
		"",
		"  exact_proto_import  ",
		"exact_proto_import; DROP TABLE contract_links",
	}
	for i, hc := range hostileConfidences {
		err := ls.Append(ctx, mkRow("hostile-conf-"+itoaAdv(i), hc, "artifact"))
		if err == nil {
			t.Errorf("plan20 adv L-13 [hostile_conf=%q]: Append succeeded; want SQL CHECK refusal", hc)
			continue
		}
		if !isCheckConstraintErr(err) {
			t.Errorf("plan20 adv L-13 [hostile_conf=%q]: err = %v; want SQL CHECK constraint failure", hc, err)
		}
	}

	hostileMethods := []string{
		"forged_method",
		"ARTIFACT",
		"",
		"  artifact  ",
		"artifact UNION SELECT * FROM caronte_workspaces",
	}
	for i, hm := range hostileMethods {
		err := ls.Append(ctx, mkRow("hostile-method-"+itoaAdv(i), "exact_proto_import", hm))
		if err == nil {
			t.Errorf("plan20 adv L-13 [hostile_method=%q]: Append succeeded; want SQL CHECK refusal", hm)
			continue
		}
		if !isCheckConstraintErr(err) {
			t.Errorf("plan20 adv L-13 [hostile_method=%q]: err = %v; want SQL CHECK constraint failure", hm, err)
		}
	}

	legalEnumValues := []struct {
		conf, method string
	}{

		{"exact_proto_import", "artifact"},
		{"spec_artifact", "artifact"},
		{"static_path", "static"},
		{"fuzzy_path", "fuzzy"},
	}
	for i, p := range legalEnumValues {
		err := ls.Append(ctx, mkRow("legal-pair-"+itoaAdv(i), p.conf, p.method))
		if err != nil {
			t.Errorf("plan20 adv L-13 [legal_pair conf=%s method=%s]: Append failed: %v", p.conf, p.method, err)
		}
	}

	crossFieldIllegal := []struct {
		conf, method, note string
	}{
		{"exact_proto_import", "fuzzy", "exact_proto requires artifact"},
		{"spec_artifact", "static", "spec_artifact requires artifact"},
		{"static_path", "fuzzy", "static_path requires static"},
		{"fuzzy_path", "static", "fuzzy_path requires fuzzy"},
		{"exact_proto_import", "caronte_yaml", "exact_proto requires artifact"},
	}
	for i, p := range crossFieldIllegal {
		err := ls.Append(ctx, mkRow("xfield-illegal-"+itoaAdv(i), p.conf, p.method))
		if err != nil {

			t.Logf("plan20 adv L-13 (informational): cross-field illegal (conf=%s, method=%s) REFUSED by SQL: %v — possible future joint CHECK; expected behavior is per-field CHECK only at SQL layer; the Go-layer checkTierConsistency is the cross-field gate",
				p.conf, p.method, err)
		}
	}
}

func isCheckConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "CHECK constraint failed") ||
		strings.Contains(s, "constraint failed")
}
