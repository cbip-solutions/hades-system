// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package proto

import "time"

func realNowSeconds() int64 { return time.Now().Unix() }
