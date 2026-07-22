// Package mcpserver exposes a run's branch-graph analysis to AI clients over the
// Model Context Protocol (stdio). Tools mirror the reverse-queries a human uses:
// where a fix landed, what a release contains, unreleased work, PRs by merge
// method, commit search.
package mcpserver

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wm-it-25/git-branch-graph/internal/query"
	"github.com/wm-it-25/git-branch-graph/internal/runs"
)

// serverInstructions orients the LLM to the whole toolset: what this server
// knows, the required workflow, and which tool answers which kind of question.
const serverInstructions = `git-branch-graph answers questions about the branch, merge, release and PR
state of analyzed git repositories.

Workflow:
1. Call list_repositories to get a run id (every other tool takes a "run" arg).
2. Optionally call repository_summary to learn the branch/ref names.
3. Then use the query tool that fits the question:
   - "did this fix ship / which release or branch contains commit X or PR #N?"  -> where_is
   - "what isn't released yet / diff two refs?"                                  -> unreleased_commits
   - "which version is on which environment / release overview?"                 -> releases
   - "which PRs were squash- vs merge-landed / PR list?"                         -> list_prs
   - "find the commit for a change described in words"                           -> search_commits (then where_is)

Notes:
- Refs are branch names (e.g. dev, master) or tags (e.g. w0.10.14). where_is
  accepts a commit SHA (full or prefix) or a bare PR number.
- Data is a point-in-time snapshot (see capturedAt); it is not live.
- Merge method is offline-classified (squash vs merge) and refined by PR data
  when available; PRs unverified against the repo are omitted from links.`

// Server serves MCP tools backed by run folders under DataDir.
type Server struct {
	DataDir string
}

// Run starts the MCP server on stdio and blocks until the client disconnects.
func (s *Server) Run(ctx context.Context) error {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "git-branch-graph",
		Version: "0.1.0",
	}, &mcp.ServerOptions{Instructions: serverInstructions})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_repositories",
		Title:       "List analyzed repositories",
		Description: "List the analyzed repositories (runs) available to query, with their id, default branch, and commit/branch/tag counts. ALWAYS call this first: every other tool needs a `run` id from here.",
	}, s.listRepositories)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "repository_summary",
		Title:       "Repository summary",
		Description: "Overview of one run: org/repo, default branch, commit & tag counts, and the list of branches (marking the default). Use to orient before drilling in, or to learn the branch/ref names other tools expect.",
	}, s.repositorySummary)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "where_is",
		Title:       "Where is this commit / PR?",
		Description: "Given a commit SHA or a PR number, return every branch and release tag that CONTAINS it. Use when asked 'did this fix ship?', 'which release has this fix?', 'is PR #123 in production?', or 'what version includes commit X?'. Returns empty tags if it is not in any release yet.",
	}, s.whereIs)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "unreleased_commits",
		Title:       "Unreleased commits (release readiness)",
		Description: "List commits present in one ref (`in`, e.g. the default branch) but not yet in another (`notIn`, e.g. the latest release tag). Use for 'what's not released yet?', 'what would the next release include?', or comparing two branches/tags. Get ref names from repository_summary or releases.",
	}, s.unreleasedCommits)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_prs",
		Title:       "List pull requests",
		Description: "List PRs, optionally filtered by how they landed (`method`: squash|merge|rebase) or `state` (merged|open|closed). Use for 'which PRs were squash-merged?', 'show merge-commit PRs', or auditing merge strategy.",
	}, s.listPRs)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "releases",
		Title:       "Release / environment matrix",
		Description: "Return release tags grouped into version families × environments (e.g. release / devnet / testnetboot), newest first. Use for 'which version is on devnet?', 'what environments has v1.2 reached?', or an overview of release state across environments.",
	}, s.releases)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_commits",
		Title:       "Search commits",
		Description: "Find commits whose subject or author matches a text query, or by PR number. Use to locate a commit/SHA when the user describes a change in words (e.g. 'the fee delegation fix') before passing it to where_is.",
	}, s.searchCommits)

	return srv.Run(ctx, &mcp.StdioTransport{})
}

// ---- inputs / outputs ----

type runInput struct {
	Run string `json:"run" jsonschema:"the run id from list_repositories"`
}

type listReposOut struct {
	Repositories []runs.Info `json:"repositories"`
}

func (s *Server) listRepositories(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listReposOut, error) {
	list, err := runs.List(s.DataDir)
	if err != nil {
		return nil, listReposOut{}, err
	}
	return nil, listReposOut{Repositories: list}, nil
}

type summaryOut struct {
	Org           string   `json:"org"`
	Repo          string   `json:"repo"`
	DefaultBranch string   `json:"defaultBranch"`
	HeadSHA       string   `json:"headSha"`
	Commits       int      `json:"commits"`
	Branches      []branch `json:"branches"`
	TagCount      int      `json:"tagCount"`
}
type branch struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

