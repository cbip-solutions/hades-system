package merge

import (
	"errors"
	"testing"
)

func TestClassifyExecErrorNilReturnsUnknown(t *testing.T) {
	got := classifyExecError(nil, 0)
	if got != CandidateFailureUnknown {
		t.Errorf("classifyExecError(nil, 0) = %v want CandidateFailureUnknown", got)
	}
}

func TestClassifyExecErrorPositiveExitCodeReturnsPanic(t *testing.T) {
	got := classifyExecError(errors.New("test"), 1)
	if got != CandidateFailurePanic {
		t.Errorf("classifyExecError(non-timeout, exit=1) = %v want Panic", got)
	}
}

func TestErrIsTimeoutNilReturnsFalse(t *testing.T) {
	if errIsTimeout(nil) {
		t.Error("errIsTimeout(nil) = true want false")
	}
}

func TestErrIsTimeoutGenericErrorReturnsFalse(t *testing.T) {
	if errIsTimeout(errors.New("plain error")) {
		t.Error("errIsTimeout(generic) = true want false")
	}
}
