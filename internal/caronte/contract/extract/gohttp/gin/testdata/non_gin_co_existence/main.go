// SPDX-License-Identifier: MIT
package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()
	r.GET("/gin", ginHandler)

	http.HandleFunc("/legacy", legacyHandler)
	_ = r.Run(":8080")
}

func ginHandler(c *gin.Context)                              { c.JSON(200, gin.H{}) }
func legacyHandler(w http.ResponseWriter, req *http.Request) { w.WriteHeader(http.StatusOK) }
