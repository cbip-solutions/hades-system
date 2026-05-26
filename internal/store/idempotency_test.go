package store

import (
	"bytes"
	"errors"
	"testing"
)

func TestIdempotencyMarkPendingThenCompleted(t *testing.T) {
	s := setupStoreT(t)
	if err := s.MarkIdempotencyPending("k1", []byte("rh"), 100, 100+86400); err != nil {
		t.Fatalf("MarkPending: %v", err)
	}
	got, err := s.GetIdempotency("k1")
	if err != nil || got == nil {
		t.Fatalf("Get: got=%v err=%v", got, err)
	}
	if got.Status != "pending" || !bytes.Equal(got.RequestHash, []byte("rh")) {
		t.Errorf("got %+v", got)
	}
	if err := s.MarkIdempotencyCompleted("k1", 200, `{"x":"y"}`, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	got, _ = s.GetIdempotency("k1")
	if got == nil {
		t.Fatal("expected entry post-completed")
	}
	if got.Status != "completed" || got.ResponseStatusCode != 200 {
		t.Errorf("got %+v", got)
	}
	if got.ResponseHeaders != `{"x":"y"}` || !bytes.Equal(got.ResponseBody, []byte(`{"ok":true}`)) {
		t.Errorf("body/headers mismatch: %+v", got)
	}
}

func TestIdempotencyMarkFailed(t *testing.T) {
	s := setupStoreT(t)
	if err := s.MarkIdempotencyPending("kf", []byte("rh"), 1, 86401); err != nil {
		t.Fatalf("MarkPending: %v", err)
	}
	if err := s.MarkIdempotencyFailed("kf", "upstream 500"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	got, _ := s.GetIdempotency("kf")
	if got == nil || got.Status != "failed" {
		t.Errorf("got %+v", got)
	}
	if got.ErrorMessage != "upstream 500" {
		t.Errorf("ErrorMessage = %q, want %q", got.ErrorMessage, "upstream 500")
	}
}

func TestGetIdempotencyMissingReturnsNilNoError(t *testing.T) {
	s := setupStoreT(t)
	got, err := s.GetIdempotency("nope")
	if err != nil || got != nil {
		t.Errorf("got=%v err=%v", got, err)
	}
}

func TestPurgeExpiredIdempotency(t *testing.T) {
	s := setupStoreT(t)
	if err := s.MarkIdempotencyPending("e1", []byte("h"), 1, 50); err != nil {
		t.Fatalf("MarkPending e1: %v", err)
	}
	if err := s.MarkIdempotencyPending("e2", []byte("h"), 1, 60); err != nil {
		t.Fatalf("MarkPending e2: %v", err)
	}
	if err := s.MarkIdempotencyPending("fresh", []byte("h"), 1, 1_000_000_000); err != nil {
		t.Fatalf("MarkPending fresh: %v", err)
	}
	n, err := s.PurgeExpiredIdempotency(100)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if n != 2 {
		t.Errorf("purged = %d, want 2", n)
	}
	if got, _ := s.GetIdempotency("fresh"); got == nil {
		t.Error("fresh row should remain")
	}
}

func TestMarkCompletedUnknownReturnsErr(t *testing.T) {
	s := setupStoreT(t)
	err := s.MarkIdempotencyCompleted("nope", 200, "{}", []byte("x"))
	if !errors.Is(err, ErrIdempotencyNotFound) {
		t.Errorf("err = %v, want ErrIdempotencyNotFound", err)
	}
}

func TestMarkFailedUnknownReturnsErr(t *testing.T) {
	s := setupStoreT(t)
	err := s.MarkIdempotencyFailed("nope", "irrelevant")
	if !errors.Is(err, ErrIdempotencyNotFound) {
		t.Errorf("err = %v, want ErrIdempotencyNotFound", err)
	}
}

func TestMarkCompletedRejectsNonPending(t *testing.T) {
	s := setupStoreT(t)
	if err := s.MarkIdempotencyPending("k", []byte("rh"), 1, 86401); err != nil {
		t.Fatalf("MarkPending: %v", err)
	}
	if err := s.MarkIdempotencyFailed("k", "upstream timeout"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	err := s.MarkIdempotencyCompleted("k", 200, "{}", []byte("body"))
	if !errors.Is(err, ErrIdempotencyNotFound) {
		t.Errorf("err = %v, want ErrIdempotencyNotFound", err)
	}
	got, _ := s.GetIdempotency("k")
	if got == nil || got.Status != "failed" {
		t.Errorf("status drifted: %+v", got)
	}
	if got.ErrorMessage != "upstream timeout" {
		t.Errorf("ErrorMessage clobbered: %q", got.ErrorMessage)
	}
}

func TestMarkFailedRejectsNonPending(t *testing.T) {
	s := setupStoreT(t)
	if err := s.MarkIdempotencyPending("k", []byte("rh"), 1, 86401); err != nil {
		t.Fatalf("MarkPending: %v", err)
	}
	if err := s.MarkIdempotencyCompleted("k", 200, "{}", []byte("body")); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	err := s.MarkIdempotencyFailed("k", "late failure")
	if !errors.Is(err, ErrIdempotencyNotFound) {
		t.Errorf("err = %v, want ErrIdempotencyNotFound", err)
	}
	got, _ := s.GetIdempotency("k")
	if got == nil || got.Status != "completed" {
		t.Errorf("status drifted: %+v", got)
	}
}

func TestMarkPendingReplacesExisting(t *testing.T) {
	s := setupStoreT(t)
	if err := s.MarkIdempotencyPending("k", []byte("rh1"), 1, 100); err != nil {
		t.Fatalf("MarkPending #1: %v", err)
	}
	if err := s.MarkIdempotencyCompleted("k", 200, `{}`, []byte("body")); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := s.MarkIdempotencyPending("k", []byte("rh2"), 5, 200); err != nil {
		t.Fatalf("MarkPending #2: %v", err)
	}
	got, err := s.GetIdempotency("k")
	if err != nil || got == nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "pending" {
		t.Errorf("Status = %q, want pending", got.Status)
	}
	if !bytes.Equal(got.RequestHash, []byte("rh2")) {
		t.Errorf("RequestHash = %q, want rh2", got.RequestHash)
	}
	if got.ResponseStatusCode != 0 || got.ResponseBody != nil {
		t.Errorf("response state should be reset, got %+v", got)
	}
}
