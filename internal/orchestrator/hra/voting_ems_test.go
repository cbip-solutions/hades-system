package hra_test

import (
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
)

func TestEMS_ConvergesAt3of5(t *testing.T) {

	samples := []hra.ClassificationVote{
		{Class: hra.ClassNeedsFix},
		{Class: hra.ClassNeedsFix},
		{Class: hra.ClassNeedsFix},
	}
	d, converged, err := hra.EMSDecide(samples, 5)
	if err != nil {
		t.Fatalf("err=%v want nil", err)
	}
	if !converged {
		t.Fatal("converged=false, want true (3-of-5 unanimous needs_fix)")
	}
	if d.Winner != hra.ClassNeedsFix {
		t.Fatalf("Winner=%v want needs_fix", d.Winner)
	}
	if d.ForCount != 3 || d.Threshold != 3 {
		t.Errorf("ForCount=%d Threshold=%d, want 3/3", d.ForCount, d.Threshold)
	}
}

func TestEMS_NotConvergedAt2of5_2to0(t *testing.T) {

	samples := []hra.ClassificationVote{
		{Class: hra.ClassNeedsFix},
		{Class: hra.ClassNeedsFix},
	}
	_, converged, err := hra.EMSDecide(samples, 5)
	if err != nil {
		t.Fatalf("err=%v want nil", err)
	}
	if converged {
		t.Fatal("converged=true, want false (2 < threshold 3)")
	}
}

func TestEMS_NotConvergedAt3of5_2to1(t *testing.T) {

	samples := []hra.ClassificationVote{
		{Class: hra.ClassNeedsFix},
		{Class: hra.ClassNeedsFix},
		{Class: hra.ClassAck},
	}
	_, converged, err := hra.EMSDecide(samples, 5)
	if err != nil {
		t.Fatalf("err=%v want nil", err)
	}
	if converged {
		t.Fatal("converged=true, want false (2-1 not majority of 5)")
	}
}

func TestEMS_FullSampleTieEscalatesL3(t *testing.T) {

	samples := []hra.ClassificationVote{
		{Class: hra.ClassAck},
		{Class: hra.ClassAck},
		{Class: hra.ClassNeedsFix},
		{Class: hra.ClassNeedsFix},
	}
	_, _, err := hra.EMSDecide(samples, 4)
	if !errors.Is(err, hra.ErrPluralityTie) {
		t.Fatalf("err=%v want ErrPluralityTie", err)
	}
}

func TestEMS_FullSampleConvergesNormally(t *testing.T) {

	samples := []hra.ClassificationVote{
		{Class: hra.ClassAck}, {Class: hra.ClassAck}, {Class: hra.ClassAck},
		{Class: hra.ClassNeedsFix}, {Class: hra.ClassNeedsFix},
	}
	d, converged, err := hra.EMSDecide(samples, 5)
	if err != nil {
		t.Fatalf("err=%v want nil", err)
	}
	if !converged {
		t.Fatal("converged=false, want true (full sample with majority)")
	}
	if d.Winner != hra.ClassAck {
		t.Fatalf("Winner=%v want ack", d.Winner)
	}
}

func TestEMS_OverSampleIsError(t *testing.T) {

	samples := []hra.ClassificationVote{
		{Class: hra.ClassAck}, {Class: hra.ClassAck}, {Class: hra.ClassAck},
	}
	_, _, err := hra.EMSDecide(samples, 2)
	if err == nil {
		t.Fatal("expected error for samples > totalExpected, got nil")
	}
}

func TestEMS_ZeroOrNegativeTotalIsError(t *testing.T) {
	samples := []hra.ClassificationVote{{Class: hra.ClassAck}}
	for _, n := range []int{0, -1, -100} {
		if _, _, err := hra.EMSDecide(samples, n); err == nil {
			t.Errorf("totalExpected=%d returned nil err, want error", n)
		}
	}
}

func TestEMS_EmptySamplesIsError(t *testing.T) {
	_, _, err := hra.EMSDecide(nil, 5)
	if !errors.Is(err, hra.ErrNoVotes) {
		t.Fatalf("err=%v want ErrNoVotes", err)
	}
}

func TestEMS_UnknownClassIsError(t *testing.T) {
	samples := []hra.ClassificationVote{{Class: hra.Class("garbage")}}
	_, _, err := hra.EMSDecide(samples, 5)
	if !errors.Is(err, hra.ErrUnknownClass) {
		t.Fatalf("err=%v want ErrUnknownClass", err)
	}
}
