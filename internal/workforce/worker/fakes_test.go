package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/workforce/queue"
	"github.com/cbip-solutions/hades-system/internal/workforce/subprocess"
	"github.com/cbip-solutions/hades-system/internal/workforce/worker"
)

type fakeSharedTaskList struct {
	mu       sync.Mutex
	rows     map[string]queue.TaskRow
	enqErr   error
	advErr   error
	claimErr error
}

func newFakeSharedTaskList() *fakeSharedTaskList {
	return &fakeSharedTaskList{rows: map[string]queue.TaskRow{}}
}

func taskKey(projectID string, id queue.TaskID) string {
	return projectID + ":" + string(id)
}

func (f *fakeSharedTaskList) Enqueue(ctx context.Context, row queue.TaskRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enqErr != nil {
		err := f.enqErr
		f.enqErr = nil
		return err
	}
	k := taskKey(row.ProjectID, row.TaskID)
	if _, exists := f.rows[k]; exists {
		return queue.ErrDuplicateTask
	}
	now := time.Now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	if row.Status == 0 {
		row.Status = queue.StatusPending
	}
	f.rows[k] = row
	return nil
}

func (f *fakeSharedTaskList) Claim(ctx context.Context, taskID queue.TaskID, threadID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimErr != nil {
		err := f.claimErr
		f.claimErr = nil
		return err
	}
	for k, row := range f.rows {
		if row.TaskID != taskID {
			continue
		}
		if row.Status != queue.StatusPending {
			return queue.ErrTaskNotPending
		}
		row.Status = queue.StatusInProgress
		row.ThreadID = threadID
		row.UpdatedAt = time.Now().UTC()
		f.rows[k] = row
		return nil
	}
	return queue.ErrTaskNotFound
}

func (f *fakeSharedTaskList) Advance(ctx context.Context, taskID queue.TaskID, newStatus queue.Status) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.advErr != nil {
		err := f.advErr
		f.advErr = nil
		return err
	}
	for k, row := range f.rows {
		if row.TaskID != taskID {
			continue
		}
		if !queue.IsValidTransition(row.Status, newStatus) {
			return queue.ErrInvalidTransition
		}
		row.Status = newStatus
		row.UpdatedAt = time.Now().UTC()
		f.rows[k] = row
		return nil
	}
	return queue.ErrTaskNotFound
}

func (f *fakeSharedTaskList) Get(ctx context.Context, taskID queue.TaskID) (queue.TaskRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.rows {
		if row.TaskID == taskID {
			return row, nil
		}
	}
	return queue.TaskRow{}, queue.ErrTaskNotFound
}

func (f *fakeSharedTaskList) ListByStatus(ctx context.Context, projectID string, status queue.Status) ([]queue.TaskRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []queue.TaskRow{}
	for _, row := range f.rows {
		if row.ProjectID == projectID && row.Status == status {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeSharedTaskList) ByThread(ctx context.Context, threadID string) ([]queue.TaskRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []queue.TaskRow{}
	for _, row := range f.rows {
		if row.ThreadID == threadID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeSharedTaskList) setNextClaimErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimErr = err
}

func (f *fakeSharedTaskList) setNextAdvanceErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.advErr = err
}

type fakeCheckpointQueue struct {
	mu       sync.Mutex
	rows     []queue.Checkpoint
	failNext bool
	nextErr  error
}

func newFakeCheckpointQueue() *fakeCheckpointQueue {
	return &fakeCheckpointQueue{}
}

func (f *fakeCheckpointQueue) Put(ctx context.Context, cp queue.Checkpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNext {
		f.failNext = false
		err := f.nextErr
		f.nextErr = nil
		return err
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	f.rows = append(f.rows, cp)
	return nil
}

func (f *fakeCheckpointQueue) Drain(ctx context.Context, taskID queue.TaskID) ([]queue.Checkpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []queue.Checkpoint{}
	for i, cp := range f.rows {
		if cp.TaskID == taskID && !cp.Consumed {
			cp.Consumed = true
			f.rows[i] = cp
			out = append(out, cp)
		}
	}
	return out, nil
}

func (f *fakeCheckpointQueue) Peek(ctx context.Context, taskID queue.TaskID) ([]queue.Checkpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []queue.Checkpoint{}
	for _, cp := range f.rows {
		if cp.TaskID == taskID && !cp.Consumed {
			out = append(out, cp)
		}
	}
	return out, nil
}

func (f *fakeCheckpointQueue) ByThread(ctx context.Context, threadID string) ([]queue.Checkpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []queue.Checkpoint{}
	for _, cp := range f.rows {
		if cp.ThreadID == threadID {
			out = append(out, cp)
		}
	}
	return out, nil
}

func (f *fakeCheckpointQueue) setFailNextPut(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext = true
	f.nextErr = err
}

func (f *fakeCheckpointQueue) snapshot() []queue.Checkpoint {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]queue.Checkpoint, len(f.rows))
	copy(out, f.rows)
	return out
}

