// Package db writes the graph to a single-file SQLite database — the queryable,
// first-class output alongside graph.json. Uses the pure-Go modernc driver (no
// CGO), keeping gbg a static binary.
package db

import (
	"database/sql"
	"os"

	_ "modernc.org/sqlite"

	"github.com/wm-it-25/git-branch-graph/internal/model"
)

const schema = `
CREATE TABLE commits (
  sha TEXT PRIMARY KEY, author_name TEXT, author_email TEXT,
  authored_at TEXT, committed_at TEXT, subject TEXT, pr_num INTEGER,
  is_merge INTEGER NOT NULL DEFAULT 0, lane INTEGER, color TEXT, branch_of TEXT
);
CREATE TABLE edges (
  child_sha TEXT NOT NULL, parent_sha TEXT NOT NULL, parent_index INTEGER NOT NULL,
  edge_type TEXT NOT NULL, PRIMARY KEY (child_sha, parent_sha)
);
CREATE TABLE refs (
  ref_name TEXT PRIMARY KEY, ref_type TEXT NOT NULL, target_sha TEXT NOT NULL,
  is_default INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE prs (
  pr_num INTEGER PRIMARY KEY, state TEXT, merge_method TEXT, merge_sha TEXT,
  base_ref TEXT, head_ref TEXT, url TEXT
);
CREATE TABLE checks (
  sha TEXT NOT NULL, context TEXT NOT NULL, state TEXT, url TEXT,
  PRIMARY KEY (sha, context)
);
CREATE TABLE containment (
  sha TEXT NOT NULL, ref_name TEXT NOT NULL, ref_type TEXT NOT NULL,
  PRIMARY KEY (sha, ref_name)
);
CREATE TABLE meta (
  repo_url TEXT, org TEXT, repo TEXT, default_branch TEXT, head_sha TEXT,
  captured_at TEXT, commit_count INTEGER, branch_count INTEGER, tag_count INTEGER
);
`

const indexes = `
CREATE INDEX idx_edges_parent ON edges(parent_sha);
CREATE INDEX idx_edges_child  ON edges(child_sha);
CREATE INDEX idx_commits_pr   ON commits(pr_num);
CREATE INDEX idx_commits_time ON commits(committed_at);
CREATE INDEX idx_contain_ref  ON containment(ref_name);
CREATE INDEX idx_contain_sha  ON containment(sha);
`

// Write creates path (overwriting) and populates all tables from the graph.
// Raw refs/edges are passed alongside the computed graph so the SQL layer holds
// the same facts as the CSV layer plus the ontology columns.
func Write(path string, g model.Graph, refs []model.Ref, edges []model.Edge) (containRows int, err error) {
	_ = os.Remove(path)
	dbc, err := sql.Open("sqlite", path)
	if err != nil {
		return 0, err
	}
	defer dbc.Close()

	// Bulk-load tuning; durability is irrelevant for a regenerated artifact.
	for _, p := range []string{"PRAGMA journal_mode=OFF", "PRAGMA synchronous=OFF"} {
		if _, err := dbc.Exec(p); err != nil {
			return 0, err
		}
	}
	if _, err := dbc.Exec(schema); err != nil {
		return 0, err
	}

	tx, err := dbc.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// commits
	cst, err := tx.Prepare(`INSERT INTO commits
		(sha,author_name,author_email,authored_at,committed_at,subject,pr_num,is_merge,lane,color,branch_of)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	for _, n := range g.Nodes {
		if _, err = cst.Exec(n.SHA, n.Author, "", "", n.CommittedAt, n.Subject,
			nullInt(n.PRNum), b2i(n.IsMerge), n.Lane, n.Color, nullStr(n.BranchOf)); err != nil {
			return 0, err
		}
	}
	cst.Close()

	// edges
	est, err := tx.Prepare(`INSERT OR IGNORE INTO edges
		(child_sha,parent_sha,parent_index,edge_type) VALUES (?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	for _, e := range edges {
		if _, err = est.Exec(e.Child, e.Parent, e.ParentIndex, e.Type); err != nil {
			return 0, err
		}
	}
	est.Close()

	// refs
	rst, err := tx.Prepare(`INSERT INTO refs (ref_name,ref_type,target_sha,is_default) VALUES (?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	for _, r := range refs {
		if _, err = rst.Exec(r.Name, r.Type, r.TargetSHA, b2i(r.IsDefault)); err != nil {
			return 0, err
		}
	}
	rst.Close()

	// containment (the large table)
	tst, err := tx.Prepare(`INSERT OR IGNORE INTO containment (sha,ref_name,ref_type) VALUES (?,?,?)`)
	if err != nil {
		return 0, err
	}
	for sha, list := range g.Containment {
		for _, cr := range list {
			if _, err = tst.Exec(sha, cr.Name, cr.Type); err != nil {
				return 0, err
			}
			containRows++
		}
	}
	tst.Close()

	// meta (single row)
	if _, err = tx.Exec(`INSERT INTO meta
		(repo_url,org,repo,default_branch,head_sha,captured_at,commit_count,branch_count,tag_count)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		g.Meta.Ref.URL, g.Meta.Ref.Org, g.Meta.Ref.Repo, g.Meta.DefaultBranch,
		g.Meta.HeadSHA, g.Meta.CapturedAt, g.Meta.CommitCount, g.Meta.BranchCount,
		g.Meta.TagCount); err != nil {
		return 0, err
	}

	if err = tx.Commit(); err != nil {
		return 0, err
	}
	if _, err = dbc.Exec(indexes); err != nil {
		return 0, err
	}
	return containRows, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(s string) any {
	if s == "" {
		return nil
	}
	return s // sqlite coerces numeric text into the INTEGER column
}
