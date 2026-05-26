package zenday

import "testing"

func TestParsePercent_HappyPath(t *testing.T) {
	tests := []struct {
		in   string
		want float64
	}{
		{"92.5%", 92.5},
		{"100.0%", 100.0},
		{"0.0%", 0.0},
		{"81.0%", 81.0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := parsePercent(tt.in)
			if got != tt.want {
				t.Errorf("parsePercent(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParsePercent_MalformedReturnsZero(t *testing.T) {
	tests := []string{"", "not a percent", "x", "abc%"}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			if got := parsePercent(in); got != 0 {
				t.Errorf("parsePercent(%q) = %v, want 0 (defensive fallback)", in, got)
			}
		})
	}
}

func TestHasPrefix_ShortStringReturnsFalse(t *testing.T) {
	if hasPrefix("x", "scheduler.") {
		t.Error("hasPrefix(\"x\", \"scheduler.\") = true; want false (s shorter than p)")
	}
	if hasPrefix("", "abc") {
		t.Error("hasPrefix(\"\", \"abc\") = true; want false")
	}
	if !hasPrefix("scheduler.routine_failed", "scheduler.") {
		t.Error("hasPrefix(\"scheduler.routine_failed\", \"scheduler.\") = false; want true")
	}
	if !hasPrefix("any", "") {
		t.Error("hasPrefix(\"any\", \"\") = false; want true (empty prefix matches everything)")
	}
}
