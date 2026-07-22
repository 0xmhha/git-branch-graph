// Package export writes a self-contained static site (embedded SPA + selected
// run data + a manifest) that renders and queries without a running server —
// the browser reads graph.json and queries graph.sqlite via sql.js.
package export

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/wm-it-25/git-branch-graph/internal/webui"
)

type runManifest struct {
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

type metaFile struct {
	Org           string `json:"org"`
	Repo          string `json:"repo"`
	DefaultBranch string `json:"default_branch"`
	HeadSHA       string `json:"head_sha"`
	CapturedAt    string `json:"captured_at"`
	CommitCount   int    `json:"commit_count"`
	BranchCount   int    `json:"branch_count"`
	TagCount      int    `json:"tag_count"`
}

// Export writes the static site to outDir. runFilter, when non-empty, exports a
// single run; otherwise every run in dataDir is included.
func Export(dataDir, outDir, runFilter string) (int, error) {
	webFS, ok := webui.FS()
	if !ok {
		return 0, fmt.Errorf("no embedded SPA — build the frontend first (`make binary`)")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return 0, err
	}
	if err := copyEmbedded(webFS, outDir); err != nil {
		return 0, fmt.Errorf("copy SPA: %w", err)
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return 0, err
	}
	var manifest []runManifest
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if runFilter != "" && e.Name() != runFilter {
			continue
		}
		metaPath := filepath.Join(dataDir, e.Name(), "meta.json")
		b, err := os.ReadFile(metaPath)
		if err != nil {
			continue // not a run folder
		}
		var m metaFile
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		manifest = append(manifest, runManifest{
			ID: e.Name(), Org: m.Org, Repo: m.Repo, DefaultBranch: m.DefaultBranch,
			HeadSHA: m.HeadSHA, CapturedAt: m.CapturedAt,
			Commits: m.CommitCount, Branches: m.BranchCount, Tags: m.TagCount,
		})
		dst := filepath.Join(outDir, "data", e.Name())
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return 0, err
		}
		for _, f := range []string{"graph.json", "graph.sqlite", "meta.json"} {
			if err := copyFile(filepath.Join(dataDir, e.Name(), f), filepath.Join(dst, f)); err != nil {
				return 0, fmt.Errorf("copy %s/%s: %w", e.Name(), f, err)
			}
		}
	}
	if len(manifest) == 0 {
		return 0, fmt.Errorf("no runs found in %s", dataDir)
	}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(outDir, "runs.json"), mb, 0o644); err != nil {
		return 0, err
	}
	return len(manifest), nil
}

func copyEmbedded(src fs.FS, outDir string) error {
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