func (s *Server) repositorySummary(_ context.Context, _ *mcp.CallToolRequest, in runInput) (*mcp.CallToolResult, summaryOut, error) {
	db, err := s.open(in.Run)
	if err != nil {
		return nil, summaryOut{}, err
	}
	defer db.Close()
	var out summaryOut
	var head sql.NullString
	err = db.QueryRow(`SELECT org, repo, default_branch, head_sha, commit_count, tag_count FROM meta`).
		Scan(&out.Org, &out.Repo, &out.DefaultBranch, &head, &out.Commits, &out.TagCount)
	if err != nil {
		return nil, summaryOut{}, err
	}
	out.HeadSHA = head.String
	rows, err := db.Query(`SELECT ref_name, is_default FROM refs WHERE ref_type='branch' ORDER BY is_default DESC, ref_name`)
	if err != nil {
		return nil, summaryOut{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var b branch
		var def int
		if err := rows.Scan(&b.Name, &def); err != nil {
			return nil, summaryOut{}, err
		}
		b.IsDefault = def == 1
		out.Branches = append(out.Branches, b)
	}
	return nil, out, nil
}

type whereIsInput struct {
	Run string `json:"run" jsonschema:"the run id from list_repositories"`
	Ref string `json:"ref" jsonschema:"a commit SHA (full or prefix) or a PR number"`
}
type whereIsOut struct {
	SHA      string      `json:"sha" jsonschema:"the resolved commit SHA (a PR number is resolved to its merge commit)"`
	Branches []query.Ref `json:"branches" jsonschema:"branches whose history contains this commit"`
	Tags     []query.Ref `json:"tags" jsonschema:"release tags containing this commit (empty = not released yet)"`
}

func (s *Server) whereIs(_ context.Context, _ *mcp.CallToolRequest, in whereIsInput) (*mcp.CallToolResult, whereIsOut, error) {
	db, err := s.open(in.Run)
	if err != nil {
		return nil, whereIsOut{}, err
	}
	defer db.Close()
	sha := strings.TrimSpace(in.Ref)
	if isNumeric(sha) {
		if resolved, ok := query.ResolvePR(db, sha); ok {
			sha = resolved
		}
	}
	branches, tags, err := query.Containment(db, sha)
	if err != nil {
		return nil, whereIsOut{}, err
	}
	if branches == nil {
		branches = []query.Ref{}
	}
	if tags == nil {
		tags = []query.Ref{}
	}
	return nil, whereIsOut{SHA: sha, Branches: branches, Tags: tags}, nil
}

type unreleasedInput struct {
	Run   string `json:"run" jsonschema:"the run id"`
	In    string `json:"in" jsonschema:"ref that has the commits, e.g. the default branch 'dev'"`
	NotIn string `json:"notIn" jsonschema:"ref that lacks them, e.g. a release tag 'w0.10.14'"`
	Limit int    `json:"limit,omitempty" jsonschema:"max commits (default 500)"`
}
type commitsOut struct {
	Count   int            `json:"count" jsonschema:"number of commits returned"`
	Commits []query.Commit `json:"commits" jsonschema:"matching commits, newest first"`
}

func (s *Server) unreleasedCommits(_ context.Context, _ *mcp.CallToolRequest, in unreleasedInput) (*mcp.CallToolResult, commitsOut, error) {
	db, err := s.open(in.Run)
	if err != nil {
		return nil, commitsOut{}, err
	}
	defer db.Close()
	if in.In == "" || in.NotIn == "" {
		return nil, commitsOut{}, fmt.Errorf("both 'in' and 'notIn' are required")
	}
	commits, err := query.Diff(db, in.In, in.NotIn, in.Limit)
	if err != nil {
		return nil, commitsOut{}, err
	}
	return nil, commitsOut{Count: len(commits), Commits: commits}, nil
}

type listPRsInput struct {
	Run    string `json:"run" jsonschema:"the run id"`
	Method string `json:"method,omitempty" jsonschema:"filter by merge method: squash | merge | rebase"`
	State  string `json:"state,omitempty" jsonschema:"filter by state: merged | open | closed"`
	Limit  int    `json:"limit,omitempty" jsonschema:"max rows (default 200)"`
}
type prsOut struct {
	PRs []query.PR `json:"prs"`
}

func (s *Server) listPRs(_ context.Context, _ *mcp.CallToolRequest, in listPRsInput) (*mcp.CallToolResult, prsOut, error) {
	db, err := s.open(in.Run)
	if err != nil {
		return nil, prsOut{}, err
	}
	defer db.Close()
	prs, err := query.PRs(db, in.Method, in.State, in.Limit)
	if err != nil {
		return nil, prsOut{}, err
	}
	return nil, prsOut{PRs: prs}, nil
}

type releasesOut struct {
	Environments []string        `json:"environments"`
	Releases     []query.Release `json:"releases"`
}

func (s *Server) releases(_ context.Context, _ *mcp.CallToolRequest, in runInput) (*mcp.CallToolResult, releasesOut, error) {
	db, err := s.open(in.Run)
	if err != nil {
		return nil, releasesOut{}, err
	}
	defer db.Close()
	envs, rels, err := query.Releases(db)
	if err != nil {
		return nil, releasesOut{}, err
	}
	return nil, releasesOut{Environments: envs, Releases: rels}, nil
}

type searchInput struct {
	Run   string `json:"run" jsonschema:"the run id"`
	Query string `json:"query" jsonschema:"text to match in subject/author, or a PR number"`
	Limit int    `json:"limit,omitempty" jsonschema:"max results (default 100)"`
}

func (s *Server) searchCommits(_ context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, commitsOut, error) {
	db, err := s.open(in.Run)
	if err != nil {
		return nil, commitsOut{}, err
	}
	defer db.Close()
	if strings.TrimSpace(in.Query) == "" {
		return nil, commitsOut{}, fmt.Errorf("query is required")
	}
	commits, err := query.Search(db, in.Query, in.Limit)
	if err != nil {
		return nil, commitsOut{}, err
	}
	return nil, commitsOut{Count: len(commits), Commits: commits}, nil
}

// open validates the run id and opens its DB read-only.
func (s *Server) open(runID string) (*sql.DB, error) {
	if runID == "" || runID != filepath.Base(runID) || strings.HasPrefix(runID, ".") {
		return nil, fmt.Errorf("invalid run id %q", runID)
	}
	dir := filepath.Join(s.DataDir, runID)
	if _, err := os.Stat(filepath.Join(dir, "graph.sqlite")); err != nil {
		return nil, fmt.Errorf("no such run %q (use list_repositories)", runID)
	}
	return query.Open(dir)
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
