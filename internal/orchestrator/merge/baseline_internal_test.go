package merge

import (
	"testing"
	"time"
)

func TestSelectCommandDefaultBranchReturnsEmpty(t *testing.T) {
	b := &concreteBaseline{
		cfg: BaselineConfig{Timeout: time.Second, StderrCapBytes: 64},
	}
	cmd, env := b.selectCommand(TestTierUnknown, TestSuite{
		Full:  []string{"go", "test"},
		Smoke: []string{"go", "test", "-tags=smoke"},
	})
	if cmd != nil {
		t.Errorf("selectCommand(TestTierUnknown) cmd = %v want nil", cmd)
	}
	if env != nil {
		t.Errorf("selectCommand(TestTierUnknown) env = %v want nil", env)
	}
}
