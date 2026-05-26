// SPDX-License-Identifier: MIT
package ecosystem

import (
	"crypto/tls"
)

func badDial() {
	conn, _ := tls.Dial("tcp", "example.com:443", nil)
	_ = conn
}

func badClient() {
	var c tls.Config
	_ = c
}
