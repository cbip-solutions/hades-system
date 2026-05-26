package litestream

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type fakeBehavior struct {
	exitCode int
	sleepMs  int
	stderr   string
}

func writeFakeLitestreamScript(t *testing.T, dir string, b fakeBehavior) string {
	t.Helper()
	js, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal behavior: %v", err)
	}
	_ = js
	script := "#!/bin/bash\n"
	if b.stderr != "" {
		script += "echo '" + b.stderr + "' 1>&2\n"
	}
	if b.sleepMs > 0 {
		script += "sleep " + msToSec(b.sleepMs) + "\n"
	}
	script += "exit " + itoa(b.exitCode) + "\n"
	path := filepath.Join(dir, "fake-litestream.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func writeStubConfig(p string) error {
	return os.WriteFile(p, []byte("# stub litestream config\n"), 0o600)
}

func msToSec(ms int) string {

	if ms == 0 {
		return "0"
	}
	if ms < 1000 {
		return "0." + leftPad(ms, 3)
	}
	return itoa(ms/1000) + "." + leftPad(ms%1000, 3)
}

func leftPad(n, width int) string {
	s := itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
