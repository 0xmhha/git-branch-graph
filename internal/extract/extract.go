// Package extract performs the local git 1-pass scan that produces the raw CSV
// layer: commits.csv, refs.csv, edges.csv.
package extract

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/wm-it-25/git-branch-graph/internal/csvw"
	"github.com/wm-it-25/git-branch-graph/internal/gitcmd"
	"github.com/wm-it-25/git-branch-graph/internal/model"
)

const (
	us = "\x1f" // unit separator: between git format fields
	rs = "\x1e" // record separator: between commits
)

// prNum captures the "(#123)" pattern common to squash/merge subjects.
var prNum = regexp.MustCompile(`\(#(\d+)\)`)

// parsePR returns the PR number from a commit subject ("" if none). When a
// subject contains multiple "(#N)" tokens, the last one wins — that is the
// merge/squash PR, not an inline reference.
func parsePR(subject string) string {
	m := prNum.FindAllStringSubmatch(subject, -1)
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1][1]
}

// Result summarizes what was written.
type Result struct {
	Commits  int
	Branches int
	Tags     int
	RawDir   string
}

// Scan performs the git 1-pass read without writing anything, returning the
// in-memory raw layer. Callers that also want CSVs call WriteCSVs.
func Scan(bareDir, defaultBranch string) (commits []model.Commit, refs []model.Ref, edges []model.Edge, err error) {
	commits, edges, err = scanCommits(bareDir)
	if err != nil {
		return nil, nil, nil, err
	}
	refs, _, _, err = scanRefs(bareDir, defaultBranch)
	if err != nil {
		return nil, nil, nil, err
	}
	return commits, refs, edges, nil
}

// WriteCSVs writes the raw layer to outDir/raw/*.csv.
func WriteCSVs(outDir string, commits []model.Commit, refs []model.Ref, edges []model.Edge) (Result, error) {
	rawDir := filepath.Join(outDir, "raw")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		return Result{}, err
	}
	if err := writeCommits(filepath.Join(rawDir, "commits.csv"), commits); err != nil {
		return Result{}, err
	}
	if err := writeEdges(filepath.Join(rawDir, "edges.csv"), edges); err != nil {
		return Result{}, err
	}
	if err := writeRefs(filepath.Join(rawDir, "refs.csv"), refs); err != nil {
		return Result{}, err
	}
	branches, tags := 0, 0
	for _, r := range refs {
		if r.Type == "tag" {
			tags++
		} else {
			branches++
		}
	}
	return Result{Commits: len(commits), Branches: branches, Tags: tags, RawDir: rawDir}, nil
}

// cherryMarker matches git's `-x` annotation: "(cherry picked from commit <sha>)".
var cherryMarker = regexp.MustCompile(`cherry picked from commit ([0-9a-f]{7,40})`)

// ScanCherryPicks finds commits carrying a `-x` cherry-pick marker and maps each
// to its source commit SHA. Exact and offline (message-only; no blobs). When a
// commit was cherry-picked through several branches it carries multiple markers;
// the last (most immediate source) is used.
func ScanCherryPicks(bareDir string) (map[string]string, error) {
	// Only commits that mention the marker — cheap even on huge repos.
	out, err := gitcmd.RunStream(bareDir, "log", "--all", "--no-abbrev",
		"--grep=cherry picked from commit",
		"--pretty=format:%H%x1f%b"+rs)
	if err != nil {
		return nil, err
	}
	picks := map[string]string{}
	for _, rec := range strings.Split(string(out), rs) {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, us, 2)
		if len(f) < 2 {
			continue
		}
		if m := cherryMarker.FindAllStringSubmatch(f[1], -1); len(m) > 0 {
			picks[f[0]] = m[len(m)-1][1] // last marker = immediate source
		}
	}
	return picks, nil
}

// WriteCherries writes the cherry-pick map to outDir/raw/cherries.csv.
func WriteCherries(outDir string, picks map[string]string) error {
	header := []string{"cherry_sha", "source_sha"}
	rows := make([][]string, 0, len(picks))
	for c, s := range picks {
		rows = append(rows, []string{c, s})
	}
	return csvw.Write(filepath.Join(outDir, "raw", "cherries.csv"), header, rows)
}

// WritePRs writes the classified PR table to outDir/raw/prs.csv.
func WritePRs(outDir string, prs []model.PR) error {
	header := []string{"pr_num", "state", "merge_method", "merge_sha", "base_ref", "head_ref", "url", "ci_state"}
	rows := make([][]string, 0, len(prs))
	for _, p := range prs {
		rows = append(rows, []string{p.Num, p.State, p.MergeMethod, p.MergeSHA, p.BaseRef, p.HeadRef, p.URL, p.CIState})
	}
	return csvw.Write(filepath.Join(outDir, "raw", "prs.csv"), header, rows)
}

