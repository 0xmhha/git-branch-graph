// Package query holds read-only queries over a run's graph.sqlite, returning Go
// structs. Shared by the HTTP server and the MCP server so both expose the same
// answers ("where is this fix", "what's in this release", …).
package query

import (
	"database/sql"
	"path/filepath"
	"regexp"
	"sort"

	_ "modernc.org/sqlite"
)

type Ref struct {
	Name string `json:"name" jsonschema:"branch or tag name"`
	Type string `json:"type" jsonschema:"either 'branch' or 'tag'"`
}

type Commit struct {
	SHA         string `json:"sha" jsonschema:"full commit SHA"`
	Subject     string `json:"subject" jsonschema:"commit subject line"`
	PRNum       string `json:"prNum,omitempty" jsonschema:"PR number if the commit references one"`
	CommittedAt string `json:"committedAt" jsonschema:"commit timestamp (ISO8601)"`
}

type PR struct {
	Num         int    `json:"num" jsonschema:"PR number"`
	State       string `json:"state,omitempty" jsonschema:"merged | open | closed (empty if unknown)"`
	MergeMethod string `json:"mergeMethod,omitempty" jsonschema:"squash | merge | rebase — how it landed"`
	BaseRef     string `json:"baseRef,omitempty" jsonschema:"branch it merged into"`
	HeadRef     string `json:"headRef,omitempty" jsonschema:"source branch"`
	URL         string `json:"url,omitempty" jsonschema:"GitHub PR URL"`
}

type RelCell struct {
	Tag string `json:"tag" jsonschema:"the concrete tag for this environment"`
	SHA string `json:"sha" jsonschema:"commit the tag points at"`
}

type Release struct {
	Family  string             `json:"family" jsonschema:"version family (tag without an environment suffix)"`
	Date    string             `json:"date" jsonschema:"newest tag date in the family"`
	MainTag string             `json:"mainTag" jsonschema:"representative tag for the family"`
	Envs    map[string]RelCell `json:"envs" jsonschema:"environment name -> the tag that reached it"`
}

// Open opens a run's graph.sqlite read-only.
func Open(runDir string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+filepath.Join(runDir, "graph.sqlite")+"?mode=ro")
}

// ResolvePR maps a PR number to its merge commit SHA.
func ResolvePR(db *sql.DB, pr string) (string, bool) {
	var sha sql.NullString
	if err := db.QueryRow(`SELECT merge_sha FROM prs WHERE pr_num=?`, pr).Scan(&sha); err != nil {
		return "", false
	}
	return sha.String, sha.Valid && sha.String != ""
}

// Containment returns the branches and tags that contain a commit.
func Containment(db *sql.DB, sha string) (branches, tags []Ref, err error) {
	rows, err := db.Query(`SELECT r.ref_name, r.ref_type
		FROM containment ct JOIN commits c ON c.id=ct.commit_id JOIN refs r ON r.id=ct.ref_id
		WHERE c.sha=? ORDER BY r.ref_type, r.ref_name`, sha)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rf Ref
		if err := rows.Scan(&rf.Name, &rf.Type); err != nil {
			return nil, nil, err
		}
		if rf.Type == "tag" {
			tags = append(tags, rf)
		} else {
			branches = append(branches, rf)
		}
	}
	return branches, tags, rows.Err()
}

// Diff returns commits contained in `in` but not in `notin`.
func Diff(db *sql.DB, in, notin string, limit int) ([]Commit, error) {
	rows, err := db.Query(`SELECT c.sha, c.subject, c.pr_num, c.committed_at FROM commits c
		WHERE EXISTS (SELECT 1 FROM containment ct JOIN refs r ON r.id=ct.ref_id WHERE ct.commit_id=c.id AND r.ref_name=?1)
		  AND NOT EXISTS (SELECT 1 FROM containment ct JOIN refs r ON r.id=ct.ref_id WHERE ct.commit_id=c.id AND r.ref_name=?2)
		ORDER BY c.committed_at DESC LIMIT ?3`, in, notin, clamp(limit, 500))
	if err != nil {
		return nil, err
	}
	return scanCommits(rows)
}

