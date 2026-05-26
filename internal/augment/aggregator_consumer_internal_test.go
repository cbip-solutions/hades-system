package augment

import "testing"

func TestIsVecUnavailableErr_NilErr(t *testing.T) {
	if isVecUnavailableErr(nil) {
		t.Error("nil err should return false")
	}
}

func TestIsVecUnavailableErr_NoMatch(t *testing.T) {

	if isVecUnavailableErr(simpleErr("random db corruption")) {
		t.Error("non-matching err should return false")
	}
}

func TestAtoiSimple_Empty(t *testing.T) {
	_, err := atoiSimple("")
	if err == nil {
		t.Error("empty string should return error")
	}
}

func TestAtoiSimple_Valid(t *testing.T) {
	got, err := atoiSimple("2026")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 2026 {
		t.Errorf("want 2026, got %d", got)
	}
}

type simpleErr string

func (e simpleErr) Error() string { return string(e) }
