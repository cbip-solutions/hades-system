// SPDX-License-Identifier: MIT
package main

import "github.com/labstack/echo/v4"

var e = echo.New()

func init() { e.GET("/health", h) }
func main() { _ = e.Start(":8080") }

func h(c echo.Context) error { return nil }
