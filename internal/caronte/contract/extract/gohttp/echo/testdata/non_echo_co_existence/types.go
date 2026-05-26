// SPDX-License-Identifier: MIT
package main

type Widget struct {
	Name string
}

func (w Widget) Render() string { return w.Name }
