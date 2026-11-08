package main

import "ofam.dev/ochazuke"

// Namespace is the singular namespace o-git declares (lexicon plan): its
// operations are git.<noun>.<verb> and a host mounts its catalog under it.
const Namespace = "git"

func init() {
	if err := ochazuke.DeclareNamespace(Namespace); err != nil {
		panic(err)
	}
}
