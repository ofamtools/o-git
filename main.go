// Command o-git is the family's git extension: the repositories o-proj
// knows (and any directory named), their worktrees and their branches,
// through the git CLI. Local only: it never fetches, pushes or tags.
//
//	o-git repo list|status|show|state [REPO] [--path DIR,...]
//	o-git worktree list [REPO] | add BRANCH [PATH] [--repo R] [--from REF] | remove --target worktree/PATH [--force] [--yes]
//	o-git branch list [REPO] | create NAME [FROM] [--switch] | switch NAME | delete --target branch/REPO:NAME [--force] [--yes]
//	o-git catalog
//
// Every command prints JSON (repo state: JSON Lines).
package main

import (
	"context"
	"io"
	"os"

	ocli "github.com/unixsuperhero/ochazuke/cli"
	"github.com/unixsuperhero/oden"
)

func main() {
	os.Exit(oden.Report(os.Stderr, progName(), Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)))
}

// progName is how messages name this command: the word under the parent
// oden app that ran it, else the bin name.
func progName() string {
	if parent := os.Getenv("ODEN_PARENT"); parent != "" {
		return parent + " git"
	}
	return "o-git"
}

// Main runs one invocation.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	a, err := New()
	if err != nil {
		return err
	}
	a.Stdin, a.Stdout, a.Stderr = stdin, stdout, stderr
	app := oden.New("o-git", bareJSON(args), oden.WithBuiltins(), oden.WithStdio(stdin, stdout, stderr))
	ocli.Mount(app, a.Shell())
	return app.Run(context.Background())
}
