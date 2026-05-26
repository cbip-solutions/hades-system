package hra_test

import (
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
)

func TestPlurality_Tie_2Reviewers_1to1(t *testing.T) {

	votes := []hra.ClassificationVote{
		{ReviewerID: "R1", Class: hra.ClassAck},
		{ReviewerID: "R2", Class: hra.ClassNeedsFix},
	}
	_, err := hra.Plurality(votes)
	if !errors.Is(err, hra.ErrPluralityTie) {
		t.Fatalf("err=%v want ErrPluralityTie", err)
	}
}

func TestPlurality_Tie_4Reviewers_2to2(t *testing.T) {

	votes := []hra.ClassificationVote{
		{ReviewerID: "R1", Class: hra.ClassAck},
		{ReviewerID: "R2", Class: hra.ClassAck},
		{ReviewerID: "R3", Class: hra.ClassNeedsFix},
		{ReviewerID: "R4", Class: hra.ClassNeedsFix},
	}
	_, err := hra.Plurality(votes)
	if !errors.Is(err, hra.ErrPluralityTie) {
		t.Fatalf("err=%v want ErrPluralityTie", err)
	}
}

func TestPlurality_Tie_6Reviewers_3to3(t *testing.T) {

	votes := []hra.ClassificationVote{
		{ReviewerID: "R1", Class: hra.ClassAck}, {ReviewerID: "R2", Class: hra.ClassAck}, {ReviewerID: "R3", Class: hra.ClassAck},
		{ReviewerID: "R4", Class: hra.ClassNeedsFix}, {ReviewerID: "R5", Class: hra.ClassNeedsFix}, {ReviewerID: "R6", Class: hra.ClassNeedsFix},
	}
	_, err := hra.Plurality(votes)
	if !errors.Is(err, hra.ErrPluralityTie) {
		t.Fatalf("err=%v want ErrPluralityTie", err)
	}
}

func TestPlurality_NoTieAt5Reviewers_3to2(t *testing.T) {

	votes := []hra.ClassificationVote{
		{ReviewerID: "R1", Class: hra.ClassAck}, {ReviewerID: "R2", Class: hra.ClassAck}, {ReviewerID: "R3", Class: hra.ClassAck},
		{ReviewerID: "R4", Class: hra.ClassNeedsFix}, {ReviewerID: "R5", Class: hra.ClassNeedsFix},
	}
	d, err := hra.Plurality(votes)
	if err != nil {
		t.Fatalf("err=%v want nil", err)
	}
	if d.Winner != hra.ClassAck {
		t.Fatalf("Winner=%v want ack", d.Winner)
	}
}

func TestPlurality_TieIsErrorsIs(t *testing.T) {
	_, err := hra.Plurality([]hra.ClassificationVote{
		{ReviewerID: "R1", Class: hra.ClassAck},
		{ReviewerID: "R2", Class: hra.ClassNeedsFix},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, hra.ErrPluralityTie) {
		t.Fatalf("errors.Is(err, ErrPluralityTie) = false; err=%v", err)
	}
}
