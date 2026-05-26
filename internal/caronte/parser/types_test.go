package parser

import "testing"

func TestTreeTypeExported(t *testing.T) {
	var _ *Tree
	_ = t
}
