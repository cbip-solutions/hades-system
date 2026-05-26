package preflight

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	const isolatedXDG = "/__zen_preflight_test_isolated_xdg__/nonexistent"
	os.Setenv("XDG_CONFIG_HOME", isolatedXDG)
	os.Exit(m.Run())
}
