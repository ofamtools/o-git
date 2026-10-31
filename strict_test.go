package main

import (
	"testing"

	"ofam.dev/ochazuke/strictcheck"
)

func TestNoDeprecatedOchazuke(t *testing.T) { strictcheck.Check(t) }
