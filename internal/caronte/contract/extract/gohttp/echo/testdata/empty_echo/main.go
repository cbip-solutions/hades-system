// SPDX-License-Identifier: MIT
package main

import "github.com/labstack/echo/v4"

func main() {
	e := echo.New()
	_ = e.Start(":8080")
}
