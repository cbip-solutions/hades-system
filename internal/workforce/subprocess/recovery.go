// SPDX-License-Identifier: MIT
package subprocess

import (
	"encoding/json"
	"fmt"
	"time"
)

type Checkpoint struct {
	ThreadID  string
	TaskID    string
	Index     int
	State     string
	CreatedAt time.Time
}

type CheckpointStore interface {
	LastN(threadID ThreadID, n int) ([]Checkpoint, error)
}

// jsonMarshal is the encoder seam used by RebuildPrompt. Production uses
// encoding/json; tests substitute a recorder so the rare-but-real Marshal
// error branch is exercisable.
//
// CONCURRENCY WARNING: package-level var; tests that swap it MUST NOT
// use t.Parallel() — the swap races with concurrent tests in the same
// package that call RebuildPrompt. Same constraint applies to getpgid,
// kill (openclaude_session.go), and randRead (session.go).
var jsonMarshal = json.Marshal

func RebuildPrompt(threadID ThreadID, history []Checkpoint) (Message, error) {
	type cpJSON struct {
		Index     int    `json:"index"`
		TaskID    string `json:"task_id"`
		State     string `json:"state"`
		CreatedAt int64  `json:"created_at_unix"`
	}
	out := struct {
		ThreadID    string   `json:"thread_id"`
		History     []cpJSON `json:"history"`
		HistorySize int      `json:"history_size"`
	}{
		ThreadID: string(threadID),
		History:  make([]cpJSON, 0, len(history)),
	}
	for _, c := range history {
		out.History = append(out.History, cpJSON{
			Index:     c.Index,
			TaskID:    c.TaskID,
			State:     c.State,
			CreatedAt: c.CreatedAt.UTC().Unix(),
		})
	}
	out.HistorySize = len(out.History)
	body, err := jsonMarshal(out)
	if err != nil {
		return Message{}, fmt.Errorf("subprocess: encode RebuildPrompt: %w", err)
	}
	return Message{
		Kind:     MessageKindNotification,
		Method:   "context/restore",
		ThreadID: threadID,
		Payload:  body,
	}, nil
}

const LastNDefault = 16

type RecoveryConfig struct {
	LastN int
}
