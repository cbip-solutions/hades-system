package augment_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

func privacySchemas() map[string]*augment.DoctrineSchema {
	return map[string]*augment.DoctrineSchema{
		"max-scope": {
			Augmentation: augment.AugmentationAxis{
				Enable: true, MaxKGTokens: 25000, TimeoutMs: 2000,
				CrossProjectScope: "opt-in",
			},
			KnowledgeCrossProject: augment.CrossProjectAxis{
				VisibleTo:       []string{"max-scope", "default"},
				QueriesCanReach: []string{"max-scope", "default"},
			},
		},
		"default": {
			Augmentation: augment.AugmentationAxis{
				Enable: true, MaxKGTokens: 10000, TimeoutMs: 1000,
				CrossProjectScope: "opt-in",
			},
			KnowledgeCrossProject: augment.CrossProjectAxis{
				VisibleTo:       []string{"max-scope", "default"},
				QueriesCanReach: []string{"max-scope", "default"},
			},
		},
		"capa-firewall": {
			Augmentation: augment.AugmentationAxis{
				Enable: false, MaxKGTokens: 0, TimeoutMs: 500,
				CrossProjectScope: "forbidden",
			},
			KnowledgeCrossProject: augment.CrossProjectAxis{
				VisibleTo:       []string{},
				QueriesCanReach: []string{"self"},
			},
		},
	}
}

type projectDoctrineLookup struct {
	mp map[string]string
}

func (p *projectDoctrineLookup) DoctrineForProject(_ context.Context, projectID string) (string, error) {
	if d, ok := p.mp[projectID]; ok {
		return d, nil
	}
	return "", errors.New("project not authorized: " + projectID)
}

func newPrivacyFilter(t *testing.T, schemas map[string]*augment.DoctrineSchema, projects map[string]string) *augment.PrivacyFilter {
	t.Helper()
	loader := &fakeDoctrineLoader{schemas: schemas}
	lookup := &projectDoctrineLookup{mp: projects}
	return augment.NewPrivacyFilter(loader, lookup)
}

func TestPrivacyFilter_CapaFirewallIsolatesToSelf(t *testing.T) {
	pf := newPrivacyFilter(t, privacySchemas(), map[string]string{
		"internal-platform-x": "max-scope",
		"client-secret":       "capa-firewall",
		"another-proj":        "default",
	})
	results := []augment.QueryResult{
		{NoteID: "n1", ProjectID: "internal-platform-x", Source: "fts"},
		{NoteID: "n2", ProjectID: "client-secret", Source: "fts"},
		{NoteID: "n3", ProjectID: "another-proj", Source: "fts"},
	}
	filtered, dropped, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "capa-firewall",
		SourceProject:  "client-secret",
		Candidates:     results,
	})
	if err != nil {
		t.Fatalf("FilterCrossProject: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ProjectID != "client-secret" {
		t.Fatalf("filtered: want 1 row from client-secret, got %+v", filtered)
	}
	sort.Strings(dropped)
	want := []string{"another-proj", "internal-platform-x"}
	if !reflect.DeepEqual(dropped, want) {
		t.Fatalf("dropped: want %v, got %v", want, dropped)
	}
}

func TestPrivacyFilter_DefaultDoctrineCrossesToMaxScope(t *testing.T) {
	pf := newPrivacyFilter(t, privacySchemas(), map[string]string{
		"internal-platform-x": "max-scope",
		"client-secret":       "capa-firewall",
		"another-proj":        "default",
	})
	results := []augment.QueryResult{
		{NoteID: "n1", ProjectID: "internal-platform-x", Source: "fts"},
		{NoteID: "n2", ProjectID: "client-secret", Source: "fts"},
		{NoteID: "n3", ProjectID: "another-proj", Source: "fts"},
	}
	filtered, dropped, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "default",
		SourceProject:  "another-proj",
		Candidates:     results,
	})
	if err != nil {
		t.Fatalf("FilterCrossProject: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("filtered: want 2 rows, got %d: %+v", len(filtered), filtered)
	}
	if len(dropped) != 1 || dropped[0] != "client-secret" {
		t.Fatalf("dropped: want [client-secret], got %v", dropped)
	}
}

func TestPrivacyFilter_MaxScopeCannotReachCapaFirewall(t *testing.T) {
	pf := newPrivacyFilter(t, privacySchemas(), map[string]string{
		"internal-platform-x": "max-scope",
		"client-secret":       "capa-firewall",
	})
	results := []augment.QueryResult{
		{NoteID: "n1", ProjectID: "internal-platform-x", Source: "fts"},
		{NoteID: "n2", ProjectID: "client-secret", Source: "fts"},
	}
	filtered, dropped, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "max-scope",
		SourceProject:  "internal-platform-x",
		Candidates:     results,
	})
	if err != nil {
		t.Fatalf("FilterCrossProject: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ProjectID != "internal-platform-x" {
		t.Fatalf("filtered: want 1 row from internal-platform-x, got %+v", filtered)
	}
	if len(dropped) != 1 || dropped[0] != "client-secret" {
		t.Fatalf("dropped: want [client-secret], got %v", dropped)
	}
}

func TestPrivacyFilter_SelfProjectAlwaysVisible(t *testing.T) {
	pf := newPrivacyFilter(t, map[string]*augment.DoctrineSchema{
		"weird": {
			KnowledgeCrossProject: augment.CrossProjectAxis{
				QueriesCanReach: []string{},
			},
		},
	}, map[string]string{
		"my-proj": "weird",
	})
	results := []augment.QueryResult{
		{NoteID: "n1", ProjectID: "my-proj", Source: "fts"},
	}
	filtered, dropped, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "weird",
		SourceProject:  "my-proj",
		Candidates:     results,
	})
	if err != nil {
		t.Fatalf("FilterCrossProject: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected own project always visible, got dropped=%v", dropped)
	}
}