type fakeFixPromptQueue struct {
	mu   sync.Mutex
	rows []queue.FixPrompt
}

func newFakeFixPromptQueue() *fakeFixPromptQueue {
	return &fakeFixPromptQueue{}
}

func (f *fakeFixPromptQueue) Put(ctx context.Context, fp queue.FixPrompt) error {

	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if fp.CreatedAt.IsZero() {
		fp.CreatedAt = time.Now().UTC()
	}
	f.rows = append(f.rows, fp)
	return nil
}

func (f *fakeFixPromptQueue) DrainByWorker(ctx context.Context, workerID string) ([]queue.FixPrompt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []queue.FixPrompt{}
	for i, fp := range f.rows {
		if fp.WorkerID == workerID && !fp.Consumed {
			fp.Consumed = true
			f.rows[i] = fp
			out = append(out, fp)
		}
	}
	return out, nil
}

func (f *fakeFixPromptQueue) PendingByWorker(ctx context.Context, workerID string) ([]queue.FixPrompt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []queue.FixPrompt{}
	for _, fp := range f.rows {
		if fp.WorkerID == workerID && !fp.Consumed {
			out = append(out, fp)
		}
	}
	return out, nil
}

func (f *fakeFixPromptQueue) snapshot() []queue.FixPrompt {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]queue.FixPrompt, len(f.rows))
	copy(out, f.rows)
	return out
}

type fakeSession struct {
	mu       sync.Mutex
	threadID subprocess.ThreadID
	sent     []subprocess.Message
	inbound  []subprocess.Message
	closed   bool
	closeErr error

	recvBlock bool
}

func newFakeSession(threadID string, inbound ...subprocess.Message) *fakeSession {
	return &fakeSession{
		threadID: subprocess.ThreadID(threadID),
		inbound:  append([]subprocess.Message(nil), inbound...),
	}
}

func (f *fakeSession) ThreadID() subprocess.ThreadID { return f.threadID }

func (f *fakeSession) Send(ctx context.Context, msg subprocess.Message) error {
	if !msg.Kind.IsValid() {
		return errors.New("fakeSession: invalid MessageKind")
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return subprocess.ErrSessionClosed
	}
	f.sent = append(f.sent, msg)
	f.mu.Unlock()
	return nil
}

func (f *fakeSession) Receive(ctx context.Context) (subprocess.Message, error) {
	for {
		f.mu.Lock()
		if f.closed {
			f.mu.Unlock()
			return subprocess.Message{}, subprocess.ErrSessionClosed
		}
		if len(f.inbound) > 0 {
			msg := f.inbound[0]
			f.inbound = f.inbound[1:]
			f.mu.Unlock()
			return msg, nil
		}
		if !f.recvBlock {
			f.mu.Unlock()
			return subprocess.Message{}, subprocess.ErrSessionClosed
		}
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return subprocess.Message{}, ctx.Err()
		case <-time.After(5 * time.Millisecond):

		}
	}
}

func (f *fakeSession) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return f.closeErr
	}
	f.closed = true
	return f.closeErr
}

func (f *fakeSession) sentSnapshot() []subprocess.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]subprocess.Message, len(f.sent))
	copy(out, f.sent)
	return out
}

func (f *fakeSession) pushInbound(msgs ...subprocess.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inbound = append(f.inbound, msgs...)
}

func (f *fakeSession) setRecvBlock(b bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recvBlock = b
}

type fakeToolRelay struct {
	mu       sync.Mutex
	response json.RawMessage
	err      error
	calls    int
	lastName string
	lastIn   json.RawMessage
}

func newFakeToolRelay(response string, err error) *fakeToolRelay {
	return &fakeToolRelay{
		response: json.RawMessage(response),
		err:      err,
	}
}

func (f *fakeToolRelay) Dispatch(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastName = name
	f.lastIn = append(json.RawMessage(nil), input...)
	if f.err != nil {
		return nil, f.err
	}
	return append(json.RawMessage(nil), f.response...), nil
}

func (f *fakeToolRelay) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeStlBadGet struct{ err error }

