// SPDX-License-Identifier: MIT
package main

import "github.com/labstack/echo/v4"

func main() {
	e := echo.New()
	v1 := e.Group("/v1")
	admin := v1.Group("/admin")
	admin.GET("/stats", stats)
	_ = e.Start(":8080")
}

func stats(c echo.Context) error { return nil }
