package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"

	"ofam.dev/ochazuke"
	"ofam.dev/oden"
)

// viewCLI preserves the original bare view argv grammar. Flags come from the
// registered operation; the adapter only folds its positional directory argv
// into the existing comma selector before calling that same operation.
func (a *App) viewCLI(ctx context.Context, words []string) error {
	op := a.r.Operations.At(ochazuke.Key(a.r.CanonicalKey("repo.view")))
	args := ochazuke.Arguments{}
	flags := flag.NewFlagSet("o-git view", flag.ContinueOnError)
	flags.SetOutput(a.Stderr)
	for name, param := range op.Parameters {
		flags.Var(&viewFlag{name: name, param: param, args: args}, name, param.Description)
	}
	if err := flags.Parse(words); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return &oden.ExitError{Code: 2}
	}
	if dirs := flags.Args(); len(dirs) > 0 {
		name := op.Positional[0]
		paths := strings.Join(dirs, ",")
		if existing := ochazuke.StringArg(args, name, ""); existing != "" {
			paths = existing + "," + paths
		}
		args[name] = paths
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return a.Shell().Run(ctx, []string{"repo", "view", "--json", string(encoded)})
}

type viewFlag struct {
	name  string
	param ochazuke.Parameter
	args  ochazuke.Arguments
}

func (f *viewFlag) String() string {
	if value := f.args[f.name]; value != nil {
		return fmt.Sprint(value)
	}
	return ""
}

func (f *viewFlag) Set(text string) error {
	value, err := ochazuke.ParseFlagValue(f.param.Type, text)
	if err != nil {
		return err
	}
	f.args[f.name] = value
	return nil
}

func (f *viewFlag) IsBoolFlag() bool { return f.param.Type == "boolean" }
