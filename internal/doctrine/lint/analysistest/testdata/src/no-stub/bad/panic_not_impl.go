// SPDX-License-Identifier: MIT
// Package bad contains nostub-trigger fixtures. Each function carries a
// `want` annotation per analysistest convention so analysistest.Run can
// verify the analyzer reports exactly the expected diagnostics.
package bad

func DoSomething() {
	panic("not implemented")
}

func DoSomethingYet() {
	panic("not implemented yet")
}

func DoTodo() {
	panic("TODO")
}

func DoUnimplemented() {
	panic("unimplemented")
}

func DoCaseInsensitive() {
	panic("Not Implemented")
}

func PanicWithFmtSprintf(x int) {
	if x < 0 {
		panic("invalid x: must be >= 0")
	}
}
