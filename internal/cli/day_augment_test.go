package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/zenday"
)

type fakeAugmentDayClient struct {
	morning zenday.BriefDoc
}

func (f *fakeAugmentDayClient) GenerateMorning(_ context.Context, _ bool) (zenday.BriefDoc, error) {
	return f.morning, nil
}

func (f *fakeAugmentDayClient) GenerateEOD(_ context.Context, _ bool) (zenday.BriefDoc, error) {
	return f.morning, nil
}

func (f *fakeAugmentDayClient) CheckPending(_ context.Context) (zenday.BriefDoc, error) {
	return f.morning, nil
}

func TestZenDayMorningRendersAugmentationSection(t *testing.T) {
	t.Parallel()
	doc := zenday.BriefDoc{
		Date: time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC),
		Type: zenday.BriefTypeMorning,
		Augmentation: &zenday.AugmentationSection{
			TotalCostUSD:       0.42,
			TokensConsumed:     8234,
			TokensCeiling:      25000,
			KGQueriesFired:     47,
			CacheHitRate:       0.62,
			LastIndexedRFC3339: "2026-05-09T14:00:00Z",
		},
	}
	fake := &fakeAugmentDayClient{morning: doc}
	var buf bytes.Buffer
	if err := runDay(context.Background(), fake, &buf, false, false, false); err != nil {
		t.Fatalf("runDay: %v", err)
	}
	out := buf.String()
	wantSubs := []string{
		"Augmentation",
		"$0.42",
		"8,234",
		"25,000",
		"47",
		"62%",
		"2026-05-09T14:00:00Z",
	}
	for _, s := range wantSubs {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q\nfull:\n%s", s, out)
		}
	}
}

func TestZenDayMorningRendersZeroAugmentationGracefully(t *testing.T) {
	t.Parallel()
	doc := zenday.BriefDoc{
		Date:         time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC),
		Type:         zenday.BriefTypeMorning,
		Augmentation: &zenday.AugmentationSection{},
	}
	fake := &fakeAugmentDayClient{morning: doc}
	var buf bytes.Buffer
	if err := runDay(context.Background(), fake, &buf, false, false, false); err != nil {
		t.Fatalf("runDay: %v", err)
	}
	if !strings.Contains(buf.String(), "Augmentation") {
		t.Fatalf("zero-augmentation should still render the section header; got %q", buf.String())
	}
}

func TestZenDayMorningRendersKnowledgeSection(t *testing.T) {
	t.Parallel()
	doc := zenday.BriefDoc{
		Date: time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC),
		Type: zenday.BriefTypeMorning,
		Knowledge: &zenday.KnowledgeSection{
			FTS5Docs:                    5234,
			FTS5DocsDeltaSinceYesterday: 12,
			AggregatorDBSizeMB:          142,
			PromoteToday:                3,
			CrossProjectQueries:         17,
			LitestreamReplicaLagSec:     23,
		},
	}
	fake := &fakeAugmentDayClient{morning: doc}
	var buf bytes.Buffer
	if err := runDay(context.Background(), fake, &buf, false, false, false); err != nil {
		t.Fatalf("runDay: %v", err)
	}
	out := buf.String()
	wantSubs := []string{
		"Knowledge",
		"5,234",
		"142",
		"3",
		"17",
		"23s",
	}
	for _, s := range wantSubs {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q\nfull:\n%s", s, out)
		}
	}
}

func TestZenDayMorningKnowledgeReplicaLagWarn(t *testing.T) {
	t.Parallel()
	doc := zenday.BriefDoc{
		Date: time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC),
		Type: zenday.BriefTypeMorning,
		Knowledge: &zenday.KnowledgeSection{
			FTS5Docs:                5234,
			LitestreamReplicaLagSec: 120,
		},
	}
	fake := &fakeAugmentDayClient{morning: doc}
	var buf bytes.Buffer
	_ = runDay(context.Background(), fake, &buf, false, false, false)
	if !strings.Contains(buf.String(), "WARN") {
		t.Errorf("replica lag >= 60s must surface WARN; got %q", buf.String())
	}
}

func TestZenDayMorningRendersNotificationsSection(t *testing.T) {
	t.Parallel()
	doc := zenday.BriefDoc{
		Date: time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC),
		Type: zenday.BriefTypeMorning,
		Notifications: &zenday.NotificationsSection{
			RoutesActive:         []string{"slack", "email"},
			PendingAcks:          2,
			CostCap50Alerts:      1,
			CostCap80Alerts:      0,
			CostCap100Alerts:     0,
			CaronteHealthDigests: 3,
			HermesDispatchErrors: 0,
		},
	}
	fake := &fakeAugmentDayClient{morning: doc}
	var buf bytes.Buffer
	if err := runDay(context.Background(), fake, &buf, false, false, false); err != nil {
		t.Fatalf("runDay: %v", err)
	}
	out := buf.String()
	wantSubs := []string{
		"Notifications",
		"slack, email",
		"pending_acks: 2",
		"50%=1",
	}
	for _, s := range wantSubs {
		if !strings.Contains(out, s) {
			t.Errorf("output missing %q\nfull:\n%s", s, out)
		}
	}
}

func TestZenDayMorningNotificationsCostCap100Surfaces(t *testing.T) {
	t.Parallel()
	doc := zenday.BriefDoc{
		Date: time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC),
		Type: zenday.BriefTypeMorning,
		Notifications: &zenday.NotificationsSection{
			CostCap100Alerts: 1,
		},
	}
	fake := &fakeAugmentDayClient{morning: doc}
	var buf bytes.Buffer
	_ = runDay(context.Background(), fake, &buf, false, false, false)
	if !strings.Contains(buf.String(), "100%=1") {
		t.Errorf("cost cap 100%% must surface; got %q", buf.String())
	}
}

func TestZenDayMorningNotificationsHermesErrorsSurface(t *testing.T) {
	t.Parallel()
	doc := zenday.BriefDoc{
		Date: time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC),
		Type: zenday.BriefTypeMorning,
		Notifications: &zenday.NotificationsSection{
			HermesDispatchErrors: 5,
		},
	}
	fake := &fakeAugmentDayClient{morning: doc}
	var buf bytes.Buffer
	_ = runDay(context.Background(), fake, &buf, false, false, false)
	if !strings.Contains(buf.String(), "hermes_dispatch_errors: 5") {
		t.Errorf("hermes errors must surface; got %q", buf.String())
	}
}
