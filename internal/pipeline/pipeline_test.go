package pipeline

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestOriginRef verifies that a local checkout's origin remote wins over the
// parent-directory org guess (which produced broken links like
// github.com/github/<repo> for repos checked out under a "github" folder).
func TestOriginRef(t *testing.T) {
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	t.Run("https origin", func(t *testing.T) {
		// Parent dir "github" reproduces the misleading org guess.
		dir := filepath.Join(t.TempDir(), "github", "some-repo")
		git("", "init", dir)
		git(dir, "remote", "add", "origin", "https://github.com/stable-net/stablenet-knowledge-mcp.git")

		ref, ok := originRef(dir)
		if !ok {
			t.Fatal("originRef: expected ok")
		}
		if ref.Org != "stable-net" || ref.Repo != "stablenet-knowledge-mcp" {
			t.Errorf("got org=%q repo=%q, want stable-net/stablenet-knowledge-mcp", ref.Org, ref.Repo)
		}
	})

	t.Run("ssh origin", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "work", "some-repo")
		git("", "init", dir)
		git(dir, "remote", "add", "origin", "git@github.com:acme/widgets.git")

		ref, ok := originRef(dir)
		if !ok {
			t.Fatal("originRef: expected ok")
		}
		if ref.Org != "acme" || ref.Repo != "widgets" {
			t.Errorf("got org=%q repo=%q, want acme/widgets", ref.Org, ref.Repo)
		}
	})

	t.Run("no origin falls back", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "github", "detached-repo")
		git("", "init", dir)

		if _, ok := originRef(dir); ok {
			t.Error("originRef: expected !ok for repo without origin")
		}
	})

	t.Run("local-path origin is ignored", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "github", "mirror-repo")
		git("", "init", dir)
		git(dir, "remote", "add", "origin", "/some/local/mirror.git")

		if _, ok := originRef(dir); ok {
			t.Error("originRef: expected !ok for local-path origin")
		}
	})
}
