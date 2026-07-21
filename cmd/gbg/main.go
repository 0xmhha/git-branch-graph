// Command gbg is the git-branch-graph core CLI.
//
// Usage:
//
//	gbg ingest <github-url-or-local-path> [flags]
//
// Flags:
//
//	--data-dir string        output root (default "./data")
//	--default-branch string  override default branch (for local clones)
//	--force                  re-extract even if the run folder already exists
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wm-it-25/git-branch-graph/internal/acquire"
	"github.com/wm-it-25/git-branch-graph/internal/extract"
	"github.com/wm-it-25/git-branch-graph/internal/model"
	"github.com/wm-it-25/git-branch-graph/internal/paths"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ingest":
		if err := runIngest(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "gbg ingest <github-url-or-local-path> [--data-dir dir] [--default-branch b] [--force]")
}

func runIngest(args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	dataDir := fs.String("data-dir", "./data", "output root directory")
	defBranch := fs.String("default-branch", "", "override default branch")
	force := fs.Bool("force", false, "re-extract even if run folder exists")

	// The stdlib flag parser stops at the first positional, so pull the URL out
	// first — this lets flags appear before OR after the URL.
	input, rest := splitPositional(args)
	if input == "" {
		return fmt.Errorf("missing repository URL or path")
	}
	_ = fs.Parse(rest)
	ref := paths.ParseRepoRef(input)
	fmt.Printf("[1/3] acquire  %s (%s/%s)\n", ref.URL, ref.Org, ref.Repo)

	acq, err := acquire.Ensure(*dataDir, ref, *defBranch)
	if err != nil {
		return err
	}
	fmt.Printf("      default=%s head=%s\n", acq.DefaultBranch, short(acq.HeadSHA))

	runDir := paths.RunDir(*dataDir, ref, acq.DefaultBranch, acq.HeadSHA)
	metaPath := filepath.Join(runDir, "meta.json")

	if !*force {
		if _, err := os.Stat(metaPath); err == nil {
			fmt.Printf("[cache] HEAD unchanged -> reusing %s\n", runDir)
			fmt.Printf("done: %s\n", runDir)
			return nil
		}
	}

	fmt.Printf("[2/3] extract  git 1-pass -> raw/*.csv\n")
	res, err := extract.Run(acq.BareDir, runDir, acq.DefaultBranch)
	if err != nil {
		return err
	}
	fmt.Printf("      commits=%d branches=%d tags=%d\n", res.Commits, res.Branches, res.Tags)

	fmt.Printf("[3/3] meta     meta.json\n")
	snap := model.Snapshot{
		Ref:           ref,
		DefaultBranch: acq.DefaultBranch,
		HeadSHA:       acq.HeadSHA,
		CapturedAt:    time.Now().Format(time.RFC3339),
		CommitCount:   res.Commits,
		BranchCount:   res.Branches,
		TagCount:      res.Tags,
	}
	if err := writeMeta(metaPath, snap); err != nil {
		return err
	}

	fmt.Printf("done: %s\n", runDir)
	return nil
}

// writeMeta serializes the snapshot as meta.json.
func writeMeta(path string, s model.Snapshot) error {
	m := map[string]any{
		"repo_url":       s.Ref.URL,
		"org":            s.Ref.Org,
		"repo":           s.Ref.Repo,
		"default_branch": s.DefaultBranch,
		"head_sha":       s.HeadSHA,
		"captured_at":    s.CapturedAt,
		"commit_count":   s.CommitCount,
		"branch_count":   s.BranchCount,
		"tag_count":      s.TagCount,
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// splitPositional returns the first non-flag argument (the repo URL/path) and
// the remaining args with it removed, so flags may appear on either side.
// A value following a known value-flag is skipped so it isn't mistaken for the
// positional (e.g. "--data-dir ./out <url>").
func splitPositional(args []string) (pos string, rest []string) {
	valueFlags := map[string]bool{
		"--data-dir": true, "-data-dir": true,
		"--default-branch": true, "-default-branch": true,
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if pos == "" && len(a) > 0 && a[0] != '-' {
			pos = a
			continue
		}
		rest = append(rest, a)
		// skip the value that belongs to a "--flag value" form
		if valueFlags[a] && i+1 < len(args) {
			i++
			rest = append(rest, args[i])
		}
	}
	return pos, rest
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
