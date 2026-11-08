package main

// Rows are data: applications and owners can replace any one of them.
func (a *App) bindComparePage() error {
	return a.r.LoadDefaultBindings("compare-page", []byte(`{"bindings":[
 {"context":"compare-page/normal","bindings":[
  {"key":"f","action":"comparison.set","args":{"side":"from"},"hint":"from"},
  {"key":"t","action":"comparison.set","args":{"side":"to"},"hint":"to"},
  {"key":"~","action":"comparison.swap"},
  {"key":"F","action":"comparison.fetch"},
  {"key":"R","action":"comparison.reload"},
  {"key":"q","action":"app.quit"}
 ]},
 {"context":"comparison/normal","bindings":[
  {"key":"enter","action":"comparison.open"},
  {"key":"y y","action":"comparison.yank"}
 ]},
 {"context":"repo/normal","bindings":[{"key":"c","action":"repo.compare"},{"key":"F","action":"repo.fetch"}]},
 {"context":"worktree/normal","bindings":[{"key":"c","action":"worktree.compare"},{"key":"F","action":"repo.fetch"}]},
 {"context":"branch/normal","bindings":[{"key":"c","action":"branch.compare"},{"key":"F","action":"repo.fetch"}]}
 ]}`))
}
