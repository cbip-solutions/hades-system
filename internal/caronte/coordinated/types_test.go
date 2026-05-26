package coordinated

import "testing"

func TestConsumerRefFieldSet(t *testing.T) {
	r := ConsumerRef{
		Repo:   "client-app",
		CallID: "client-app:auth_handler.go:42:GetUser",
		NodeID: "client-app:auth_handler.go:42:GetUser",
		File:   "auth_handler.go",
		Line:   42,
	}
	if r.Repo == "" || r.CallID == "" || r.NodeID == "" || r.File == "" || r.Line == 0 {
		t.Fatal("ConsumerRef field set incomplete (master C-8 drift)")
	}
}
