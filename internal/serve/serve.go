// Package serve is the gbg HTTP backend: it lists run folders, serves each
// run's graph.json, answers server-side SQLite queries (so the browser never
// loads the large graph.sqlite), and hosts the built Svelte SPA.
package serve

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Server holds the paths it serves from.
type Server struct {
	DataDir string // root containing run folders + .repos
	WebDir  string // built SPA (web/dist); may be empty
}

// Handler builds the HTTP routes (Go 1.22+ pattern mux).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/runs", s.handleRuns)
	mux.HandleFunc("GET /api/runs/{id}/graph.json", s.handleGraph)
	mux.HandleFunc("GET /api/runs/{id}/containment", s.handleContainment)

	if s.WebDir != "" {
		mux.Handle("/", spaFileServer(s.WebDir))
	}
	return withCORS(mux)
}

type runSummary struct {
	ID            string `json:"id"`
	Org           string `json:"org"`
	Repo          string `json:"repo"`
	DefaultBranch string `json:"defaultBranch"`
	HeadSHA       string `json:"headSha"`
	CapturedAt    string `json:"capturedAt"`
	Commits       int    `json:"commits"`
	Branches      int    `json:"branches"`
	Tags          int    `json:"tags"`
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.DataDir)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	var out []runSummary
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue // skip .repos and hidden
		}
		metaPath := filepath.Join(s.DataDir, e.Name(), "meta.json")
		b, err := os.ReadFile(metaPath)
		if err != nil {
			continue // not a run folder
		}
		var m struct {
			Org           string `json:"org"`
			Repo          string `json:"repo"`
			DefaultBranch string `json:"default_branch"`
			HeadSHA       string `json:"head_sha"`
			CapturedAt    string `json:"captured_at"`
			CommitCount   int    `json:"commit_count"`
			BranchCount   int    `json:"branch_count"`
			TagCount      int    `json:"tag_count"`
		}
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		out = append(out, runSummary{
			ID: e.Name(), Org: m.Org, Repo: m.Repo, DefaultBranch: m.DefaultBranch,
			HeadSHA: m.HeadSHA, CapturedAt: m.CapturedAt,
			Commits: m.CommitCount, Branches: m.BranchCount, Tags: m.TagCount,
		})
	}
	writeJSON(w, out)
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.runDir(w, r)
	if !ok {
		return
	}
	http.ServeFile(w, r, filepath.Join(dir, "graph.json"))
}

func (s *Server) handleContainment(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.runDir(w, r)
	if !ok {
		return
	}
	sha := r.URL.Query().Get("sha")
	if sha == "" {
		httpErr(w, http.StatusBadRequest, fmt.Errorf("missing sha"))
		return
	}
	// Read-only open of the run's SQLite — server-side query, small JSON back.
	dbc, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "graph.sqlite")+"?mode=ro")
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	defer dbc.Close()
	rows, err := dbc.Query(
		`SELECT ref_name, ref_type FROM containment WHERE sha=? ORDER BY ref_type, ref_name`, sha)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	type ref struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	var tags, branches []ref
	for rows.Next() {
		var rf ref
		if err := rows.Scan(&rf.Name, &rf.Type); err != nil {
			httpErr(w, http.StatusInternalServerError, err)
			return
		}
		if rf.Type == "tag" {
			tags = append(tags, rf)
		} else {
			branches = append(branches, rf)
		}
	}
	writeJSON(w, map[string]any{"sha": sha, "branches": branches, "tags": tags})
}

// runDir resolves and validates the {id} path segment to a run folder.
func (s *Server) runDir(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	// Reject traversal: id must be a single clean path element.
	if id == "" || id != filepath.Base(id) || strings.Contains(id, "..") {
		httpErr(w, http.StatusBadRequest, fmt.Errorf("invalid run id"))
		return "", false
	}
	dir := filepath.Join(s.DataDir, id)
	if fi, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil || fi.IsDir() {
		httpErr(w, http.StatusNotFound, fmt.Errorf("run not found"))
		return "", false
	}
	return dir, true
}

// spaFileServer serves static files, falling back to index.html for client-side
// routes (SPA behavior).
func spaFileServer(webDir string) http.Handler {
	fs := http.FileServer(http.Dir(webDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(webDir, filepath.Clean(r.URL.Path))
		if fi, err := os.Stat(p); err != nil || fi.IsDir() {
			if r.URL.Path != "/" {
				http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
				return
			}
		}
		fs.ServeHTTP(w, r)
	})
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, code int, err error) {
	http.Error(w, err.Error(), code)
}