func (f *fakeStlBadGet) Enqueue(ctx context.Context, row queue.TaskRow) error { return nil }
func (f *fakeStlBadGet) Claim(ctx context.Context, id queue.TaskID, t string) error {
	return nil
}
func (f *fakeStlBadGet) Advance(ctx context.Context, id queue.TaskID, s queue.Status) error {
	return nil
}
func (f *fakeStlBadGet) Get(ctx context.Context, id queue.TaskID) (queue.TaskRow, error) {
	return queue.TaskRow{}, f.err
}
func (f *fakeStlBadGet) ListByStatus(ctx context.Context, p string, s queue.Status) ([]queue.TaskRow, error) {
	return nil, nil
}
func (f *fakeStlBadGet) ByThread(ctx context.Context, t string) ([]queue.TaskRow, error) {
	return nil, nil
}

type fakeFpqBadDrain struct{ err error }

func (f *fakeFpqBadDrain) Put(ctx context.Context, fp queue.FixPrompt) error { return nil }
func (f *fakeFpqBadDrain) DrainByWorker(ctx context.Context, w string) ([]queue.FixPrompt, error) {
	return nil, f.err
}
func (f *fakeFpqBadDrain) PendingByWorker(ctx context.Context, w string) ([]queue.FixPrompt, error) {
	return nil, nil
}

type fakeSessionFailingSecondSend struct {
	mu        sync.Mutex
	threadID  subprocess.ThreadID
	inbound   []subprocess.Message
	sendCount int
	closed    bool
}

func (f *fakeSessionFailingSecondSend) ThreadID() subprocess.ThreadID { return f.threadID }

func (f *fakeSessionFailingSecondSend) Send(ctx context.Context, msg subprocess.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendCount++
	if f.sendCount > 1 {
		return subprocess.ErrSessionClosed
	}
	return nil
}

func (f *fakeSessionFailingSecondSend) Receive(ctx context.Context) (subprocess.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.inbound) == 0 {
		return subprocess.Message{}, subprocess.ErrSessionClosed
	}
	msg := f.inbound[0]
	f.inbound = f.inbound[1:]
	return msg, nil
}

func (f *fakeSessionFailingSecondSend) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

type stlPlannerOKThenError struct {
	realFake  *fakeSharedTaskList
	parentID  queue.TaskID
	parentErr error
	errOn     int
	calls     int
}

func (s *stlPlannerOKThenError) Enqueue(ctx context.Context, row queue.TaskRow) error {
	return s.realFake.Enqueue(ctx, row)
}
func (s *stlPlannerOKThenError) Claim(ctx context.Context, id queue.TaskID, t string) error {
	return s.realFake.Claim(ctx, id, t)
}
func (s *stlPlannerOKThenError) Advance(ctx context.Context, id queue.TaskID, st queue.Status) error {
	return s.realFake.Advance(ctx, id, st)
}
func (s *stlPlannerOKThenError) Get(ctx context.Context, id queue.TaskID) (queue.TaskRow, error) {
	if id == s.parentID && s.parentErr != nil {
		return queue.TaskRow{}, s.parentErr
	}
	return s.realFake.Get(ctx, id)
}
func (s *stlPlannerOKThenError) ListByStatus(ctx context.Context, p string, st queue.Status) ([]queue.TaskRow, error) {
	return s.realFake.ListByStatus(ctx, p, st)
}
func (s *stlPlannerOKThenError) ByThread(ctx context.Context, t string) ([]queue.TaskRow, error) {
	return s.realFake.ByThread(ctx, t)
}

type stlNthClaimFails struct {
	mu       sync.Mutex
	realFake *fakeSharedTaskList
	failOn   int
	calls    int
	err      error
}

func (s *stlNthClaimFails) Enqueue(ctx context.Context, row queue.TaskRow) error {
	return s.realFake.Enqueue(ctx, row)
}
func (s *stlNthClaimFails) Claim(ctx context.Context, id queue.TaskID, t string) error {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	if n == s.failOn {
		return s.err
	}
	return s.realFake.Claim(ctx, id, t)
}
func (s *stlNthClaimFails) Advance(ctx context.Context, id queue.TaskID, st queue.Status) error {
	return s.realFake.Advance(ctx, id, st)
}
func (s *stlNthClaimFails) Get(ctx context.Context, id queue.TaskID) (queue.TaskRow, error) {
	return s.realFake.Get(ctx, id)
}
func (s *stlNthClaimFails) ListByStatus(ctx context.Context, p string, st queue.Status) ([]queue.TaskRow, error) {
	return s.realFake.ListByStatus(ctx, p, st)
}
func (s *stlNthClaimFails) ByThread(ctx context.Context, t string) ([]queue.TaskRow, error) {
	return s.realFake.ByThread(ctx, t)
}

