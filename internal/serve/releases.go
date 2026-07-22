package serve

import (
	"database/sql"
	"net/http"
	"regexp"
	"sort"
)

// envSuffix splits a tag into a version family and an environment. A trailing
// "_<letters…>" segment is treated as an environment (e.g. w0.10.10_devnet →
// family w0.10.10, env "devnet"); a bare tag is the base "release".
var envSuffix = regexp.MustCompile(`^(.*?)_([A-Za-z][A-Za-z0-9_]*)$`)

const baseEnv = "release"

func splitVersion(tag string) (family, env string) {
	if m := envSuffix.FindStringSubmatch(tag); m != nil {
		return m[1], m[2]
	}
	return tag, baseEnv
}

type relCell struct {
	Tag string `json:"tag"`
	SHA string `json:"sha"`
}

type release struct {
	Family  string             `json:"family"`
	Date    string             `json:"date"`
	MainTag string             `json:"mainTag"` // representative tag for diffs
	Envs    map[string]relCell `json:"envs"`
}

// handleReleases groups release tags into version families × environments — the
// data behind the release matrix. All from existing refs/commits tables.
func (s *Server) handleReleases(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.runDir(w, r)
	if !ok {
		return
	}
	dbc, err := s.openRO(dir)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	defer dbc.Close()

	rows, err := dbc.Query(`SELECT r.ref_name, r.target_sha, COALESCE(c.committed_at,'')
		FROM refs r LEFT JOIN commits c ON c.sha = r.target_sha
		WHERE r.ref_type='tag'`)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	fams := map[string]*release{}
	envSet := map[string]bool{}
	for rows.Next() {
		var name, sha, date string
		if err := rows.Scan(&name, &sha, &date); err != nil {
			httpErr(w, http.StatusInternalServerError, err)
			return
		}
		family, env := splitVersion(name)
		envSet[env] = true
		f := fams[family]
		if f == nil {
			f = &release{Family: family, Envs: map[string]relCell{}}
			fams[family] = f
		}
		f.Envs[env] = relCell{Tag: name, SHA: sha}
		// Family date = newest tag date in the family; representative = base tag.
		if date > f.Date {
			f.Date = date
		}
		if env == baseEnv || f.MainTag == "" {
			f.MainTag = name
		}
	}

	// Environment columns: base first, then the rest alphabetically.
	var envs []string
	for e := range envSet {
		if e != baseEnv {
			envs = append(envs, e)
		}
	}
	sort.Strings(envs)
	envs = append([]string{baseEnv}, envs...)

	// Releases newest first.
	releases := make([]*release, 0, len(fams))
	for _, f := range fams {
		releases = append(releases, f)
	}
	sort.Slice(releases, func(i, j int) bool {
		if releases[i].Date != releases[j].Date {
			return releases[i].Date > releases[j].Date
		}
		return releases[i].Family > releases[j].Family
	})

	writeJSON(w, map[string]any{"environments": envs, "releases": releases})
}

// resolvePRSHA returns the merge commit SHA for a PR number.
func resolvePRSHA(dbc *sql.DB, pr string) (string, bool) {
	var sha sql.NullString
	if err := dbc.QueryRow(`SELECT merge_sha FROM prs WHERE pr_num=?`, pr).Scan(&sha); err != nil {
		return "", false
	}
	return sha.String, sha.Valid && sha.String != ""
}
