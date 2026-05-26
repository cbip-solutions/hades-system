package link

import "testing"

// TestHTTPKeyCollapsesAllParamSyntaxes pins every parameter syntax from
// the spec §6 + §13.1 corpus to the OpenAPI canonical `/path/{param}` form.
// Sister-test to the extractor outputs of Phase D (chi/gin/echo) + Phase E
// (FastAPI / Next.js / NestJS) — the LINK side and the EXTRACT side MUST
// agree byte-for-byte on the key (otherwise the linker silently misses
// every link).
func TestHTTPKeyCollapsesAllParamSyntaxes(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   string
	}{

		{"GET", "/users/:id", "GET /users/{param}"},
		{"POST", "/orders/:orderID/items/:itemID", "POST /orders/{param}/items/{param}"},

		{"GET", "/users/[id]", "GET /users/{param}"},
		{"DELETE", "/users/[userId]/posts/[postId]", "DELETE /users/{param}/posts/{param}"},

		{"GET", "/users/{id:int}", "GET /users/{param}"},
		{"PUT", "/items/{itemId:uuid}", "PUT /items/{param}"},

		{"GET", "/users/{id}", "GET /users/{param}"},

		{"GET", "/users/<id>", "GET /users/{param}"},
		{"GET", "/users/<int:id>", "GET /users/{param}"},

		{"GET", "/users/${id}", "GET /users/{param}"},

		{"GET", "/", "GET /"},
		{"GET", "/health", "GET /health"},
		{"GET", "/api/v1/users", "GET /api/v1/users"},
		{"GET", "/users/", "GET /users/"},

		{"get", "/x", "GET /x"},
		{"Post", "/x", "POST /x"},

		{"GET", "/users/:id/posts/{postId}/comments/[commentId]", "GET /users/{param}/posts/{param}/comments/{param}"},

		{"GET", "/users/:id([0-9]+)", "GET /users/{param}"},
	}
	for _, c := range cases {
		got := HTTPKey(c.method, c.path)
		if got != c.want {
			t.Errorf("HTTPKey(%q, %q) = %q; want %q", c.method, c.path, got, c.want)
		}
	}
}

func TestGRPCKey(t *testing.T) {
	cases := []struct {
		pkg, svc, rpc, want string
	}{
		{"acme.v1", "OrderService", "PlaceOrder", "acme.v1.OrderService/PlaceOrder"},
		{"acme", "PingService", "Ping", "acme.PingService/Ping"},
	}
	for _, c := range cases {
		if got := GRPCKey(c.pkg, c.svc, c.rpc); got != c.want {
			t.Errorf("GRPCKey(%q,%q,%q) = %q; want %q", c.pkg, c.svc, c.rpc, got, c.want)
		}
	}
}

func TestGRPCKeyEmptyPackage(t *testing.T) {
	if got := GRPCKey("", "PingService", "Ping"); got != "PingService/Ping" {
		t.Errorf("GRPCKey(empty pkg) = %q; want PingService/Ping", got)
	}
}

func TestMQKey(t *testing.T) {
	if got := MQKey("orders.created"); got != "orders.created" {
		t.Errorf("MQKey = %q; want orders.created", got)
	}
}

func TestGraphQLKey(t *testing.T) {
	if got := GraphQLKey("Query", "users"); got != "Query.users" {
		t.Errorf("GraphQLKey = %q; want Query.users", got)
	}
}

func TestHTTPKeyEmptyPathIsNotNormalisedToBare(t *testing.T) {
	if got := HTTPKey("GET", ""); got != "GET " {

		t.Errorf("HTTPKey(GET, \"\") = %q; want \"GET \" (deterministic, empty preserved)", got)
	}
}
