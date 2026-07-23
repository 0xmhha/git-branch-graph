// Package loader reads a run folder's raw/*.csv + meta.json back into the model,
// so the ontology stage can be re-run standalone (`gbg ontology <rundir>`)
// without re-cloning or re-scanning.
package loader

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wm-it-25/git-branch-graph/internal/model"
)

// Load reads snapshot + raw layer from a run directory.
func Load(runDir string) (model.Snapshot, []model.Commit, []model.Ref, []model.Edge, error) {
	snap, err := loadMeta(filepath.Join(runDir, "meta.json"))
	if err != nil {
		return model.Snapshot{}, nil, nil, nil, err
	}
	raw := filepath.Join(runDir, "raw")
	commits, err := loadCommits(filepath.Join(raw, "commits.csv"))
	if err != nil {
		return snap, nil, nil, nil, err
	}
	refs, err := loadRefs(filepath.Join(raw, "refs.csv"))
	if err != nil {
		return snap, nil, nil, nil, err
	}
	edges, err := loadEdges(filepath.Join(raw, "edges.csv"))
	if err != nil {
		return snap, nil, nil, nil, err
	}
	return snap, commits, refs, edges, nil
}

func loadMeta(path string) (model.Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return model.Snapshot{}, err
	}
	var m struct {
		RepoURL       string `json:"repo_url"`
		Org           string `json:"org"`
		Repo          string `json:"repo"`
		DefaultBranch string `json:"default_branch"`
		HeadSHA       string `json:"head_sha"`
		CapturedAt    string `json:"captured_at"`
		CommitCount   int    `json:"commit_count"`
		BranchCount   int    `json:"branch_count"`
		TagCount      int    `json:"tag_count"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return model.Snapshot{}, err
	}
	return model.Snapshot{
		Ref:           model.RepoRef{URL: m.RepoURL, Org: m.Org, Repo: m.Repo, Slug: m.Org + "__" + m.Repo},
		DefaultBranch: m.DefaultBranch, HeadSHA: m.HeadSHA, CapturedAt: m.CapturedAt,
		CommitCount: m.CommitCount, BranchCount: m.BranchCount, TagCount: m.TagCount,
	}, nil
}

func readCSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		rows = rows[1:] // drop header
	}
	return rows, nil
}

func loadCommits(path string) ([]model.Commit, error) {
	rows, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	out := make([]model.Commit, 0, len(rows))
	for _, r := range rows {
		if len(r) < 10 {
			continue
		}
		var parents []string
		if p := strings.TrimSpace(r[1]); p != "" {
			parents = strings.Fields(p)
		}
		out = append(out, model.Commit{
			SHA: r[0], Parents: parents, AuthorName: r[2], AuthorEmail: r[3],
			AuthoredAt: r[4], CommittedAt: r[5], Refs: r[6], Subject: r[7],
			PRNum: r[8], IsMerge: r[9] == "1",
		})
	}
	return out, nil
}

func loadRefs(path string) ([]model.Ref, error) {
	rows, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	out := make([]model.Ref, 0, len(rows))
	for _, r := range rows {
		if len(r) < 4 {
			continue
		}
		out = append(out, model.Ref{
			Name: r[0], Type: r[1], TargetSHA: r[2], IsDefault: r[3] == "1",
		})
	}
	return out, nil
}

// LoadCherries reads raw/cherries.csv (cherry_sha → source_sha); returns an
// empty map if the file is absent.
func LoadCherries(runDir string) map[string]string {
	rows, err := readCSV(filepath.Join(runDir, "raw", "cherries.csv"))
	if err != nil {
		return map[string]string{}
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		if len(r) >= 2 {
			m[r[0]] = r[1]
		}
	}
	return m
}

// LoadLocal reads raw/local.csv (local-vs-remote state); Known=false if the
// file is absent (remote analysis, or the source had no remote-tracking refs).
func LoadLocal(runDir string) model.LocalState {
	ls := model.LocalState{Unpushed: map[string]bool{}, RemoteBranches: map[string]bool{}}
	rows, err := readCSV(filepath.Join(runDir, "raw", "local.csv"))
	if err != nil {
		return ls
	}
	for _, r := range rows {
		if len(r) < 2 {
			continue
		}
		switch r[0] {
		case "unpushed":
			ls.Unpushed[r[1]] = true
		case "remote_branch":
			ls.RemoteBranches[r[1]] = true
		}
	}
	ls.Known = true
	return ls
}

func loadEdges(path string) ([]model.Edge, error) {
	rows, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	out := make([]model.Edge, 0, len(rows))
	for _, r := range rows {
		if len(r) < 4 {
			continue
		}
		pi, _ := strconv.Atoi(r[2])
		out = append(out, model.Edge{Child: r[0], Parent: r[1], ParentIndex: pi, Type: r[3]})
	}
	return out, nil
}
