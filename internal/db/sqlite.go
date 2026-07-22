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

// commits and refs carry an integer id so the large containment table can store
// two ints per row instead of a repeated 40-char SHA + ref name — the file drops
// from hundreds of MB to tens.
const schema = `
CREATE TABLE commits (
  id INTEGER PRIMARY KEY, sha TEXT UNIQUE NOT NULL, author_name TEXT, author_email TEXT,
  authored_at TEXT, committed_at TEXT, subject TEXT, pr_num INTEGER,
  is_merge INTEGER NOT NULL DEFAULT 0, lane INTEGER, color TEXT, branch_of TEXT
);
CREATE TABLE edges (
  child_sha TEXT NOT NULL, parent_sha TEXT NOT NULL, parent_index INTEGER NOT NULL,
  edge_type TEXT NOT NULL, PRIMARY KEY (child_sha, parent_sha)
);
CREATE TABLE refs (
  id INTEGER PRIMARY KEY, ref_name TEXT UNIQUE NOT NULL, ref_type TEXT NOT NULL,
  target_sha TEXT NOT NULL, is_default INTEGER NOT NULL DEFAULT 0
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
  commit_id INTEGER NOT NULL, ref_id INTEGER NOT NULL,
  PRIMARY KEY (commit_id, ref_id)
) WITHOUT ROWID;
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
CREATE INDEX idx_contain_ref  ON containment(ref_id);
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

	// commits (integer id = position in g.Nodes + 1)
	commitID := make(map[string]int, len(g.Nodes))
	cst, err := tx.Prepare(`INSERT INTO commits
		(id,sha,author_name,author_email,authored_at,committed_at,subject,pr_num,is_merge,lane,color,branch_of)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	for i, n := range g.Nodes {
		id := i + 1
		commitID[n.SHA] = id
		if _, err = cst.Exec(id, n.SHA, n.Author, "", "", n.CommittedAt, n.Subject,
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

	// refs (integer id)
	refID := make(map[string]int, len(refs))
	rst, err := tx.Prepare(`INSERT INTO refs (id,ref_name,ref_type,target_sha,is_default) VALUES (?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	for i, r := range refs {
		id := i + 1
		refID[r.Name] = id
		if _, err = rst.Exec(id, r.Name, r.Type, r.TargetSHA, b2i(r.IsDefault)); err != nil {
			return 0, err
		}
	}
	rst.Close()

	// prs (offline-classified + optional enrich)
	pst, err := tx.Prepare(`INSERT OR REPLACE INTO prs
		(pr_num,state,merge_method,merge_sha,base_ref,head_ref,url) VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	for _, p := range g.PRs {
		if _, err = pst.Exec(nullInt(p.Num), nullStr(p.State), nullStr(p.MergeMethod),
			nullStr(p.MergeSHA), nullStr(p.BaseRef), nullStr(p.HeadRef), nullStr(p.URL)); err != nil {
			return 0, err
		}
	}
	pst.Close()

	// checks (PR CI rollup, when enrich supplied it)
	kst, err := tx.Prepare(`INSERT OR IGNORE INTO checks (sha,context,state,url) VALUES (?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	for _, p := range g.PRs {
		if p.CIState != "" && p.MergeSHA != "" {
			if _, err = kst.Exec(p.MergeSHA, "rollup", p.CIState, p.URL); err != nil {
				return 0, err
			}
		}
	}
	kst.Close()

	// containment (the large table) — integer (commit_id, ref_id) pairs
	tst, err := tx.Prepare(`INSERT OR IGNORE INTO containment (commit_id,ref_id) VALUES (?,?)`)
	if err != nil {
		return 0, err
	}
	for sha, list := range g.Containment {
		cid, ok := commitID[sha]
		if !ok {
			continue
		}
		for _, cr := range list {
			rid, ok := refID[cr.Name]
			if !ok {
				continue
			}
			if _, err = tst.Exec(cid, rid); err != nil {
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
