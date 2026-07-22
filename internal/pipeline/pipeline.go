// Package pipeline runs the end-to-end ingest (acquire → extract → enrich →
// ontology) behind a single entry point with staged progress callbacks, shared
// by the CLI and the HTTP server.
package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wm-it-25/git-branch-graph/internal/acquire"
	"github.com/wm-it-25/git-branch-graph/internal/db"
	"github.com/wm-it-25/git-branch-graph/internal/enrich"
	"github.com/wm-it-25/git-branch-graph/internal/extract"
	"github.com/wm-it-25/git-branch-graph/internal/model"
	"github.com/wm-it-25/git-branch-graph/internal/ontology"
	"github.com/wm-it-25/git-branch-graph/internal/paths"
)

// Progress reports a stage transition: a short stage key, an overall percent
// [0,100], and a human message.
type Progress func(stage string, pct int, msg string)

// Options configure one ingest run.
type Options struct {
	Input         string // GitHub URL, local repo path, or an existing run folder
	DataDir       string
	DefaultBranch string // override (local clones)
	Repo          string // canonical owner/name override
	NoEnrich      bool
	Force         bool
}

// Result identifies the produced (or reused) run.
type Result struct {
	RunID    string // folder name (under DataDir) or absolute path (external run)
	RunDir   string
	Existing bool // input was an already-analyzed folder
}

func noop(string, int, string) {}

// Run executes the pipeline for opts.Input, streaming progress.
func Run(opts Options, prog Progress) (Result, error) {
	if prog == nil {
		prog = noop
	}
	prog("resolve", 3, "Resolving input…")

	// (1) Already-analyzed folder → just point the viewer at it.
	if id, dir, ok := existingRun(opts.Input, opts.DataDir); ok {
		prog("load", 60, "Found existing analysis")
		prog("done", 100, "Ready")
		return Result{RunID: id, RunDir: dir, Existing: true}, nil
	}

	remote := strings.Contains(opts.Input, "://") || strings.HasPrefix(opts.Input, "git@")
	ref := paths.ParseRepoRef(opts.Input)
	if opts.Repo != "" {
		o, r, ok := splitRepo(opts.Repo)
		if !ok {
			return Result{}, fmt.Errorf("--repo must be owner/name, got %q", opts.Repo)
		}
		ref.Org, ref.Repo, ref.Slug = o, r, o+"__"+r
	}

	// (2) Acquire — clone (remote) or mirror (local path), bare + blobless.
	if remote {
		prog("acquire", 12, fmt.Sprintf("Cloning %s/%s…", ref.Org, ref.Repo))
	} else {
		prog("acquire", 12, fmt.Sprintf("Reading local repo %s…", ref.Repo))
	}
	acq, err := acquire.Ensure(opts.DataDir, ref, opts.DefaultBranch)
	if err != nil {
		return Result{}, err
	}

	runDir := paths.RunDir(opts.DataDir, ref, acq.DefaultBranch, acq.HeadSHA)
	metaPath := filepath.Join(runDir, "meta.json")
	if !opts.Force {
		if _, err := os.Stat(metaPath); err == nil {
			prog("done", 100, "Cached (unchanged)")
			return Result{RunID: filepath.Base(runDir), RunDir: runDir}, nil
		}
	}

	// (3) Extract — git 1-pass → raw CSV.
	prog("extract", 40, "Extracting commits & refs…")
	commits, refs, edges, err := extract.Scan(acq.BareDir, acq.DefaultBranch)
	if err != nil {
		return Result{}, err
	}
	res, err := extract.WriteCSVs(runDir, commits, refs, edges)
	if err != nil {
		return Result{}, err
	}
	snap := model.Snapshot{
		Ref: ref, DefaultBranch: acq.DefaultBranch, HeadSHA: acq.HeadSHA,
		CapturedAt:  time.Now().Format(time.RFC3339),
		CommitCount: res.Commits, BranchCount: res.Branches, TagCount: res.Tags,
	}
	if err := writeMeta(metaPath, snap); err != nil {
		return Result{}, err
	}

	// (4) Enrich — GitHub PR/CI (optional).
	var enriched map[string]model.PR
	if !opts.NoEnrich {
		prog("enrich", 62, "Fetching PR metadata…")
		enriched = tryEnrich(ref, commits, prog)
	}

	// (5) Ontology — lanes, columns, containment, classification.
	prog("ontology", 85, "Computing branch graph…")
	if err := BuildOntology(runDir, snap, commits, refs, edges, enriched); err != nil {
		return Result{}, err
	}

	prog("done", 100, "Ready")
	return Result{RunID: filepath.Base(runDir), RunDir: runDir}, nil
}

// existingRun reports whether input is an already-analyzed run folder and its
// serving id (folder name when under DataDir, else an absolute path).
func existingRun(input, dataDir string) (id, dir string, ok bool) {
	fi, err := os.Stat(input)
	if err != nil || !fi.IsDir() {
		return "", "", false
	}
	if _, err := os.Stat(filepath.Join(input, "graph.json")); err != nil {
		return "", "", false
	}
	abs, _ := filepath.Abs(input)
	absData, _ := filepath.Abs(dataDir)
	if rel, err := filepath.Rel(absData, abs); err == nil && rel == filepath.Base(abs) {
		return filepath.Base(abs), abs, true // direct child of DataDir
	}
	return abs, abs, true // external folder — served by absolute path
}

// BuildOntology computes and writes graph.json + graph.sqlite + prs.csv.
func BuildOntology(runDir string, snap model.Snapshot, commits []model.Commit, refs []model.Ref, edges []model.Edge, enriched map[string]model.PR) error {
	g := ontology.Build(snap, commits, refs, edges, enriched)
	if err := ontology.WriteJSON(filepath.Join(runDir, "graph.json"), g); err != nil {
		return fmt.Errorf("graph.json: %w", err)
	}
	if err := extract.WritePRs(runDir, g.PRs); err != nil {
		return fmt.Errorf("prs.csv: %w", err)
	}
	if _, err := db.Write(filepath.Join(runDir, "graph.sqlite"), g, refs, edges); err != nil {
		return fmt.Errorf("graph.sqlite: %w", err)
	}
	return nil
}

// tryEnrich returns nil only when enrich did NOT run (no token). When a token is
// present it returns a non-nil (possibly empty) map, which signals downstream
// that PR verification is meaningful.
func tryEnrich(ref model.RepoRef, commits []model.Commit, prog Progress) map[string]model.PR {
	token := enrich.Token()
	if token == "" {
		return nil
	}
	var nums []string
	for _, c := range commits {
		if c.PRNum != "" {
			nums = append(nums, c.PRNum)
		}
	}
	if len(nums) == 0 {
		return map[string]model.PR{} // ran, nothing to fetch
	}
	prog("enrich", 64, fmt.Sprintf("Fetching %d PRs from GitHub…", len(nums)))
	prs, _ := enrich.Fetch(ref.Org, ref.Repo, token, nums)
	if prs == nil {
		prs = map[string]model.PR{}
	}
	return prs
}

func writeMeta(path string, s model.Snapshot) error {
	m := map[string]any{
		"repo_url": s.Ref.URL, "org": s.Ref.Org, "repo": s.Ref.Repo,
		"default_branch": s.DefaultBranch, "head_sha": s.HeadSHA,
		"captured_at": s.CapturedAt, "commit_count": s.CommitCount,
		"branch_count": s.BranchCount, "tag_count": s.TagCount,
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func splitRepo(s string) (owner, name string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(s), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), true
}
