package store

import (
	"testing"
	"time"
)

func TestTierHealthSamples_InsertAndQuery(t *testing.T) {
	db := newTestStore(t).DB()
	base := time.UnixMilli(1700000000000)
	for i, s := range []TierHealthSampleRow{
		{TS: base, Provider: "deepseek-direct", Tier: "openai-compat", Success: true, LatencyMS: 120},
		{TS: base.Add(time.Second), Provider: "deepseek-direct", Tier: "openai-compat", Success: false, LatencyMS: 90, ErrorPattern: "5xx"},
		{TS: base.Add(2 * time.Second), Provider: "gemini-flash", Tier: "gemini", Success: true, LatencyMS: 200},
	} {
		if err := InsertTierHealthSample(db, s); err != nil {
			t.Fatalf("InsertTierHealthSample[%d]: %v", i, err)
		}
	}
	got, err := QueryTierHealthSamples(db, "deepseek-direct", time.UnixMilli(0))
	if err != nil {
		t.Fatalf("QueryTierHealthSamples: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d samples for deepseek-direct, want 2", len(got))
	}
	if got[0].Provider != "deepseek-direct" || got[1].ErrorPattern != "5xx" {
		t.Errorf("unexpected rows: %+v", got)
	}
	if !got[0].TS.Equal(base) {
		t.Errorf("TS round-trip failed: got %v, want %v", got[0].TS, base)
	}
}

func TestInsertTierHealthSample_RejectsEmptyProvider(t *testing.T) {
	db := newTestStore(t).DB()
	err := InsertTierHealthSample(db, TierHealthSampleRow{
		TS: time.Now(), Provider: "", Tier: "gemini", Success: true,
	})
	if err == nil {
		t.Fatal("InsertTierHealthSample with empty provider: want error, got nil")
	}
}

func TestInsertTierHealthSample_RejectsEmptyTier(t *testing.T) {
	db := newTestStore(t).DB()
	err := InsertTierHealthSample(db, TierHealthSampleRow{
		TS: time.Now(), Provider: "deepseek-direct", Tier: "", Success: true,
	})
	if err == nil {
		t.Fatal("InsertTierHealthSample with empty tier: want error, got nil")
	}
}

func TestInsertTierHealthSample_DBError(t *testing.T) {
	s := newTestStore(t)
	db := s.DB()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := InsertTierHealthSample(db, TierHealthSampleRow{
		TS: time.Now(), Provider: "deepseek-direct", Tier: "openai-compat", Success: true,
	})
	if err == nil {
		t.Fatal("InsertTierHealthSample on closed DB: want error, got nil")
	}
}

func TestQueryTierHealthSamples_DBError(t *testing.T) {
	s := newTestStore(t)
	db := s.DB()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := QueryTierHealthSamples(db, "deepseek-direct", time.UnixMilli(0))
	if err == nil {
		t.Fatal("QueryTierHealthSamples on closed DB: want error, got nil")
	}
}

func TestQueryTierHealthSamples_EmptyResult(t *testing.T) {
	db := newTestStore(t).DB()
	got, err := QueryTierHealthSamples(db, "no-such-provider", time.UnixMilli(0))
	if err != nil {
		t.Fatalf("QueryTierHealthSamples: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d samples, want 0", len(got))
	}
}
