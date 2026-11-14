package main

import (
	"testing"

	"ofam.dev/ochazuke/refusecheck"
)

// o-git adds no refusal: an error template, Refuse, Unavailable, Lacks, a
// guard without a fallback, a Confirm without Loses or an Enum without
// Accept in the repository's non-test Go. The accepted sites are
// testdata/refusecheck.txt, which only shrinks (REFUSECHECK_UPDATE=1 removes
// rows and lowers counts). See ochazuke's refusecheck/README.md.
func TestNoNewRefusals(t *testing.T) {
	t.Parallel()
	refusecheck.Check(t, ".", "testdata/refusecheck.txt")
}
