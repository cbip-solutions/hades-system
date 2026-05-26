// SPDX-License-Identifier: MIT
package echo

import "time"

func realNowSeconds() int64 { return time.Now().Unix() }
