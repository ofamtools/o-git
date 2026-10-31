// Command o-git is the family's git extension: the repositories o-proj
// knows (and any directory named), their worktrees and their branches,
// through the git CLI. Fetches only on explicit request; never pulls, pushes or tags.
//
//	o-git repo list|status|show|state [REPO] [--path DIR,...]
//	o-git worktree list [REPO] | add BRANCH [PATH] [--repo R] [--from REF] | remove PATH [--force] [--yes]
//	o-git branch list [REPO] | create NAME [FROM] [--switch] | switch NAME | delete NAME [--repo R] [--force] [--yes]
//	o-git [view] [--page git|compare] [--repo DIR] [--path DIR,...] [--smoke]
//	o-git catalog
//
// Every NOUN VERB command prints JSON (repo state: JSON Lines).
package main

import (
	"context"
	"io"
	"os"
	"strings"

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
	app := oden.New("o-git", args, oden.WithBuiltins(), oden.WithStdio(stdin, stdout, stderr),
		oden.WithDefault(func(_ context.Context, args []string) error {
			if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
				return oden.ErrNotFound
			}
			return runTUI(args, stdin, stdout, stderr)
		}))
	// App commands claim their names before framework noun aliases (view
	// is also the short name of ochazuke.view).
	app.Add(viewCommand(stdin, stdout, stderr))
	ocli.Mount(app, a.Shell())
	app.HelpText += `
Page:
  o-git (or o-git view) opens the Git page; --smoke renders one frame.`
	return app.Run(context.Background())
}
