package main

import (
	"io"
	"strings"
)

// A workaround for ochazuke v0.1.1, copied from o-csv and o-json; it goes
// away with ochazuke v0.2.0.

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
