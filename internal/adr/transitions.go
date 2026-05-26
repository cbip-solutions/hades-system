// SPDX-License-Identifier: MIT
package adr

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

var reADRID = regexp.MustCompile(`^ADR-[0-9]{4}$`)

func IsValidTransition(from, to Status) bool {

	if from == to {
		return false
	}

	if from == StatusReserved || to == StatusReserved {
		return false
	}
	switch from {
	case StatusProposed:
		return to == StatusAccepted || to == StatusRejected
	case StatusAccepted:
		return to == StatusSuperseded || to == StatusDeprecated
	default:

		return false
	}
}

func applyTransition(
	ctx context.Context,
	path string,
	targetStatus Status,
	supersededBy string,
	operatorID string,
	reason string,
	sink EventSink,
	now func() time.Time,
) error {

	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("adr: applyTransition: context: %w", err)
	}

	if reason == "" {
		return fmt.Errorf("%w", ErrEmptyReason)
	}

	if sink == nil {
		sink = NoopEventSink{}
	}

	a, err := ParseFile(path)
	if err != nil {
		return fmt.Errorf("adr: applyTransition: parse: %w", err)
	}

	fromStatus := a.Frontmatter.Status

	if fromStatus == StatusReserved {
		return fmt.Errorf("%w", ErrReservedStatusNotTransitionable)
	}

	if !IsValidTransition(fromStatus, targetStatus) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, fromStatus, targetStatus)
	}

	a.Frontmatter.Status = targetStatus
	a.Frontmatter.Date = now().UTC().Format("2006-01-02")
	if supersededBy != "" {
		a.Frontmatter.SupersededBy = supersededBy
	}

	fmBytes, err := yaml.Marshal(a.Frontmatter)
	if err != nil {
		return fmt.Errorf("adr: applyTransition: marshal frontmatter: %w", err)
	}

	content := "---\n" + string(fmBytes) + "---\n" + a.Body

	tmpPath := path + ".transition.tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("adr: applyTransition: write tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {

		_ = os.Remove(tmpPath)
		return fmt.Errorf("adr: applyTransition: rename: %w", err)
	}

	evtType, _ := EventTypeForTransition(fromStatus, targetStatus)
	payload := EventPayload{
		ADRID:      a.Frontmatter.ID,
		StatusFrom: fromStatus,
		StatusTo:   targetStatus,
		OperatorID: operatorID,
		Reason:     reason,
		Timestamp:  now(),
	}
	if err := sink.Emit(evtType, payload); err != nil {

		return fmt.Errorf("adr: applyTransition: emit event: %w", err)
	}

	return nil
}

func Accept(
	ctx context.Context,
	path string,
	operatorID string,
	reason string,
	sink EventSink,
	now func() time.Time,
) error {
	return applyTransition(ctx, path, StatusAccepted, "", operatorID, reason, sink, now)
}

func Reject(
	ctx context.Context,
	path string,
	operatorID string,
	reason string,
	sink EventSink,
	now func() time.Time,
) error {
	return applyTransition(ctx, path, StatusRejected, "", operatorID, reason, sink, now)
}

func Supersede(
	ctx context.Context,
	path string,
	newID string,
	operatorID string,
	reason string,
	sink EventSink,
	now func() time.Time,
) error {
	if !reADRID.MatchString(newID) {
		return fmt.Errorf("adr: Supersede: newID %q does not match required pattern %s", newID, reADRID.String())
	}
	return applyTransition(ctx, path, StatusSuperseded, newID, operatorID, reason, sink, now)
}

func Deprecate(
	ctx context.Context,
	path string,
	operatorID string,
	reason string,
	sink EventSink,
	now func() time.Time,
) error {
	return applyTransition(ctx, path, StatusDeprecated, "", operatorID, reason, sink, now)
}
