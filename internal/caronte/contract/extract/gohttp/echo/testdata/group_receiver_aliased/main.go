// SPDX-License-Identifier: MIT
package main

import "github.com/labstack/echo/v4"

func reg(g *echo.Group) {
	g.GET("/path", h)
}

func main() {
	e := echo.New()
	g := e.Group("/v1")
	reg(g)
	_ = e.Start(":8080")
}

func h(c echo.Context) error { return nil }
