// SPDX-License-Identifier: MIT
package main

import labecho "github.com/labstack/echo/v4"

func main() {
	e := labecho.New()
	e.GET("/health", h)
	_ = e.Start(":8080")
}

func h(c labecho.Context) error { return nil }
