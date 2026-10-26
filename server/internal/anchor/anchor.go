// Package anchor checks the claim behind an entry against the repository and the web, and derives the stale tier from the result.
package anchor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/TheAmbitiousCorry/trails/internal/store"
)

// Anchor mirrors an anchors row. Hash holds the sha for commit and produced_by anchors, and the content hash for the rest;
// transcript keeps its provider and id in Extra, and produced_by keeps its command in PathOrURL.
type Anchor struct {
	ID            int64  `json:"id"`
	EntryID       string `json:"entry_id"`
	Type          string `json:"type"`
	PathOrURL     string `json:"path_or_url,omitempty"`
	RangeStart    int    `json:"range_start,omitempty"`
	RangeEnd      int    `json:"range_end,omitempty"`
	Hash          string `json:"hash,omitempty"`
	FetchedAt     string `json:"fetched_at,omitempty"`
	LastCheckedAt string `json:"last_checked_at,omitempty"`
	OK            bool   `json:"ok"`
	Extra         string `json:"extra,omitempty"`
}

// Label is how a notice names the anchor to a reader.
func (a Anchor) Label() string {
	switch a.Type {
	case "range":
		return fmt.Sprintf("range %s:%d-%d", a.PathOrURL, a.RangeStart, a.RangeEnd)
	case "commit":
		return "commit " + a.Hash
	case "produced_by":
		return fmt.Sprintf("produced_by %s at %s", a.PathOrURL, a.Hash)
	case "transcript":
		return "transcript " + a.Extra
	}
	return a.Type + " " + a.PathOrURL
}

// Checker re-checks anchors against the repository at Repo and the web through Fetcher.
type Checker struct {
	Repo    string
	Fetcher Fetcher
}

var shaRe = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// Check reports whether the anchor still holds. A target that is gone or unreachable is stale, not an error;
// err is for a check that could not run at all (unknown type, git missing, cancelled context).
func (c Checker) Check(ctx context.Context, a Anchor) (bool, error) {
	switch a.Type {
	case "file", "range", "dir", "url":
		h, err := c.hash(ctx, a)
		if err != nil {
			return false, ctx.Err()
		}
		return h == a.Hash, nil
	case "commit":
		if !shaRe.MatchString(a.Hash) {
			return false, nil
		}
		err := exec.CommandContext(ctx, "git", "-C", c.Repo, "merge-base", "--is-ancestor", a.Hash, "HEAD").Run()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return false, nil
		}
		return err == nil, err
	case "transcript":
		return true, nil
	case "produced_by":
		head, err := c.head(ctx)
		if err != nil {
			return false, ctx.Err()
		}
		return head == a.Hash, nil
	}
	return false, fmt.Errorf("anchor type %q has no check", a.Type)
}

// Resolve fills Hash and FetchedAt at write time with the same functions Check compares against, and refuses an anchor that does not hold right now.
func (c Checker) Resolve(ctx context.Context, a Anchor) (Anchor, error) {
	switch a.Type {
	case "file", "range", "dir", "url":
		h, err := c.hash(ctx, a)
		if err != nil {
			return a, err
		}
		a.Hash = h
	case "produced_by":
		head, err := c.head(ctx)
		if err != nil {
			return a, err
		}
		a.Hash = head
	case "commit":
		ok, err := c.Check(ctx, a)
		if err != nil {
			return a, err
		}
		if !ok {
			return a, fmt.Errorf("commit %q is not an ancestor of HEAD", a.Hash)
		}
	case "transcript":
	default:
		return a, fmt.Errorf("anchor type %q has no check", a.Type)
	}
	now := store.Now()
	a.FetchedAt, a.LastCheckedAt, a.OK = now, now, true
	return a, nil
}

func (c Checker) head(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", c.Repo, "rev-parse", "HEAD").Output()
	return strings.TrimSpace(string(out)), err
}

// hash is the current content hash of a file, range, dir or url anchor.
func (c Checker) hash(ctx context.Context, a Anchor) (string, error) {
	if a.Type == "url" {
		if c.Fetcher == nil {
			return "", errors.New("no fetcher configured")
		}
		body, err := c.Fetcher.GetBytes(ctx, a.PathOrURL)
		if err != nil {
			return "", err
		}
		return sum(body), nil
	}
	if !filepath.IsLocal(a.PathOrURL) {
		return "", fmt.Errorf("path %q is not inside the repository", a.PathOrURL)
	}
	full := filepath.Join(c.Repo, a.PathOrURL)
	switch a.Type {
	case "file":
		b, err := os.ReadFile(full)
		if err != nil {
			return "", err
		}
		return sum(b), nil
	case "range":
		b, err := os.ReadFile(full)
		if err != nil {
			return "", err
		}
		lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		if a.RangeStart < 1 || a.RangeEnd < a.RangeStart || a.RangeEnd > len(lines) {
			return "", fmt.Errorf("lines %d-%d are outside %s", a.RangeStart, a.RangeEnd, a.PathOrURL)
		}
		return sum([]byte(strings.Join(lines[a.RangeStart-1:a.RangeEnd], "\n"))), nil
	default:
		var files []string
		err := filepath.WalkDir(full, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == ".git" {
				return filepath.SkipDir
			}
			if d.Type().IsRegular() {
				rel, err := filepath.Rel(full, p)
				if err != nil {
					return err
				}
				files = append(files, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		sort.Strings(files)
		return sum([]byte(strings.Join(files, "\n"))), nil
	}
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
