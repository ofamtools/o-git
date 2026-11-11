package main

import "ofam.dev/ochazuke"

// defineStores makes each list noun a thing noun over its list verb
// (ochazuke.DefineListFacts), so the core verbs get, list, create, set, delete,
// archive and edit exist on it: the tool only reads these, so they are
// facts nouns, and the verbs the tool registers under a core name (branch
// create and delete, compare set, every list) stay and win. The key is the
// row's id (the one a list prints).
func (a *App) defineStores() {
	repo := repoParameter()
	inRepo := map[string]ochazuke.Parameter{"repo": repo}
	pair := map[string]ochazuke.Parameter{
		"repo": repo,
		"pair": {Type: "string", Description: "comparison as in comparison.inspect; --pair excludes --from/--to"},
		"from": {Type: "string", Description: "from endpoint; excludes --pair"},
		"to":   {Type: "string", Description: "to endpoint; excludes --pair"},
	}
	a.r.DefineListFacts([]ochazuke.ListFacts{
		{Noun: "repo", Sample: Repo{}, ListParams: map[string]ochazuke.Parameter{"path": directoriesParameter()}, List: a.repoList},
		{Noun: "worktree", Sample: WorktreeRow{}, ListParams: inRepo, List: a.worktreeList},
		{Noun: "branch", Sample: BranchRow{}, ListParams: inRepo, List: a.branchList},
		{Noun: "commit", Sample: Commit{}, ListParams: pair, List: a.commitList},
		{Noun: "change", Sample: Change{}, ListParams: pair, List: a.changeList},
		{Noun: "endpoint", Sample: Endpoint{}, ListParams: inRepo, List: a.endpointList},
		{Noun: "comparison", Sample: Comparison{}, ListParams: inRepo, List: a.compareList},
		{Noun: "diff-file", Sample: DiffFileRow{}, ListParams: pair, List: a.diffFileList},
	}...)
	a.typePathProps()
	a.scopeFactGets()
}
