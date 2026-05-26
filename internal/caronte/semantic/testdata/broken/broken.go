// SPDX-License-Identifier: MIT
package broken

func Caller() { Callee() }

func Callee() {}
