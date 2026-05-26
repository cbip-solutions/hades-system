package cli

import (
	"strings"
	"testing"
)

func TestBypassExtractRegistered(t *testing.T) {
	for _, sub := range NewBypassCmd().Commands() {
		if strings.HasPrefix(sub.Use, "extract-config") {
			return
		}
	}
	t.Error("zen bypass extract-config not registered")
}

func TestBypassCrossValidateRegistered(t *testing.T) {
	for _, sub := range NewBypassCmd().Commands() {
		if strings.HasPrefix(sub.Use, "cross-validate") {
			return
		}
	}
	t.Error("zen bypass cross-validate not registered")
}

func TestBypassExtractFlags(t *testing.T) {
	cmd := newBypassExtractCmd()
	for _, name := range []string{"out", "listen", "capture-only"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("--%s flag missing", name)
		}
	}
}

func TestBypassCrossValidateFlags(t *testing.T) {
	cmd := newBypassCrossValidateCmd()
	for _, name := range []string{"config", "plugin", "report"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("--%s flag missing", name)
		}
	}
}
