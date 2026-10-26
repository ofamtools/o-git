package main

import (
	"io"
	"strings"
)

// Workarounds for ochazuke v0.1.1, copied from o-csv and o-json; both go
// away with ochazuke v0.2.0.

// bareJSON drops a --json given without a value: the shell's --json takes
// arguments as JSON, and its output is always JSON, so o-git repo status
// --json means what it says.
func bareJSON(args []string) []string {
	out := make([]string, 0, len(args))
	for i, arg := range args {
		if arg == "--json" && (i+1 == len(args) || strings.HasPrefix(args[i+1], "--")) {
			continue
		}
		out = append(out, arg)
	}
	return out
}

// quietJobs is the shell's stderr minus its "job #N VERB done" lines.
// ochazuke/cli's Shell.Run (cli/shell.go, v0.1.1 line 188) prints one per
// job a command started, and every o-git verb is pending work; the
// operation's Quiet flag does not reach that line. A failed job's line and
// a confirm prompt pass through.
type quietJobs struct{ w io.Writer }

func (q quietJobs) Write(p []byte) (int, error) {
	if line := string(p); strings.HasPrefix(line, "job #") && strings.HasSuffix(line, " done\n") {
		return len(p), nil
	}
	return q.w.Write(p)
}