func TestPrivacyFilter_LoaderErrorPropagates(t *testing.T) {
	loader := &fakeDoctrineLoader{
		errOn: map[string]error{"max-scope": errors.New("loader exploded")},
	}
	pf := augment.NewPrivacyFilter(loader, &projectDoctrineLookup{mp: map[string]string{"internal-platform-x": "max-scope"}})
	_, _, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "max-scope",
		SourceProject:  "internal-platform-x",
		Candidates:     []augment.QueryResult{{NoteID: "n1", ProjectID: "internal-platform-x"}},
	})
	if err == nil || !contains(err.Error(), "loader exploded") {
		t.Fatalf("expected loader error to propagate, got %v", err)
	}
}

func TestPrivacyFilter_NilSchemaErrors(t *testing.T) {
	loader := &fakeDoctrineLoader{
		nilOn: map[string]bool{"foo": true},
	}
	pf := augment.NewPrivacyFilter(loader, &projectDoctrineLookup{mp: map[string]string{"p": "foo"}})
	_, _, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "foo",
		SourceProject:  "p",
		Candidates:     []augment.QueryResult{{ProjectID: "p"}},
	})
	if err == nil || !contains(err.Error(), "nil schema") {
		t.Fatalf("expected nil-schema error, got %v", err)
	}
}

func TestPrivacyFilter_UnknownProjectDropped(t *testing.T) {
	pf := newPrivacyFilter(t, privacySchemas(), map[string]string{
		"internal-platform-x": "max-scope",
	})
	results := []augment.QueryResult{
		{NoteID: "n1", ProjectID: "internal-platform-x", Source: "fts"},
		{NoteID: "n2", ProjectID: "stale-proj", Source: "fts"},
	}
	filtered, dropped, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "max-scope",
		SourceProject:  "internal-platform-x",
		Candidates:     results,
	})
	if err != nil {
		t.Fatalf("FilterCrossProject: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ProjectID != "internal-platform-x" {
		t.Fatalf("filtered: want 1 row from internal-platform-x, got %+v", filtered)
	}
	if len(dropped) != 1 || dropped[0] != "stale-proj" {
		t.Fatalf("dropped: want [stale-proj], got %v", dropped)
	}
}

func TestPrivacyFilter_EmptyCandidatesIsNoOp(t *testing.T) {
	pf := newPrivacyFilter(t, privacySchemas(), map[string]string{})
	filtered, dropped, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "max-scope",
		SourceProject:  "internal-platform-x",
		Candidates:     nil,
	})
	if err != nil {
		t.Fatalf("FilterCrossProject: %v", err)
	}
	if filtered != nil || dropped != nil {
		t.Fatalf("empty candidates: want nil/nil, got filtered=%v dropped=%v", filtered, dropped)
	}
}

func TestPrivacyFilter_DropsAreUnique(t *testing.T) {
	pf := newPrivacyFilter(t, privacySchemas(), map[string]string{
		"internal-platform-x": "max-scope",
		"client-secret":       "capa-firewall",
	})
	results := []augment.QueryResult{
		{NoteID: "n1", ProjectID: "internal-platform-x", Source: "fts"},
		{NoteID: "n2", ProjectID: "client-secret", Source: "fts"},
		{NoteID: "n3", ProjectID: "client-secret", Source: "vec"},
		{NoteID: "n4", ProjectID: "client-secret", Source: "graph"},
	}
	_, dropped, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "max-scope",
		SourceProject:  "internal-platform-x",
		Candidates:     results,
	})
	if err != nil {
		t.Fatalf("FilterCrossProject: %v", err)
	}
	if len(dropped) != 1 || dropped[0] != "client-secret" {
		t.Fatalf("dropped: want unique [client-secret], got %v", dropped)
	}
}

func TestPrivacyFilter_AllRowsFilteredReturnsNil(t *testing.T) {
	pf := newPrivacyFilter(t, privacySchemas(), map[string]string{
		"client-secret": "capa-firewall",
	})
	results := []augment.QueryResult{
		{NoteID: "n1", ProjectID: "client-secret", Source: "fts"},
	}
	filtered, dropped, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "max-scope",
		SourceProject:  "internal-platform-x",
		Candidates:     results,
	})
	if err != nil {
		t.Fatalf("FilterCrossProject: %v", err)
	}

	if filtered != nil {
		t.Fatalf("expected nil filtered when all dropped, got %v", filtered)
	}
	if len(dropped) != 1 {
		t.Fatalf("expected 1 dropped, got %v", dropped)
	}
}

func TestPrivacyFilter_NilLoaderRejected(t *testing.T) {
	pf := augment.NewPrivacyFilter(nil, &projectDoctrineLookup{mp: map[string]string{}})
	_, _, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "x",
		Candidates:     []augment.QueryResult{{ProjectID: "p"}},
	})
	if err == nil || !contains(err.Error(), "loader nil") {
		t.Fatalf("expected loader-nil error, got %v", err)
	}
}

func TestPrivacyFilter_NilLookupRejected(t *testing.T) {
	pf := augment.NewPrivacyFilter(&fakeDoctrineLoader{schemas: privacySchemas()}, nil)
	_, _, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "max-scope",
		Candidates:     []augment.QueryResult{{ProjectID: "p"}},
	})
	if err == nil || !contains(err.Error(), "lookup nil") {
		t.Fatalf("expected lookup-nil error, got %v", err)
	}
}
