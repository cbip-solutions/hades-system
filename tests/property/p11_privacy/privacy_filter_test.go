// go:build property
package p11_privacy

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

type fixedLoader struct {
	schema augment.DoctrineSchema
}

func (l *fixedLoader) Load(_ context.Context, _ string) (*augment.DoctrineSchema, error) {
	s := l.schema
	return &s, nil
}

type fixedLookup struct {
	doctrine string
}

func (l *fixedLookup) DoctrineForProject(_ context.Context, _ string) (string, error) {
	return l.doctrine, nil
}

type failingLookup struct{}

func (failingLookup) DoctrineForProject(_ context.Context, _ string) (string, error) {
	return "", errors.New("lookup unavailable")
}

func TestPrivacyFilter_SelfProjectAlwaysPasses(t *testing.T) {
	loader := &fixedLoader{
		schema: augment.DoctrineSchema{

			KnowledgeCrossProject: augment.CrossProjectAxis{QueriesCanReach: nil},
		},
	}
	lookup := &fixedLookup{doctrine: "other"}
	pf := augment.NewPrivacyFilter(loader, lookup)

	prop := func(n uint8) bool {
		count := int(n%10 + 1)
		candidates := make([]augment.QueryResult, count)
		for i := 0; i < count; i++ {
			candidates[i] = augment.QueryResult{
				NoteID:    fmt.Sprintf("n-%d", i),
				ProjectID: "p-source",
			}
		}
		filtered, _, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
			SourceDoctrine: "default",
			SourceProject:  "p-source",
			Candidates:     candidates,
		})
		if err != nil {
			return false
		}
		return len(filtered) == count
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("self-project always-passes violated: %v", err)
	}
}

func TestPrivacyFilter_EmptyInputEmptyOutput(t *testing.T) {
	pf := augment.NewPrivacyFilter(
		&fixedLoader{},
		&fixedLookup{doctrine: "default"},
	)
	filtered, dropped, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "default",
		SourceProject:  "p",
		Candidates:     nil,
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if filtered != nil {
		t.Errorf("filtered = %v, want nil", filtered)
	}
	if dropped != nil {
		t.Errorf("dropped = %v, want nil", dropped)
	}
}

func TestPrivacyFilter_ConservativeDropOnLookupError(t *testing.T) {
	loader := &fixedLoader{
		schema: augment.DoctrineSchema{
			KnowledgeCrossProject: augment.CrossProjectAxis{QueriesCanReach: []string{"default"}},
		},
	}
	pf := augment.NewPrivacyFilter(loader, failingLookup{})

	candidates := []augment.QueryResult{
		{NoteID: "self", ProjectID: "p-source"},
		{NoteID: "x-self", ProjectID: "p-source"},
		{NoteID: "cross-1", ProjectID: "p-other-1"},
		{NoteID: "cross-2", ProjectID: "p-other-2"},
	}
	filtered, dropped, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "default",
		SourceProject:  "p-source",
		Candidates:     candidates,
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(filtered) != 2 {
		t.Errorf("filtered len = %d, want 2 (both self-project)", len(filtered))
	}
	for _, r := range filtered {
		if r.ProjectID != "p-source" {
			t.Errorf("filtered row has ProjectID=%q, want p-source", r.ProjectID)
		}
	}
	if len(dropped) != 2 {
		t.Errorf("dropped len = %d, want 2", len(dropped))
	}
}

func TestPrivacyFilter_PreservesInputOrder(t *testing.T) {
	loader := &fixedLoader{
		schema: augment.DoctrineSchema{
			KnowledgeCrossProject: augment.CrossProjectAxis{QueriesCanReach: []string{"default"}},
		},
	}
	pf := augment.NewPrivacyFilter(loader, &fixedLookup{doctrine: "default"})

	candidates := []augment.QueryResult{
		{NoteID: "a", ProjectID: "p-source"},
		{NoteID: "b", ProjectID: "p-other"},
		{NoteID: "c", ProjectID: "p-source"},
		{NoteID: "d", ProjectID: "p-other"},
	}
	filtered, _, err := pf.FilterCrossProject(context.Background(), augment.PrivacyFilterInput{
		SourceDoctrine: "default",
		SourceProject:  "p-source",
		Candidates:     candidates,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	wantIDs := []string{"a", "b", "c", "d"}
	gotIDs := make([]string, len(filtered))
	for i, r := range filtered {
		gotIDs[i] = r.NoteID
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Errorf("order drift: got %v, want %v", gotIDs, wantIDs)
	}
}
