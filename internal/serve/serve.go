// Package serve is the gbg HTTP backend: it lists run folders, serves each
// run's graph.json, answers server-side SQLite queries (so the browser never
// loads the large graph.sqlite), and hosts the built Svelte SPA.
package serve

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// Server holds the paths it serves from.
type Server struct {
	DataDir string // root containing run folders + .repos
	WebDir  string // built SPA on disk (web/dist); used if WebFS is nil
	WebFS   fs.FS  // embedded SPA; takes precedence over WebDir
}

// Handler builds the HTTP routes (Go 1.22+ pattern mux).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/runs", s.handleRuns)
	mux.HandleFunc("GET /api/runs/{id}/graph.json", s.handleGraph)
	mux.HandleFunc("GET /api/runs/{id}/containment", s.handleContainment)
	mux.HandleFunc("GET /api/runs/{id}/prs", s.handlePRs)
	mux.HandleFunc("GET /api/runs/{id}/diff", s.handleDiff)

	switch {
	case s.WebFS != nil:
		mux.Handle("/", spaFromFS(s.WebFS))
	case s.WebDir != "":
		mux.Handle("/", spaFileServer(s.WebDir))
	}
	return withCORS(withGzip(mux))
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

// openRO opens the run's SQLite read-only.
func (s *Server) openRO(dir string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+filepath.Join(dir, "graph.sqlite")+"?mode=ro")
}

func clampLimit(q string) int {
	n, err := strconv.Atoi(q)
	if err != nil || n <= 0 {
		return 200
	}
	if n > 2000 {
		return 2000
	}
	return n
}

// handlePRs lists PRs, optionally filtered by merge method / state.
func (s *Server) handlePRs(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.runDir(w, r)
	if !ok {
		return
	}
	method := r.URL.Query().Get("method")
	state := r.URL.Query().Get("state")
	limit := clampLimit(r.URL.Query().Get("limit"))

	dbc, err := s.openRO(dir)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	defer dbc.Close()
	rows, err := dbc.Query(`SELECT pr_num, state, merge_method, base_ref, head_ref, url
		FROM prs
		WHERE (?1='' OR merge_method=?1) AND (?2='' OR state=?2)
		ORDER BY pr_num DESC LIMIT ?3`, method, state, limit)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	type pr struct {
		Num         int    `json:"num"`
		State       string `json:"state"`
		MergeMethod string `json:"mergeMethod"`
		BaseRef     string `json:"baseRef"`
		HeadRef     string `json:"headRef"`
		URL         string `json:"url"`
	}
	out := []pr{}
	for rows.Next() {
		var p pr
		var num sql.NullInt64
		var st, mm, br, hr, u sql.NullString
		if err := rows.Scan(&num, &st, &mm, &br, &hr, &u); err != nil {
			httpErr(w, http.StatusInternalServerError, err)
			return
		}
		p.Num = int(num.Int64)
		p.State, p.MergeMethod, p.BaseRef, p.HeadRef, p.URL = st.String, mm.String, br.String, hr.String, u.String
		out = append(out, p)
	}
	writeJSON(w, out)
}

// handleDiff returns commits contained in `in` but not in `notin` — e.g. commits
// on dev not yet on a release branch.
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.runDir(w, r)
	if !ok {
		return
	}
	in := r.URL.Query().Get("in")
	notin := r.URL.Query().Get("notin")
	if in == "" || notin == "" {
		httpErr(w, http.StatusBadRequest, fmt.Errorf("in and notin required"))
		return
	}
	limit := clampLimit(r.URL.Query().Get("limit"))

	dbc, err := s.openRO(dir)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	defer dbc.Close()
	rows, err := dbc.Query(`SELECT c.sha, c.subject, c.pr_num, c.committed_at
		FROM commits c
		WHERE EXISTS (SELECT 1 FROM containment WHERE sha=c.sha AND ref_name=?1)
		  AND NOT EXISTS (SELECT 1 FROM containment WHERE sha=c.sha AND ref_name=?2)
		ORDER BY c.committed_at DESC LIMIT ?3`, in, notin, limit)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	type row struct {
		SHA         string `json:"sha"`
		Subject     string `json:"subject"`
		PRNum       string `json:"prNum"`
		CommittedAt string `json:"committedAt"`
	}
	out := []row{}
	for rows.Next() {
		var rw row
		var pr sql.NullInt64
		if err := rows.Scan(&rw.SHA, &rw.Subject, &pr, &rw.CommittedAt); err != nil {
			httpErr(w, http.StatusInternalServerError, err)
			return
		}
		if pr.Valid {
			rw.PRNum = strconv.FormatInt(pr.Int64, 10)
		}
		out = append(out, rw)
	}
	writeJSON(w, map[string]any{"in": in, "notin": notin, "count": len(out), "commits": out})
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

// spaFromFS serves the embedded SPA, falling back to index.html for unknown
// (client-side) routes.
func spaFromFS(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(fsys, p); err != nil {
			http.ServeFileFS(w, r, fsys, "index.html")
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		h.ServeHTTP(w, r)
	})
}

// withGzip compresses text responses (graph.json is ~11MB raw, ~1.4MB gzipped).
func withGzip(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h.ServeHTTP(w, r)
			return
		}
		// Range + gzip don't mix; drop it so ServeFile returns the full body.
		r.Header.Del("Range")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		h.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

// WriteHeader strips the pre-compression Content-Length (set by ServeFile)
// before the status line is committed.
func (g *gzipResponseWriter) WriteHeader(code int) {
	g.Header().Del("Content-Length")
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	return g.gz.Write(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, code int, err error) {
	http.Error(w, err.Error(), code)
}
