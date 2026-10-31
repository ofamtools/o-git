package main

import (
	"fmt"
	"time"

	"github.com/unixsuperhero/ochazuke"
)

var defaultEndpoints = []string{
	"head=HEAD", "worktree=WORKTREE", "index=INDEX", "main={main}", "upstream={upstream}",
	"origin/main=origin/{main}", "{remote}/main={remote}/{main}", "{remote}/branch={remote}/{branch}",
	"merge-base=merge-base({main})", "fork-point=fork-point(origin/{main} | {main})",
}

var defaultPairs = []string{
	"dirty=HEAD..WORKTREE", "upstream={upstream}..HEAD", "main={main}..HEAD",
	"origin/main=origin/{main}..HEAD", "{remote}/main={remote}/{main}..HEAD",
	"{remote}/branch={remote}/{branch}..HEAD", "merge-base=merge-base({main})..HEAD",
	"fork-point=fork-point(origin/{main} | {main})..HEAD",
}

// CompareConfig is copied on the event loop before a pending read starts.
type CompareConfig struct {
	Endpoints, Pairs []string
	Main             string
	MainNames        []string
	Collapse         bool
	DefaultPair      string
	StaleAfter       time.Duration
}

func declareCompareSettings(r *ochazuke.Runtime) error {
	return r.DeclareSetting(
		ochazuke.Setting{Name: "compare.endpoints", Type: ochazuke.SettingList, Default: defaultEndpoints, Description: "Named endpoint specs, NAME=SPEC; names may be used in pairs"},
		ochazuke.Setting{Name: "compare.pairs", Type: ochazuke.SettingList, Default: defaultPairs, Description: "Saved comparisons, NAME=FROM..TO; omitted TO means HEAD"},
		ochazuke.Setting{Name: "compare.main", Type: ochazuke.SettingString, Default: "", Description: "Main branch override; empty detects the remote default then main-names"},
		ochazuke.Setting{Name: "compare.main-names", Type: ochazuke.SettingList, Default: []string{"main", "master"}, Description: "Main branch candidates, in order"},
		ochazuke.Setting{Name: "compare.collapse", Type: ochazuke.SettingBoolean, Default: true, Description: "Join names of comparisons with the same resolved endpoints"},
		ochazuke.Setting{Name: "compare.default-pair", Type: ochazuke.SettingString, Default: "dirty", Description: "The initial comparison"},
		ochazuke.Setting{Name: "compare.stale-after", Type: ochazuke.SettingDuration, Default: 24 * time.Hour, Description: "Remote-tracking age before stale; zero disables staleness", Validate: func(v any) error {
			if v.(time.Duration) < 0 {
				return fmt.Errorf("stale-after must not be negative")
			}
			return nil
		}},
	)
}

func (a *App) compareConfig() CompareConfig {
	s := a.r.Settings()
	return CompareConfig{Endpoints: s.Strings("compare.endpoints"), Pairs: s.Strings("compare.pairs"), Main: s.String("compare.main"), MainNames: s.Strings("compare.main-names"), Collapse: s.Bool("compare.collapse"), DefaultPair: s.String("compare.default-pair"), StaleAfter: s.Duration("compare.stale-after")}
}
