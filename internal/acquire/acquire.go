// Package acquire clones or updates a bare, blobless mirror of a repository.
package acquire

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wm-it-25/git-branch-graph/internal/gitcmd"
	"github.com/wm-it-25/git-branch-graph/internal/model"
	"github.com/wm-it-25/git-branch-graph/internal/paths"
)

// Result reports the local bare repo plus resolved snapshot state.
type Result struct {
	BareDir       string
	DefaultBranch string
	HeadSHA       string
}

// Ensure clones the repo (bare + blobless) if missing, otherwise fetches
// incrementally. It then resolves the default branch and its HEAD SHA.
//
// defaultOverride, when non-empty, forces the default branch (useful for local
// clones whose HEAD does not match the intended default).
func Ensure(dataDir string, ref model.RepoRef, defaultOverride string) (Result, error) {
	bare := paths.BareRepoDir(dataDir, ref)

	// A mirror is only reusable if it holds a valid HEAD; a dir left behind by a
	// killed/partial clone must be discarded and re-cloned, not fetched into.
	if valid(bare) {
		if _, err := gitcmd.Run(bare, "fetch", "--prune", "--tags", "origin",
			"+refs/heads/*:refs/heads/*"); err != nil {
			return Result{}, fmt.Errorf("fetch: %w", err)
		}
	} else {
		_ = os.RemoveAll(bare) // drop any partial leftover
		if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
			return Result{}, err
		}
		// bare + blobless; abort on a stalled transfer (no interactive prompts —
		// see gitcmd.gitEnv) so the server never hangs indefinitely.
		if _, err := gitcmd.Run("",
			"-c", "http.lowSpeedLimit=1000", "-c", "http.lowSpeedTime=30",
			"clone", "--bare", "--filter=blob:none", ref.URL, bare); err != nil {
			_ = os.RemoveAll(bare) // don't leave a partial clone behind
			return Result{}, fmt.Errorf("clone %s: %w", ref.URL, err)
		}
	}

	def := defaultOverride
	if def == "" {
		// clone HEAD symref = remote default branch (for remote URLs).
		if head, err := gitcmd.Run(bare, "symbolic-ref", "--short", "HEAD"); err == nil {
			def = strings.TrimSpace(head)
		}
	}
	if def == "" {
		return Result{}, fmt.Errorf("could not resolve default branch; pass --default-branch")
	}

	headSHA, err := gitcmd.Run(bare, "rev-parse", def)
	if err != nil {
		return Result{}, fmt.Errorf("resolve %s: %w", def, err)
	}

	return Result{BareDir: bare, DefaultBranch: def, HeadSHA: strings.TrimSpace(headSHA)}, nil
}

// valid reports whether bare is an existing repo with a resolvable HEAD.
func valid(bare string) bool {
	if _, err := os.Stat(bare); err != nil {
		return false
	}
	if _, err := gitcmd.Run(bare, "rev-parse", "--verify", "HEAD"); err != nil {
		return false
	}
	return true
}
