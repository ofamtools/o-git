package main

import (
	"testing"

	"github.com/unixsuperhero/ochazuke/strictcheck"
)

func TestNoDeprecatedOchazuke(t *testing.T) { strictcheck.Check(t) }
