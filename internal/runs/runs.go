// Package runs manages the run folders under the data directory (list / remove).
package runs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Info summarizes one run folder.
type Info struct {
	ID         string
	Org        string
	Repo       string
	Branch     string
	Commits    int
	CapturedAt string
	Size       int64
}

type metaFile struct {
	Org           string `json:"org"`
	Repo          string `json:"repo"`
	DefaultBranch string `json:"default_branch"`
	CapturedAt    string `json:"captured_at"`
	CommitCount   int    `json:"commit_count"`
}

// List returns every run folder under dataDir, newest capture first.
func List(dataDir string) ([]Info, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(dataDir, e.Name())
		b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
		if err != nil {
			continue
		}
		var m metaFile
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		out = append(out, Info{
			ID: e.Name(), Org: m.Org, Repo: m.Repo, Branch: m.DefaultBranch,
			Commits: m.CommitCount, CapturedAt: m.CapturedAt, Size: dirSize(dir),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CapturedAt > out[j].CapturedAt })
	return out, nil
}

// Remove deletes a run folder by id (validated to be a direct child of dataDir).
func Remove(dataDir, id string) error {
	if id == "" || id != filepath.Base(id) || strings.HasPrefix(id, ".") {
		return fmt.Errorf("invalid run id %q", id)
	}
	dir := filepath.Join(dataDir, id)
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		return fmt.Errorf("no such run: %s", id)
	}
	return os.RemoveAll(dir)
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.Walk(dir, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			n += fi.Size()
		}
		return nil
	})
	return n
}
