package scheduler

import (
	"errors"
	"strings"
	"testing"
)

func TestGenerateBearerToken_RandError(t *testing.T) {
	prev := randRead
	defer func() { randRead = prev }()
	randRead = func(_ []byte) (int, error) {
		return 0, errors.New("rand: synthetic")
	}
	raw, hash, err := GenerateBearerToken()
	if err == nil {
		t.Fatalf("GenerateBearerToken with broken rand: err = nil, want non-nil")
	}
	if raw != "" || hash != "" {
		t.Errorf("GenerateBearerToken with broken rand: raw=%q hash=%q, want both empty", raw, hash)
	}
	if !strings.Contains(err.Error(), "rand: synthetic") {
		t.Errorf("error did not wrap underlying rand error: %v", err)
	}
}
