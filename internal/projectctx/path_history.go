// SPDX-License-Identifier: MIT
// path_history.go — HADES design : mv-detection logic per design contract
//
// PathHistoryEntry mirrors a row in the daemon-stored path_history table
// (translated by projectctxadapter at the boundary). MvDetection is the
// result type of DetectMv.
//
// DetectMv is pure: no I/O, no DB, no clock. The caller assembles
// history + current path/ID and we decide whether the project moved.
package projectctx

import (
	"fmt"
	"time"
)

type PathHistoryEntry struct {
	ProjectID   ProjectID
	Path        string
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

type MvDetection struct {
	Alias   Alias
	OldPath string
	NewPath string
	OldID   ProjectID
	NewID   ProjectID
}

func (m *MvDetection) String() string {
	return fmt.Sprintf("mv-detected: alias=%s old=%s:%s new=%s:%s",
		m.Alias, m.OldPath, m.OldID.Short(),
		m.NewPath, m.NewID.Short())
}

func DetectMv(alias Alias, currentPath string, currentID ProjectID, history []PathHistoryEntry) *MvDetection {
	if len(history) == 0 {
		return nil
	}
	var latest *PathHistoryEntry
	for i := range history {
		if history[i].ProjectID == currentID {

			return nil
		}
		if latest == nil || history[i].LastSeenAt.After(latest.LastSeenAt) {
			latest = &history[i]
		}
	}

	return &MvDetection{
		Alias:   alias,
		OldPath: latest.Path,
		NewPath: currentPath,
		OldID:   latest.ProjectID,
		NewID:   currentID,
	}
}
