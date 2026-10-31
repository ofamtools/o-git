package main

import (
	"fmt"

	"github.com/unixsuperhero/ochazuke"
)

func declareListsSettings(r *ochazuke.Runtime) error {
	return r.DeclareSetting(
		ochazuke.Setting{Name: "compare.commits.split", Type: ochazuke.SettingBoolean, Default: true, Description: "Group commit rows into Ahead and Behind sections; false shows < or > instead"},
		ochazuke.Setting{Name: "compare.commits.limit", Type: ochazuke.SettingInteger, Default: 500, Description: "Maximum commit rows per side, followed by an N more row", Validate: func(v any) error {
			if v.(int) < 0 {
				return fmt.Errorf("commit limit must not be negative")
			}
			return nil
		}},
	)
}
