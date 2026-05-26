// SPDX-License-Identifier: MIT
package main

import "github.com/labstack/echo/v4"

func main() {
	e := echo.New()
	e.Group("/v1").GET("/users", listUsers)
	_ = e.Start(":8080")
}

func listUsers(c echo.Context) error { return nil }
