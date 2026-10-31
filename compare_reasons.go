package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

func shortRef(ref string) string {
	for _, prefix := range []string{"refs/heads/", "refs/remotes/", "refs/tags/"} {
		if strings.HasPrefix(ref, prefix) {
			return strings.TrimPrefix(ref, prefix)
		}
	}
	return ref
}

func resolvedRef(e Endpoint) string {
	if e.Ref == "" {
		return e.Spec
	}
	if e.Ref == e.SHA {
		return shortSHA(e.SHA)
	}
	return shortRef(e.Ref)
}

func timedOut(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timed out")
}

func (r *EndpointResolver) refFailure(ref string, err error) string {
	if timedOut(err) {
		return "ref resolution timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "ref resolution cancelled"
	}
	name := shortRef(ref)
	var ge *GitError
	if errors.As(err, &ge) && strings.Contains(ge.Stderr, "expected commit type") {
		return name + " is not a commit"
	}
	if v, ok := r.refs[ref]; ok && v.objectType != "commit" && v.peeledType != "commit" {
		return name + " is not a commit"
	}
	// This is a diagnostic for an absent ref, not an identity or fetch
	// dependency. A successfully resolved local josh/* branch stays local.
	remote, branch, ok := strings.Cut(strings.TrimPrefix(ref, "refs/remotes/"), "/")
	if ok && slices.Contains(r.remotes, remote) && !strings.ContainsAny(branch, "~^:@{}") {
		return fmt.Sprintf("%s has no %s branch (not pushed, or not fetched)", remote, branch)
	}
	return "no ref named " + name
}

func remoteEndpointError(remote string) error {
	if strings.ContainsAny(remote, "|()") {
		return fmt.Errorf("remote name %q contains endpoint syntax", remote)
	}
	return nil
}
