package cli

import (
	"strings"
	"testing"
)

func TestRootMountsPlan20Commands(t *testing.T) {
	root := NewRootCmd()
	want := map[string]bool{
		"contract":   true,
		"workspace":  true,
		"federation": true,
		"api-impact": true,
	}
	for _, cmd := range root.Commands() {

		head := strings.Fields(cmd.Use)
		if len(head) == 0 {
			continue
		}
		delete(want, head[0])
	}
	if len(want) != 0 {
		t.Errorf("root command tree missing Plan-20 commands: %v", want)
	}
}

func TestContractCommandMountsSubCommands(t *testing.T) {
	contract := NewContractCmdProd()
	want := map[string]bool{
		"validate": true,
		"why":      true,
	}
	for _, cmd := range contract.Commands() {
		head := strings.Fields(cmd.Use)
		if len(head) == 0 {
			continue
		}
		delete(want, head[0])
	}
	if len(want) != 0 {
		t.Errorf("`zen contract` missing sub-commands: %v", want)
	}
}

func TestWorkspaceCommandMountsSubCommands(t *testing.T) {
	ws := NewWorkspaceCmdProd()
	want := map[string]bool{
		"init":    true,
		"list":    true,
		"members": true,
		"link":    true,
		"remove":  true,
		"policy":  true,
	}
	for _, cmd := range ws.Commands() {
		head := strings.Fields(cmd.Use)
		if len(head) == 0 {
			continue
		}
		delete(want, head[0])
	}
	if len(want) != 0 {
		t.Errorf("`zen workspace` missing sub-commands: %v", want)
	}

	for _, cmd := range ws.Commands() {
		head := strings.Fields(cmd.Use)
		if len(head) == 0 || head[0] != "policy" {
			continue
		}
		wantPolicy := map[string]bool{"get": true, "set": true}
		for _, sub := range cmd.Commands() {
			subHead := strings.Fields(sub.Use)
			if len(subHead) == 0 {
				continue
			}
			delete(wantPolicy, subHead[0])
		}
		if len(wantPolicy) != 0 {
			t.Errorf("`zen workspace policy` missing sub-commands: %v", wantPolicy)
		}
	}
}

func TestFederationCommandMountsSubCommands(t *testing.T) {
	fed := NewFederationCmdProd()
	want := map[string]bool{"health": true}
	for _, cmd := range fed.Commands() {
		head := strings.Fields(cmd.Use)
		if len(head) == 0 {
			continue
		}
		delete(want, head[0])
	}
	if len(want) != 0 {
		t.Errorf("`zen federation` missing sub-commands: %v", want)
	}
}
