package safetynet

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeHealthWriter struct {
	mu        sync.Mutex
	records   []HealthRecord
	insertErr error
	recentErr error
	insertCnt int
}

func (f *fakeHealthWriter) Insert(_ context.Context, r HealthRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.insertCnt++
	if f.insertErr != nil {
		return f.insertErr
	}
	f.records = append(f.records, r)
	return nil
}

func (f *fakeHealthWriter) Recent(_ context.Context, author string, since time.Time) ([]HealthRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recentErr != nil {
		return nil, f.recentErr
	}
	out := []HealthRecord{}
	for _, r := range f.records {
		if r.AuthoredBy == author && time.Unix(r.RecordedAt, 0).After(since) {
			out = append(out, r)
		}
	}
	return out, nil
}

func TestRegression_RecordPersists(t *testing.T) {
	t.Parallel()
	w := &fakeHealthWriter{}
	em := &fakeEmitter{}
	r := NewRegression(w, em, 0.8)
	rec := HealthRecord{
		CommitSHA: "abc123", AuthoredBy: "substrate",
		TestPassRate: 1.0, TestTotal: 10, TestPassed: 10,
		DoctrineLintPass: true, RecordedAt: time.Now().Unix(),
	}
	if err := r.Record(context.Background(), rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(w.records) != 1 {
		t.Fatalf("len=%d want 1", len(w.records))
	}
	if len(em.events) != 0 {
		t.Fatalf("no alarm above threshold; got %+v", em.events)
	}
}

func TestRegression_BelowThreshold_EmitsAlarm(t *testing.T) {
	t.Parallel()
	w := &fakeHealthWriter{}
	em := &fakeEmitter{}
	r := NewRegression(w, em, 0.8)
	rec := HealthRecord{
		CommitSHA: "bad456", AuthoredBy: "substrate",
		TestPassRate: 0.5, TestTotal: 10, TestPassed: 5,
		DoctrineLintPass: false, RecordedAt: time.Now().Unix(),
	}
	if err := r.Record(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if len(em.events) != 1 || em.events[0].Type != EventRegressionBySelfAlarm {
		t.Fatalf("expected RegressionBySelfAlarm; got %+v", em.events)
	}
}

func TestRegression_LintFailAlone_EmitsAlarm(t *testing.T) {

	t.Parallel()
	w := &fakeHealthWriter{}
	em := &fakeEmitter{}
	r := NewRegression(w, em, 0.8)
	rec := HealthRecord{
		CommitSHA: "lint-fail", AuthoredBy: "substrate",
		TestPassRate: 1.0, TestTotal: 5, TestPassed: 5,
		DoctrineLintPass: false, RecordedAt: time.Now().Unix(),
	}
	if err := r.Record(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if len(em.events) != 1 {
		t.Fatalf("want alarm on lint-fail; got %+v", em.events)
	}
}

func TestRegression_Query_FiltersByAuthor(t *testing.T) {
	t.Parallel()
	w := &fakeHealthWriter{}
	r := NewRegression(w, &fakeEmitter{}, 0.8)
	now := time.Now().Unix()
	if err := r.Record(context.Background(), HealthRecord{CommitSHA: "s1", AuthoredBy: "substrate", TestPassRate: 1.0, DoctrineLintPass: true, RecordedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := r.Record(context.Background(), HealthRecord{CommitSHA: "o1", AuthoredBy: "operator", TestPassRate: 1.0, DoctrineLintPass: true, RecordedAt: now}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Query(context.Background(), "substrate", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].CommitSHA != "s1" {
		t.Fatalf("Query author=substrate = %v", got)
	}
}

func TestRegression_Validate_RejectsBadAuthor(t *testing.T) {
	t.Parallel()
	r := NewRegression(&fakeHealthWriter{}, &fakeEmitter{}, 0.8)
	err := r.Record(context.Background(), HealthRecord{
		CommitSHA: "x", AuthoredBy: "rogue", TestPassRate: 1.0, RecordedAt: 1,
	})
	if !errors.Is(err, ErrRegressionInvalidAuthor) {
		t.Fatalf("want ErrRegressionInvalidAuthor; got %v", err)
	}
}

func TestRegression_Validate_RejectsBadRate(t *testing.T) {
	t.Parallel()
	r := NewRegression(&fakeHealthWriter{}, &fakeEmitter{}, 0.8)
	cases := []float64{-0.1, 1.5, 100.0}
	for _, rate := range cases {
		err := r.Record(context.Background(), HealthRecord{
			CommitSHA: "x", AuthoredBy: "substrate", TestPassRate: rate, RecordedAt: 1,
		})
		if !errors.Is(err, ErrRegressionInvalidRate) {
			t.Errorf("rate=%v: want ErrRegressionInvalidRate; got %v", rate, err)
		}
	}
}

func TestRegression_Insert_ErrorPropagates(t *testing.T) {
	t.Parallel()
	w := &fakeHealthWriter{insertErr: errors.New("disk full")}
	r := NewRegression(w, &fakeEmitter{}, 0.8)
	err := r.Record(context.Background(), HealthRecord{
		CommitSHA: "x", AuthoredBy: "substrate", TestPassRate: 1.0,
		DoctrineLintPass: true, RecordedAt: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "insert") {
		t.Fatalf("want insert error, got %v", err)
	}
}

func TestRegression_Query_ErrorPropagates(t *testing.T) {
	t.Parallel()
	w := &fakeHealthWriter{recentErr: errors.New("query failed")}
	r := NewRegression(w, &fakeEmitter{}, 0.8)
	_, err := r.Query(context.Background(), "substrate", time.Now())
	if err == nil {
		t.Fatal("want error")
	}
}

func TestRegression_BoundaryNoStoreImport(t *testing.T) {
	t.Parallel()
	var _ HealthWriter = (*fakeHealthWriter)(nil)
}
