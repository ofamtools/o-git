package main

import (
	"testing"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

func TestCatalogDeclaresSingularNamespace(t *testing.T) {
	doc := decode[ocli.CatalogDoc](t, run(t, "", "catalog"))
	if doc.Namespace != "git" || ochazuke.DefaultNamespace != "git" {
		t.Fatalf("catalog namespace %q, default %q; want git", doc.Namespace, ochazuke.DefaultNamespace)
	}
	if got, e := ocli.CatalogNamespace(doc, "o-git"); e != nil || got != "git" {
		t.Fatal(got, e)
	}
}
