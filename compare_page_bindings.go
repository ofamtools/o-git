package main

// Rows are data: applications and owners can replace any one of them.
func (a *App) bindComparePage() error {
	return a.r.LoadDefaultBindings("compare-page", []byte(`{"bindings":[
 {"context":"compare-page/normal","bindings":[
  {"key":"f","action":"compare.set","args":{"side":"from"},"hint":"from"},
  {"key":"t","action":"compare.set","args":{"side":"to"},"hint":"to"},
  {"key":"~","action":"compare.swap"},
  {"key":"F","action":"compare.fetch"},
  {"key":"R","action":"compare.reload"},
  {"key":"q","action":"app.quit"}
 ]},
 {"context":"compare/normal","bindings":[
  {"key":"enter","action":"compare.open"},
  {"key":"y y","action":"compare.copy"}
 ]},
 {"context":"repo/normal","bindings":[{"key":"c","action":"repo.compare"},{"key":"F","action":"repo.fetch"}]},
 {"context":"worktree/normal","bindings":[{"key":"c","action":"worktree.compare"},{"key":"F","action":"repo.fetch"}]},
 {"context":"branch/normal","bindings":[{"key":"c","action":"branch.compare"},{"key":"F","action":"repo.fetch"}]}
 ]}`))
}
