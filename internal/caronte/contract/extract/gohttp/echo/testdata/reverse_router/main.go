// SPDX-License-Identifier: MIT
package main

import "github.com/labstack/echo/v4"

func main() {
	e := echo.New()
	e.GET("/users/:id", getUser).Name = "getUser"
	// e.Reverse is NOT a route — it's a reverse-lookup helper. The
	// extractor MUST NOT emit a row for Reverse calls.
	_ = e.Reverse("getUser", "42")
	_ = e.Start(":8080")
}

func getUser(c echo.Context) error { return nil }