// Search finds commits by subject, author, or PR number.
func Search(db *sql.DB, q string, limit int) ([]Commit, error) {
	like := "%" + q + "%"
	rows, err := db.Query(`SELECT sha, subject, pr_num, committed_at FROM commits
		WHERE subject LIKE ?1 OR author_name LIKE ?1 OR CAST(pr_num AS TEXT)=?2
		ORDER BY committed_at DESC LIMIT ?3`, like, q, clamp(limit, 100))
	if err != nil {
		return nil, err
	}
	return scanCommits(rows)
}

// PRs lists PRs, optionally filtered by merge method / state.
func PRs(db *sql.DB, method, state string, limit int) ([]PR, error) {
	rows, err := db.Query(`SELECT pr_num, state, merge_method, base_ref, head_ref, url FROM prs
		WHERE (?1='' OR merge_method=?1) AND (?2='' OR state=?2)
		ORDER BY pr_num DESC LIMIT ?3`, method, state, clamp(limit, 200))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PR{}
	for rows.Next() {
		var p PR
		var num sql.NullInt64
		var st, mm, br, hr, u sql.NullString
		if err := rows.Scan(&num, &st, &mm, &br, &hr, &u); err != nil {
			return nil, err
		}
		p.Num = int(num.Int64)
		p.State, p.MergeMethod, p.BaseRef, p.HeadRef, p.URL = st.String, mm.String, br.String, hr.String, u.String
		out = append(out, p)
	}
	return out, rows.Err()
}

var envSuffix = regexp.MustCompile(`^(.*?)_([A-Za-z][A-Za-z0-9_]*)$`)

func splitVersion(tag string) (family, env string) {
	if m := envSuffix.FindStringSubmatch(tag); m != nil {
		return m[1], m[2]
	}
	return tag, "release"
}

// Releases groups tags into version families × environments.
func Releases(db *sql.DB) (environments []string, releases []Release, err error) {
	rows, err := db.Query(`SELECT r.ref_name, r.target_sha, COALESCE(c.committed_at,'')
		FROM refs r LEFT JOIN commits c ON c.sha=r.target_sha WHERE r.ref_type='tag'`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	fams := map[string]*Release{}
	envSet := map[string]bool{}
	for rows.Next() {
		var name, sha, date string
		if err := rows.Scan(&name, &sha, &date); err != nil {
			return nil, nil, err
		}
		family, env := splitVersion(name)
		envSet[env] = true
		f := fams[family]
		if f == nil {
			f = &Release{Family: family, Envs: map[string]RelCell{}}
			fams[family] = f
		}
		f.Envs[env] = RelCell{Tag: name, SHA: sha}
		if date > f.Date {
			f.Date = date
		}
		if env == "release" || f.MainTag == "" {
			f.MainTag = name
		}
	}
	for e := range envSet {
		if e != "release" {
			environments = append(environments, e)
		}
	}
	sort.Strings(environments)
	environments = append([]string{"release"}, environments...)
	for _, f := range fams {
		releases = append(releases, *f)
	}
	sort.Slice(releases, func(i, j int) bool {
		if releases[i].Date != releases[j].Date {
			return releases[i].Date > releases[j].Date
		}
		return releases[i].Family > releases[j].Family
	})
	return environments, releases, rows.Err()
}

func scanCommits(rows *sql.Rows) ([]Commit, error) {
	defer rows.Close()
	out := []Commit{}
	for rows.Next() {
		var c Commit
		var pr sql.NullInt64
		if err := rows.Scan(&c.SHA, &c.Subject, &pr, &c.CommittedAt); err != nil {
			return nil, err
		}
		if pr.Valid {
			c.PRNum = itoa(pr.Int64)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func clamp(n, max int) int {
	if n <= 0 || n > max {
		return max
	}
	return n
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
