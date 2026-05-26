// SPDX-License-Identifier: MIT
package main

import "github.com/labstack/echo/v4"

type svc struct{}

func (s *svc) Health(c echo.Context) error { return nil }

func main() {
	s := &svc{}
	e := echo.New()
	e.GET("/health", s.Health)
	_ = e.Start(":8080")
}
