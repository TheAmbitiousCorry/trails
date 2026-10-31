// Package gitx runs git for the staged-docs predicates: always as git -C <checkout toplevel>, and an unknown object or ref (exit 128) reads as false, never as an error.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

const timeout = 10 * time.Second

// Run runs git in top and returns trimmed stdout, whether it succeeded, and an error only when git could not run or exited with a code the caller has not named.
// Exit 128 (unknown object or ref, not a repository) is ok=false. Every code in falseOn is also ok=false.
// Callers that list paths pass --full-name so the paths are relative to the repository root whatever top is.
func Run(ctx context.Context, top string, falseOn []int, args ...string) (out string, ok bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", top}, args...)...)
	cmd.Stdout = &stdout
	err = cmd.Run()
	if err == nil {
		return strings.TrimSpace(stdout.String()), true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code := exit.ExitCode()
		if code == 128 {
			return "", false, nil
		}
		for _, c := range falseOn {
			if c == code {
				return "", false, nil
			}
		}
	}
	return "", false, err
}

// Out is Run with no extra false codes.
func Out(ctx context.Context, top string, args ...string) (string, bool, error) {
	return Run(ctx, top, nil, args...)
}

// IsAncestor reports whether a is an ancestor of b (a commit is its own ancestor). A sha git does not know is false.
func IsAncestor(ctx context.Context, top, a, b string) (bool, error) {
	_, ok, err := Run(ctx, top, []int{1}, "merge-base", "--is-ancestor", a, b)
	return ok, err
}

// Verify resolves rev (for example "refs/heads/main^{commit}") to a full sha; a rev git does not know is false.
func Verify(ctx context.Context, top, rev string) (string, bool) {
	out, ok, _ := Run(ctx, top, []int{1}, "rev-parse", "--verify", "--quiet", rev)
	return out, ok
}

// Lines splits Out's output into non-empty lines.
func Lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimRight(l, "\r"); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// BranchRef is the ref holding the branch: refs/heads/<branch> when it exists, else the first refs/remotes/<remote>/<branch>.
// A branch with neither is gone.
func BranchRef(ctx context.Context, top, branch string) (string, bool) {
	if branch == "" {
		return "", false
	}
	if _, ok := Verify(ctx, top, "refs/heads/"+branch+"^{commit}"); ok {
		return "refs/heads/" + branch, true
	}
	out, ok, _ := Out(ctx, top, "for-each-ref", "--format=%(refname)", "refs/remotes/")
	if !ok {
		return "", false
	}
	for _, r := range Lines(out) {
		if _, rest, found := strings.Cut(strings.TrimPrefix(r, "refs/remotes/"), "/"); found && rest == branch && !strings.HasSuffix(r, "/HEAD") {
			if _, ok := Verify(ctx, top, r+"^{commit}"); ok {
				return r, true
			}
		}
	}
	return "", false
}
