package worker_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/workforce/worker"
)

func TestRunResultZeroValue(t *testing.T) {
	r := worker.RunResult{}
	if r.Success {
		t.Error("zero RunResult must have Success=false")
	}
	if r.TokensUsed != 0 {
		t.Error("zero RunResult must have TokensUsed=0")
	}
	if r.CostUSD != 0 {
		t.Error("zero RunResult must have CostUSD=0")
	}
	if r.ToolUseCount != 0 {
		t.Error("zero RunResult must have ToolUseCount=0")
	}
	if r.CheckpointIDs != nil {
		t.Error("zero RunResult must have nil CheckpointIDs")
	}
	if r.Artifacts != nil {
		t.Error("zero RunResult must have nil Artifacts")
	}
}

func TestRunResultString(t *testing.T) {
	r := worker.RunResult{
		Success:         true,
		TokensUsed:      30,
		CostUSD:         0.001,
		ToolUseCount:    1,
		CheckpointIDs:   []string{"cp-1"},
		Artifacts:       []string{"foo.go"},
		FinalStopReason: "end_turn",
	}
	got := r.String()
	if !strings.Contains(got, "OK") {
		t.Errorf("String() = %q, want substring 'OK'", got)
	}
	if !strings.Contains(got, "tokens=30") {
		t.Errorf("String() = %q, want substring 'tokens=30'", got)
	}
	if !strings.Contains(got, "checkpoints=1") {
		t.Errorf("String() = %q, want substring 'checkpoints=1'", got)
	}

	rfail := worker.RunResult{Success: false, FinalStopReason: "max_tokens"}
	if !strings.Contains(rfail.String(), "FAIL") {
		t.Errorf("String() for failure = %q, want substring 'FAIL'", rfail.String())
	}
}

func TestErrSentinels(t *testing.T) {
	cases := []error{
		worker.ErrNilWorktreePath,
		worker.ErrNilSession,
		worker.ErrNilQueues,
		worker.ErrNilDoctrineConfig,
		worker.ErrNilToolRelay,
		worker.ErrTaskNotFound,
		worker.ErrTaskAlreadyClaimed,
		worker.ErrQuotaExceeded,
		worker.ErrToolNotAvailable,
		worker.ErrToolNotInWhitelist,
		worker.ErrUnknownToolFamily,
	}
	for i, e := range cases {
		if e == nil {
			t.Errorf("sentinel %d is nil", i)
		}
	}

	wrapped := errors.New("plain")
	if errors.Is(wrapped, worker.ErrNilWorktreePath) {
		t.Error("plain string-wrap should NOT match the sentinel")
	}
}

func TestRunRequestValidate(t *testing.T) {
	good := worker.RunRequest{
		TaskID: "task-1",
		Prompt: "implement feature X",
	}
	if err := good.Validate(); err != nil {
		t.Errorf("Validate(good): %v", err)
	}
	bad := []worker.RunRequest{
		{TaskID: "", Prompt: "x"},
		{TaskID: "x", Prompt: ""},
		{TaskID: "  ", Prompt: "x"},
		{TaskID: "x", Prompt: "  "},
	}
	for i, r := range bad {
		if err := r.Validate(); err == nil {
			t.Errorf("Validate(bad %d, %+v): expected error", i, r)
		}
	}
}

func TestWorkerInterfaceCompiles(t *testing.T) {
	var _ worker.Worker = (*nopWorker)(nil)
}

type nopWorker struct{}

func (nopWorker) Run(ctx context.Context, req worker.RunRequest) (worker.RunResult, error) {
	return worker.RunResult{Success: true}, nil
}

func TestWorkerInterfaceAcceptsCtx(t *testing.T) {
	w := nopWorker{}
	res, err := w.Run(context.Background(), worker.RunRequest{TaskID: "t", Prompt: "p"})
	if err != nil {
		t.Errorf("nopWorker.Run: %v", err)
	}
	if !res.Success {
		t.Error("nopWorker.Run.Success = false")
	}
}