// Run scans the bare repo and writes raw/*.csv under outDir.
func Run(bareDir, outDir, defaultBranch string) (Result, error) {
	commits, refs, edges, err := Scan(bareDir, defaultBranch)
	if err != nil {
		return Result{}, err
	}
	return WriteCSVs(outDir, commits, refs, edges)
}

// scanCommits walks the full graph once and builds commits + parent edges.
func scanCommits(bareDir string) ([]model.Commit, []model.Edge, error) {
	// fields: sha, parents, author, email, authored, committed, refs, subject
	format := strings.Join([]string{
		"%H", "%P", "%an", "%ae", "%aI", "%cI", "%D", "%s",
	}, us) + rs

	out, err := gitcmd.RunStream(bareDir, "log", "--all", "--no-abbrev",
		"--date=iso-strict", "--pretty=format:"+format)
	if err != nil {
		return nil, nil, err
	}

	var commits []model.Commit
	var edges []model.Edge

	records := strings.Split(string(out), rs)
	for _, rec := range records {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, us)
		if len(f) < 8 {
			continue
		}
		var parents []string
		if p := strings.TrimSpace(f[1]); p != "" {
			parents = strings.Fields(p)
		}
		subject := f[7]
		pr := parsePR(subject)
		c := model.Commit{
			SHA:         f[0],
			Parents:     parents,
			AuthorName:  f[2],
			AuthorEmail: f[3],
			AuthoredAt:  f[4],
			CommittedAt: f[5],
			Refs:        f[6],
			Subject:     subject,
			PRNum:       pr,
			IsMerge:     len(parents) >= 2,
		}
		commits = append(commits, c)

		for i, p := range parents {
			et := "commit"
			if c.IsMerge && i >= 1 {
				et = "merge" // inflow parent of a merge commit
			}
			edges = append(edges, model.Edge{
				Child: c.SHA, Parent: p, ParentIndex: i, Type: et,
			})
		}
	}
	return commits, edges, nil
}

// scanRefs enumerates branches and tags. Namespaces are queried separately so
// the ref type is unambiguous; annotated tags are peeled to their commit.
func scanRefs(bareDir, defaultBranch string) ([]model.Ref, int, int, error) {
	var refs []model.Ref

	// Branches: refs/heads/* point directly at commits.
	bOut, err := gitcmd.RunStream(bareDir, "for-each-ref",
		"--format=%(refname:short)"+us+"%(objectname)", "refs/heads/")
	if err != nil {
		return nil, 0, 0, err
	}
	branches := 0
	for _, line := range nonEmptyLines(bOut) {
		f := strings.Split(line, us)
		if len(f) < 2 {
			continue
		}
		refs = append(refs, model.Ref{
			Name: f[0], Type: "branch", TargetSHA: f[1],
			IsDefault: f[0] == defaultBranch,
		})
		branches++
	}

	// Tags: refs/tags/*. %(*objectname) is the peeled commit for annotated tags;
	// empty for lightweight tags, where %(objectname) already is the commit.
	tOut, err := gitcmd.RunStream(bareDir, "for-each-ref",
		"--format=%(refname:short)"+us+"%(objectname)"+us+"%(*objectname)", "refs/tags/")
	if err != nil {
		return nil, 0, 0, err
	}
	tags := 0
	for _, line := range nonEmptyLines(tOut) {
		f := strings.Split(line, us)
		if len(f) < 2 {
			continue
		}
		target := f[1]
		if len(f) >= 3 && f[2] != "" {
			target = f[2] // annotated tag -> peeled commit
		}
		refs = append(refs, model.Ref{Name: f[0], Type: "tag", TargetSHA: target})
		tags++
	}

	return refs, branches, tags, nil
}

func nonEmptyLines(b []byte) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func writeCommits(path string, commits []model.Commit) error {
	header := []string{
		"sha", "parents", "author_name", "author_email",
		"authored_at", "committed_at", "refs", "subject", "pr_num", "is_merge",
	}
	rows := make([][]string, 0, len(commits))
	for _, c := range commits {
		rows = append(rows, []string{
			c.SHA, strings.Join(c.Parents, " "), c.AuthorName, c.AuthorEmail,
			c.AuthoredAt, c.CommittedAt, c.Refs, c.Subject, c.PRNum, boolStr(c.IsMerge),
		})
	}
	return csvw.Write(path, header, rows)
}

func writeEdges(path string, edges []model.Edge) error {
	header := []string{"child_sha", "parent_sha", "parent_index", "edge_type"}
	rows := make([][]string, 0, len(edges))
	for _, e := range edges {
		rows = append(rows, []string{e.Child, e.Parent, strconv.Itoa(e.ParentIndex), e.Type})
	}
	return csvw.Write(path, header, rows)
}

func writeRefs(path string, refs []model.Ref) error {
	header := []string{"ref_name", "ref_type", "target_sha", "is_default"}
	rows := make([][]string, 0, len(refs))
	for _, r := range refs {
		rows = append(rows, []string{r.Name, r.Type, r.TargetSHA, boolStr(r.IsDefault)})
	}
	return csvw.Write(path, header, rows)
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
