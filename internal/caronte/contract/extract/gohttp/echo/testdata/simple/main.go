// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func main() {
	e := echo.New()
	e.GET("/health", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	_ = e.Start(":8080")
}
