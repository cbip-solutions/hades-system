// SPDX-License-Identifier: MIT
package worker

import "time"

// DoctrineConfig provides per-worker doctrine values needed at runtime.
//
// ships doctrine.Resolver as a layered config-loader that returns
// a fully-merged Schema; runtime values used by the Worker live inside
// Schema (e.g., Schema.Workforce.DoctrineReinforcementTemplatePointer,
// Schema.Subprocess.PersistentTTLSliding). Worker does NOT
// import internal/doctrine directly so this small interface decouples
// the worker package from the Schema shape (lets the daemon
// workforceadapter own the bridge: read Resolver → produce
// DoctrineConfig).
//
// Concurrency implementations MUST be safe for concurrent reads from
// multiple Workers — the daemon constructs one DoctrineConfig per
// daemon-lifetime and shares it.
type DoctrineConfig interface {
	ReinforcementTemplate(name string) string

	CheckpointDeadline(name string) time.Duration
}

type StaticDoctrineConfig struct {
	Templates map[string]string

	Deadlines map[string]time.Duration
}

const DefaultCheckpointDeadline = 30 * time.Second

func (s StaticDoctrineConfig) ReinforcementTemplate(name string) string {
	if t, ok := s.Templates[name]; ok && t != "" {
		return t
	}
	return placeholderTemplate(name)
}

func (s StaticDoctrineConfig) CheckpointDeadline(name string) time.Duration {
	if d, ok := s.Deadlines[name]; ok && d > 0 {
		return d
	}
	return DefaultCheckpointDeadline
}

func placeholderTemplate(name string) string {
	return "[doctrine: " + name + "]"
}
