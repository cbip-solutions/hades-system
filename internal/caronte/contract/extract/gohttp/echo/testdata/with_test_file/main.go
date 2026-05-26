// SPDX-License-Identifier: MIT
package main

import "github.com/labstack/echo/v4"

func main() {
	e := echo.New()
	e.GET("/prod", h)
	_ = e.Start(":8080")
}

func h(c echo.Context) error { return nil }
