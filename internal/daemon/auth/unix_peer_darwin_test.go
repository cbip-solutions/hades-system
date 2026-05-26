//go:build darwin

package auth

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestXucredToPeerCred_Ngroups_GT_0(t *testing.T) {
	x := &unix.Xucred{
		Version: 1,
		Uid:     501,
		Ngroups: 3,
	}
	x.Groups[0] = 42
	x.Groups[1] = 99
	pc := xucredToPeerCred(x)
	if pc.UID != 501 {
		t.Errorf("UID: got %d, want 501", pc.UID)
	}
	if pc.GID != 42 {
		t.Errorf("GID: got %d, want 42 (Groups[0])", pc.GID)
	}
	if !pc.HasSet {
		t.Errorf("HasSet=false")
	}
}

func TestXucredToPeerCred_Ngroups_Zero_FallbackToOurGid(t *testing.T) {
	x := &unix.Xucred{
		Version: 1,
		Uid:     1000,
		Ngroups: 0,
	}
	pc := xucredToPeerCred(x)
	if pc.UID != 1000 {
		t.Errorf("UID: got %d", pc.UID)
	}
	if pc.GID != uint32(os.Getgid()) {
		t.Errorf("GID: got %d, want our %d", pc.GID, os.Getgid())
	}
	if !pc.HasSet {
		t.Errorf("HasSet=false")
	}
}

func TestXucredToPeerCred_Ngroups_Negative_FallbackToOurGid(t *testing.T) {

	x := &unix.Xucred{
		Version: 1,
		Uid:     2000,
		Ngroups: -1,
	}
	pc := xucredToPeerCred(x)
	if pc.GID != uint32(os.Getgid()) {
		t.Errorf("GID: got %d, want our %d (negative Ngroups → fallback)", pc.GID, os.Getgid())
	}
}
