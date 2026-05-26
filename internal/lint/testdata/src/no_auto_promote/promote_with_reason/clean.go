// SPDX-License-Identifier: MIT
package good

type Adapter struct{}

func (a *Adapter) Promote(noteID, operatorID, reason string) error { return nil }

func runOK() {
	a := &Adapter{}
	_ = a.Promote("note-1", "the-operator", "applies to all max-scope projects")
}
