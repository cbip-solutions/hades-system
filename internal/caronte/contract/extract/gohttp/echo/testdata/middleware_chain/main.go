// SPDX-License-Identifier: MIT
package main

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	e := echo.New()
	v1 := e.Group("/v1", middleware.Logger(), middleware.Recover())
	v1.GET("/me", me)
	_ = e.Start(":8080")
}

func me(c echo.Context) error { return nil }
