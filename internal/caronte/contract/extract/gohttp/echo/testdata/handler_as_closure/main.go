// SPDX-License-Identifier: MIT
package main

import "github.com/labstack/echo/v4"

func main() {
	e := echo.New()
	e.GET("/x", func(c echo.Context) error { return nil })
	_ = e.Start(":8080")
}
