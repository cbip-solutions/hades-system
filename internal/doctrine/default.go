// SPDX-License-Identifier: MIT
package doctrine

type Default struct{}

func (Default) Name() Name { return NameDefault }

func (Default) ArchiveStrategy() string { return "squash" }

func (Default) RequireAdvisoryDefault() bool { return false }

func (Default) PrivacyLocked() bool { return false }

func (Default) PreFlightExtras() []string { return nil }

func (Default) PreArchiveExtras() []string { return nil }
