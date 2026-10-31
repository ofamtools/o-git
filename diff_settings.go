package main

import (
	"fmt"

	"github.com/unixsuperhero/ochazuke"
)

const (
	defaultDiffLines = 20000
	maxDiffBytes     = 2 << 20 // retained hunk bytes per file; counts remain complete
)

func declareDiffSettings(r *ochazuke.Runtime) error {
	return r.DeclareSetting(
		ochazuke.Setting{Name: "compare.diff.context", Type: ochazuke.SettingInteger, Default: 3, Description: "Unified diff context lines", Validate: func(v any) error {
			if v.(int) < 0 {
				return fmt.Errorf("diff context must not be negative")
			}
			return nil
		}},
		ochazuke.Setting{Name: "compare.diff.max-lines", Type: ochazuke.SettingInteger, Default: defaultDiffLines, Description: "Maximum retained source lines per file in structured diffs; counts remain complete", Validate: func(v any) error {
			if v.(int) < 1 {
				return fmt.Errorf("diff max-lines must be positive")
			}
			return nil
		}},
	)
}
