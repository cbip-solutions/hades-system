// SPDX-License-Identifier: MIT
package bad2

type Adapter struct{}

func (a *Adapter) Promote(noteID, operatorID, reason string) error { return nil }

func runBad() {
	a := &Adapter{}
	_ = a.Promote("note-1", "the-operator", "") // want `invariant: Promote\(\) reason MUST be non-empty operator-supplied string`
}
