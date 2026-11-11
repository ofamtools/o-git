package main

import (
	"encoding/json"
	"io"

	"ofam.dev/ochazuke"
)

type diffSink struct {
	path  string
	force bool
}

func diffDestination(args ochazuke.Arguments) diffSink {
	return diffSink{path: ochazuke.StringArg(args, "destination", ""), force: boolArg(args, "force")}
}

func (sink diffSink) write(results []DiffResult, plain bool) (any, error) {
	if sink.path == "" {
		return diffOutput(results, plain), nil
	}
	path := absPath(sink.path)
	err := exportToFile(path, sink.force, func(w io.Writer) error { return writeDiffOutput(w, results, plain) })
	if err != nil {
		return nil, err
	}
	return map[string]any{"destination": path}, nil
}

func writeDiffOutput(w io.Writer, results []DiffResult, plain bool) error {
	if !plain {
		return json.NewEncoder(w).Encode(diffOutput(results, false))
	}
	for _, result := range results {
		if _, err := io.WriteString(w, result.Unified); err != nil {
			return err
		}
	}
	return nil
}