type stlNthEnqueueFails struct {
	mu       sync.Mutex
	realFake *fakeSharedTaskList
	failOn   int
	calls    int
	err      error
}

func (s *stlNthEnqueueFails) Enqueue(ctx context.Context, row queue.TaskRow) error {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	if n == s.failOn {
		return s.err
	}
	return s.realFake.Enqueue(ctx, row)
}
func (s *stlNthEnqueueFails) Claim(ctx context.Context, id queue.TaskID, t string) error {
	return s.realFake.Claim(ctx, id, t)
}
func (s *stlNthEnqueueFails) Advance(ctx context.Context, id queue.TaskID, st queue.Status) error {
	return s.realFake.Advance(ctx, id, st)
}
func (s *stlNthEnqueueFails) Get(ctx context.Context, id queue.TaskID) (queue.TaskRow, error) {
	return s.realFake.Get(ctx, id)
}
func (s *stlNthEnqueueFails) ListByStatus(ctx context.Context, p string, st queue.Status) ([]queue.TaskRow, error) {
	return s.realFake.ListByStatus(ctx, p, st)
}
func (s *stlNthEnqueueFails) ByThread(ctx context.Context, t string) ([]queue.TaskRow, error) {
	return s.realFake.ByThread(ctx, t)
}

type stlGetSometimesFails struct {
	mu       sync.Mutex
	realFake *fakeSharedTaskList
	parentID queue.TaskID
	failOn   int
	calls    int
}

func (s *stlGetSometimesFails) Enqueue(ctx context.Context, row queue.TaskRow) error {
	return s.realFake.Enqueue(ctx, row)
}
func (s *stlGetSometimesFails) Claim(ctx context.Context, id queue.TaskID, t string) error {
	return s.realFake.Claim(ctx, id, t)
}
func (s *stlGetSometimesFails) Advance(ctx context.Context, id queue.TaskID, st queue.Status) error {
	return s.realFake.Advance(ctx, id, st)
}
func (s *stlGetSometimesFails) Get(ctx context.Context, id queue.TaskID) (queue.TaskRow, error) {
	if id == s.parentID {
		s.mu.Lock()
		s.calls++
		n := s.calls
		s.mu.Unlock()
		if n == s.failOn {
			return queue.TaskRow{}, errors.New("transient db read error")
		}
	}
	return s.realFake.Get(ctx, id)
}
func (s *stlGetSometimesFails) ListByStatus(ctx context.Context, p string, st queue.Status) ([]queue.TaskRow, error) {
	return s.realFake.ListByStatus(ctx, p, st)
}
func (s *stlGetSometimesFails) ByThread(ctx context.Context, t string) ([]queue.TaskRow, error) {
	return s.realFake.ByThread(ctx, t)
}

type stlClaimNoop struct {
	realFake *fakeSharedTaskList
	target   queue.TaskID
}

func (s *stlClaimNoop) Enqueue(ctx context.Context, row queue.TaskRow) error {
	return s.realFake.Enqueue(ctx, row)
}
func (s *stlClaimNoop) Claim(ctx context.Context, id queue.TaskID, t string) error {
	if id == s.target {
		return nil
	}
	return s.realFake.Claim(ctx, id, t)
}
func (s *stlClaimNoop) Advance(ctx context.Context, id queue.TaskID, st queue.Status) error {
	return s.realFake.Advance(ctx, id, st)
}
func (s *stlClaimNoop) Get(ctx context.Context, id queue.TaskID) (queue.TaskRow, error) {
	return s.realFake.Get(ctx, id)
}
func (s *stlClaimNoop) ListByStatus(ctx context.Context, p string, st queue.Status) ([]queue.TaskRow, error) {
	return s.realFake.ListByStatus(ctx, p, st)
}
func (s *stlClaimNoop) ByThread(ctx context.Context, t string) ([]queue.TaskRow, error) {
	return s.realFake.ByThread(ctx, t)
}

type zeroDeadlineConfig struct{}

func (zeroDeadlineConfig) ReinforcementTemplate(string) string     { return "" }
func (zeroDeadlineConfig) CheckpointDeadline(string) time.Duration { return 0 }

func fakeDoctrineConfig(template string, deadline time.Duration) worker.DoctrineConfig {
	templates := map[string]string{}
	deadlines := map[string]time.Duration{}
	if template != "" {
		templates["max-scope"] = template
		templates["default"] = template
		templates["capa-firewall"] = template
	}
	if deadline > 0 {
		deadlines["max-scope"] = deadline
		deadlines["default"] = deadline
		deadlines["capa-firewall"] = deadline
	}
	return worker.StaticDoctrineConfig{Templates: templates, Deadlines: deadlines}
}
