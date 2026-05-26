package mapping

import "testing"

func TestPreset_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   Preset
		want bool
	}{
		{PresetStrict, true},
		{PresetLenient, true},
		{Preset("garbage"), false},
		{Preset(""), false},
		{Preset("STRICT"), false},
	}
	for _, c := range cases {
		got := c.in.IsValid()
		if got != c.want {
			t.Errorf("%q.IsValid(): got %v, want %v", c.in, got, c.want)
		}
	}
}
