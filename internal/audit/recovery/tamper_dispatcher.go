// SPDX-License-Identifier: MIT
package recovery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Halts struct {
	mu sync.Mutex
	m  map[string]struct{}
}

func NewHalts() *Halts { return &Halts{m: make(map[string]struct{})} }

func (h *Halts) Halt(projectID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.m[projectID] = struct{}{}
}

func (h *Halts) Clear(projectID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.m, projectID)
}

func (h *Halts) IsHalted(projectID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.m[projectID]
	return ok
}

func (h *Halts) ListHalted() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.m))
	for id := range h.m {
		out = append(out, id)
	}
	return out
}

type DoctrineResolver interface {
	DoctrineFor(ctx context.Context, projectID string) (string, error)
}

type ProjectLister interface {
	ListProjectIDs(ctx context.Context) ([]string, error)
}

type InboxNotification struct {
	ProjectID string
	Severity  string
	Message   string
}

type InboxEmitter interface {
	PushURGENT(ctx context.Context, projectID, message string) error
}

type TamperEvent struct {
	ProjectID string
	Path      TamperPath
	RecordID  int64

	LastCleanRecordID int64
	DetectionPath     string
	Severity          string
	Timestamp         time.Time
}

type EventEmitter interface {
	EmitTamperDetected(ctx context.Context, projectID string, detection *VerifyResult) error
}

type DispatchResult struct {
	Halted        bool
	CascadedHalts []string
	InboxSeverity string
	EventEmitted  bool
}

type TamperDispatcher struct {
	halts    *Halts
	doctrine DoctrineResolver
	lister   ProjectLister
	inbox    InboxEmitter
	events   EventEmitter
}

func NewTamperDispatcher(
	halts *Halts,
	doctrine DoctrineResolver,
	lister ProjectLister,
	inbox InboxEmitter,
	events EventEmitter,
) *TamperDispatcher {
	return &TamperDispatcher{
		halts:    halts,
		doctrine: doctrine,
		lister:   lister,
		inbox:    inbox,
		events:   events,
	}
}

func (d *TamperDispatcher) DispatchTamperResponse(
	ctx context.Context,
	projectID string,
	detection *VerifyResult,
) (*DispatchResult, error) {
	if projectID == "" {
		return nil, errors.New("recovery dispatcher: empty project_id")
	}
	if detection == nil {
		return nil, errors.New("recovery dispatcher: nil detection")
	}
	if detection.Clean {
		return nil, errors.New("recovery dispatcher: refusing to dispatch on Clean result")
	}

	doctrineName, err := d.doctrine.DoctrineFor(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("recovery dispatcher: doctrine lookup: %w", err)
	}

	res := &DispatchResult{}

	if d.events != nil {
		if err := d.events.EmitTamperDetected(ctx, projectID, detection); err != nil {
			return nil, fmt.Errorf("recovery dispatcher: emit event: %w", err)
		}
		res.EventEmitted = true
	}
	if d.inbox != nil {
		msg := fmt.Sprintf("audit chain tamper detected: project=%s path=%s record_id=%d",
			projectID, detection.FirstTamperPath, detection.FirstTamperRecordID)
		if err := d.inbox.PushURGENT(ctx, projectID, msg); err != nil {
			return nil, fmt.Errorf("recovery dispatcher: push inbox: %w", err)
		}
		res.InboxSeverity = "URGENT"
	}

	switch strings.ToLower(strings.TrimSpace(doctrineName)) {
	case "max-scope":
		d.halts.Halt(projectID)
		res.Halted = true
	case "default":

	case "capa-firewall":
		d.halts.Halt(projectID)
		res.Halted = true

		if d.lister != nil {
			ids, err := d.lister.ListProjectIDs(ctx)
			if err != nil {
				return res, fmt.Errorf("recovery dispatcher: list projects for cascade: %w", err)
			}
			for _, id := range ids {
				if id == projectID {
					continue
				}
				d.halts.Halt(id)
				res.CascadedHalts = append(res.CascadedHalts, id)
			}
		}
	default:

		d.halts.Halt(projectID)
		res.Halted = true
	}

	return res, nil
}
