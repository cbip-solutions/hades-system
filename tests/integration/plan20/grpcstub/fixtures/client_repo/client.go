// SPDX-License-Identifier: MIT
// hand-written stub package; the import is the load-bearing artifact
// Phase F's link resolver elevates to exact_proto_import confidence.
package main

import (
	"context"
	"log"

	greeter "client_repo/stub"
)

func main() {
	c := greeter.NewGreeterClient(nil)
	reply, err := c.Hello(context.Background(), &greeter.GreeterRequest{Name: "alice"})
	if err != nil {
		log.Fatal(err)
	}
	log.Println(reply.Message)
}
