// SPDX-License-Identifier: MIT
package autonomy

import (
	"fmt"
	"strings"
)

type Mode uint8

const (
	ModeManual Mode = iota + 1

	ModeSemi

	ModeFull
)

func (m Mode) String() string {
	switch m {
	case ModeManual:
		return "manual"
	case ModeSemi:
		return "semi"
	case ModeFull:
		return "full"
	default:
		return fmt.Sprintf("mode(%d)", uint8(m))
	}
}

func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "manual":
		return ModeManual, nil
	case "semi":
		return ModeSemi, nil
	case "full":
		return ModeFull, nil
	default:
		return 0, fmt.Errorf("autonomy: unknown mode %q (want manual|semi|full)", s)
	}
}

type Source uint8

const (
	SourceDoctrineDefault Source = iota + 1

	SourceProjectConfig

	SourceBuildFlag

	SourceCapaFirewallGuard
)

func (s Source) String() string {
	switch s {
	case SourceDoctrineDefault:
		return "doctrine-default"
	case SourceProjectConfig:
		return "project-config"
	case SourceBuildFlag:
		return "build-flag"
	case SourceCapaFirewallGuard:
		return "capa-firewall-guard"
	default:
		return fmt.Sprintf("source(%d)", uint8(s))
	}
}

type ResolveInput struct {
	Doctrine      string
	ProjectConfig *Mode
	BuildFlag     *Mode
}

type RejectedOverride struct {
	AttemptedMode Mode
	AttemptedFrom Source
}

type Resolution struct {
	Mode             Mode
	Source           Source
	RejectedOverride *RejectedOverride
}

func doctrineDefault(d string) Mode {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "max-scope":
		return ModeSemi
	default:

		return ModeManual
	}
}

func Resolve(in ResolveInput) Resolution {
	if strings.EqualFold(strings.TrimSpace(in.Doctrine), "capa-firewall") {
		var rej *RejectedOverride
		switch {
		case in.BuildFlag != nil && *in.BuildFlag != ModeManual:
			rej = &RejectedOverride{AttemptedMode: *in.BuildFlag, AttemptedFrom: SourceBuildFlag}
		case in.ProjectConfig != nil && *in.ProjectConfig != ModeManual:
			rej = &RejectedOverride{AttemptedMode: *in.ProjectConfig, AttemptedFrom: SourceProjectConfig}
		}
		return Resolution{Mode: ModeManual, Source: SourceCapaFirewallGuard, RejectedOverride: rej}
	}
	if in.BuildFlag != nil {
		return Resolution{Mode: *in.BuildFlag, Source: SourceBuildFlag}
	}
	if in.ProjectConfig != nil {
		return Resolution{Mode: *in.ProjectConfig, Source: SourceProjectConfig}
	}
	return Resolution{Mode: doctrineDefault(in.Doctrine), Source: SourceDoctrineDefault}
}
