// SPDX-License-Identifier: MIT
// Package tui — panels.go.
//
// Per-panel cadence registry + panel-typed tickMsg for the F1-F12
// panel system (spec §7.3). The dashboard's Update routes panelTickMsg
// to the appropriate panel's refetch Cmd when the message's key
// matches the active panel; inactive-panel ticks drop on the floor
// (cadence effectively pauses while the panel is hidden, restarting
// at the operator's next F-key press).
//
// Cadence assignment (per spec §7.3 + project doctrine
// "calm-by-default"):
//
// Panel | Key | Cadence | Rationale
// ------------+-----+---------+----------------------------------------
// Help | F1 | static | Hermes default; no live data
// Workforce | F2 | 1s | hot — operator wants live worker tree
// Cost | F3 | 2s | warm — 4-axis dashboard
// Audit | F4 | 1s | hot — event stream
// HRA queue | F5 | 1s | hot — confirmations queue ages
// Confirms | F6 | 1s | hot — pending decisions
// Code Graph | F7 | 5s | cold — file structure changes slowly
// Memory | F8 | 5s | cold — memory inspector
// Skills | F9 | 5s | cold — Curator browser
// Doctrine | F10 | 5s | cold — doctrine + amendments
// X-project | F11 | 5s | cold — switcher list
// Inbox | F12 | 2s | warm — notifications inbox
//
// These thresholds are doctrine-tunable post-ship via
// ~/.config/hades-system/tui.toml; release ships
// fixed defaults.
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type panelKey int

const (
	panelHelp panelKey = iota
	panelWorkforce
	panelCost
	panelAudit
	panelHRA
	panelConfirmations
	panelCodegraph
	panelMemory
	panelSkills
	panelDoctrine
	panelCrossProject
	panelInbox
)

func (p panelKey) String() string {
	switch p {
	case panelHelp:
		return "help"
	case panelWorkforce:
		return "workforce"
	case panelCost:
		return "cost"
	case panelAudit:
		return "audit"
	case panelHRA:
		return "hra"
	case panelConfirmations:
		return "confirm"
	case panelCodegraph:
		return "codegraph"
	case panelMemory:
		return "memory"
	case panelSkills:
		return "skills"
	case panelDoctrine:
		return "doctrine"
	case panelCrossProject:
		return "xproject"
	case panelInbox:
		return "inbox"
	default:
		return "unknown"
	}
}

func panelCadence(k panelKey) time.Duration {
	switch k {
	case panelHelp:
		return 0
	case panelWorkforce, panelAudit, panelHRA, panelConfirmations:
		return 1 * time.Second
	case panelCost, panelInbox:
		return 2 * time.Second
	case panelCodegraph, panelMemory, panelSkills, panelDoctrine, panelCrossProject:
		return 5 * time.Second
	default:
		return 5 * time.Second
	}
}

type panelTickMsg struct {
	key panelKey
}

func scheduleTick(key panelKey) tea.Cmd {
	d := panelCadence(key)
	if d == 0 {
		return nil
	}
	return tea.Tick(d, func(_ time.Time) tea.Msg { return panelTickMsg{key: key} })
}
