package chain

import "testing"

func TestChainErr_Error(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "ErrNoChainTip",
			err:  ErrNoChainTip,
			want: "chain: no chain tip (audit_events_raw empty)",
		},
		{
			name: "ErrEventNotFound",
			err:  ErrEventNotFound,
			want: "chain: audit event not found",
		},
		{
			name: "ErrPartitionSealNotFound",
			err:  ErrPartitionSealNotFound,
			want: "chain: partition seal not found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.err.Error()
			if got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}
