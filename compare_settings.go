package main

import (
	"fmt"
	"time"

	"ofam.dev/ochazuke"
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
	StateDir         string
	Endpoints, Pairs []string
	Main             string
	MainNames        []string
	Collapse         bool
	DefaultPair      string
	StaleAfter       time.Duration
}

func declareCompareSettings(r *ochazuke.Runtime) error {
	return r.DeclareSetting(
		ochazuke.Setting{Name: "comparison.commit-search-depth", Type: ochazuke.SettingInteger, Default: 100, Description: "Commit history searched per chooser block; zero disables automatic commit search", Validate: func(v any) error {
			if v.(int) < 0 {
				return fmt.Errorf("commit-search-depth must not be negative")
			}
			return nil
		}},
		ochazuke.Setting{Name: "comparison.pairs.width-percent", Type: ochazuke.SettingInteger, Default: 40, Description: "Maximum wide-layout share for Pairs; 60 restores the former share", Validate: func(v any) error {
			if n := v.(int); n < 25 || n > 75 {
				return fmt.Errorf("width-percent must be between 25 and 75")
			}
			return nil
		}},
		ochazuke.Setting{Name: "comparison.files.follow-commit", Type: ochazuke.SettingBoolean, Default: true, Description: "Follow the focused commit's files; off loads only on Enter"},
		ochazuke.Setting{Name: "comparison.chooser.resolve-typed", Type: ochazuke.SettingBoolean, Default: true, Description: "Resolve typed endpoint specs in the picker and rank them first"},
		ochazuke.Setting{Name: "comparison.copy-feedback", Type: ochazuke.SettingBoolean, Default: true, Description: "Show a plain notice after copying a comparison range"},
		ochazuke.Setting{Name: "comparison.endpoints", Type: ochazuke.SettingList, Default: defaultEndpoints, Description: "Named endpoint specs, NAME=SPEC; names may be used in pairs"},
		ochazuke.Setting{Name: "comparison.pairs", Type: ochazuke.SettingList, Default: defaultPairs, Description: "Saved comparisons, NAME=FROM..TO; omitted TO means HEAD"},
		ochazuke.Setting{Name: "comparison.main", Type: ochazuke.SettingString, Default: "", Description: "Main branch override; empty detects the remote default then main-names"},
		ochazuke.Setting{Name: "comparison.main-names", Type: ochazuke.SettingList, Default: []string{"main", "master"}, Description: "Main branch candidates, in order"},
		ochazuke.Setting{Name: "comparison.collapse", Type: ochazuke.SettingBoolean, Default: true, Description: "Join names of comparisons with the same resolved endpoints"},
		ochazuke.Setting{Name: "comparison.default-pair", Type: ochazuke.SettingString, Default: "dirty", Description: "The initial comparison"},
		ochazuke.Setting{Name: "comparison.stale-after", Type: ochazuke.SettingDuration, Default: 24 * time.Hour, Description: "Remote-tracking age before stale; zero disables staleness", Validate: func(v any) error {
			if v.(time.Duration) < 0 {
				return fmt.Errorf("stale-after must not be negative")
			}
			return nil
		}},
	)
}

func (a *App) compareConfig() CompareConfig {
	s := a.r.Settings()
	return CompareConfig{StateDir: a.r.Config().StateDirectory(), Endpoints: s.Strings("comparison.endpoints"), Pairs: s.Strings("comparison.pairs"), Main: s.String("comparison.main"), MainNames: s.Strings("comparison.main-names"), Collapse: s.Bool("comparison.collapse"), DefaultPair: s.String("comparison.default-pair"), StaleAfter: s.Duration("comparison.stale-after")}
}
