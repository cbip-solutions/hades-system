package ecosystem

import (
	"strings"
	"testing"
)

func TestResolveBGEModelPath_ExplicitWins(t *testing.T) {
	t.Setenv("ZEN_BGE_MODEL_PATH", "/env/path/model.onnx")
	got := ResolveBGEModelPath("/explicit/path/model.onnx")
	if got != "/explicit/path/model.onnx" {
		t.Errorf("explicit should win: got %q", got)
	}
}

func TestResolveBGEModelPath_EnvUsedWhenExplicitEmpty(t *testing.T) {
	t.Setenv("ZEN_BGE_MODEL_PATH", "/env/path/model.onnx")
	got := ResolveBGEModelPath("")
	if got != "/env/path/model.onnx" {
		t.Errorf("env should be used when explicit empty: got %q", got)
	}
}

func TestResolveBGEModelPath_DefaultPath(t *testing.T) {
	t.Setenv("ZEN_BGE_MODEL_PATH", "")
	t.Setenv("HOME", "/synthetic/home")
	got := ResolveBGEModelPath("")
	if !strings.Contains(got, "bge-reranker-v2-m3.onnx") {
		t.Errorf("default path = %q, want containing model filename", got)
	}
	if !strings.Contains(got, "/synthetic/home") {
		t.Errorf("default path = %q, want containing $HOME", got)
	}
}

// TestResolveBGEModelPath_DefaultPathReturnsEmptyWhenHomeFails asserts
// the graceful-empty fallback when os.UserHomeDir() cannot resolve.
// On unix-like systems HOME being unset is the failure mode. The
// function MUST NOT panic; it returns "".
func TestResolveBGEModelPath_DefaultPathReturnsEmptyWhenHomeFails(t *testing.T) {
	t.Setenv("ZEN_BGE_MODEL_PATH", "")
	t.Setenv("HOME", "")

	t.Setenv("USERPROFILE", "")
	got := ResolveBGEModelPath("")

	if got != "" && !strings.Contains(got, "bge-reranker-v2-m3.onnx") {
		t.Errorf("default path with HOME unset = %q, want empty or containing model filename", got)
	}
}
