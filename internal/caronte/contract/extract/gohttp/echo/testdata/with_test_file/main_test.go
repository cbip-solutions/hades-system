package main

import "github.com/labstack/echo/v4"

func testReg() {
	e := echo.New()
	e.GET("/test-only", func(c echo.Context) error { return nil })
}
