// Package paths derives filesystem-safe identifiers and run folder names.
package paths

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wm-it-25/git-branch-graph/internal/model"
)

var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// sanitize replaces path-unsafe runs (including '/') with '-'.
func sanitize(s string) string {
	return strings.Trim(unsafe.ReplaceAllString(s, "-"), "-")
}

// ParseRepoRef derives org/repo/slug from a remote URL or local path.
//
//	https://github.com/wemix/go-wemix(.git) -> org=wemix repo=go-wemix
//	git@github.com:wemix/go-wemix.git        -> org=wemix repo=go-wemix
//	/abs/path/to/go-wemix                     -> org=<parent dir> repo=go-wemix
func ParseRepoRef(input string) model.RepoRef {
	url := strings.TrimSpace(input)
	trimmed := strings.TrimSuffix(url, ".git")

	var org, repo string
	switch {
	case strings.Contains(trimmed, "://"):
		// scheme://host/org/repo
		parts := strings.Split(strings.TrimRight(trimmed, "/"), "/")
		if len(parts) >= 2 {
			org, repo = parts[len(parts)-2], parts[len(parts)-1]
		} else if len(parts) == 1 {
			repo = parts[0]
		}
	case strings.Contains(trimmed, "@") && strings.Contains(trimmed, ":"):
		// git@host:org/repo
		after := trimmed[strings.LastIndex(trimmed, ":")+1:]
		parts := strings.Split(strings.Trim(after, "/"), "/")
		if len(parts) >= 2 {
			org, repo = parts[len(parts)-2], parts[len(parts)-1]
		} else if len(parts) == 1 {
			repo = parts[0]
		}
	default:
		// local path
		clean := filepath.Clean(trimmed)
		repo = filepath.Base(clean)
		org = filepath.Base(filepath.Dir(clean))
	}

	org = sanitize(org)
	repo = sanitize(repo)
	if org == "" {
		org = "local"
	}
	return model.RepoRef{
		URL:  url,
		Org:  org,
		Repo: repo,
		Slug: org + "__" + repo,
	}
}

// BareRepoDir is the cached bare mirror location under the data dir.
func BareRepoDir(dataDir string, ref model.RepoRef) string {
	return filepath.Join(dataDir, ".repos", ref.Slug+".git")
}

// RunDir is the content-addressed output folder for one snapshot:
// data/<org>__<repo>__<branch>__<sha7>/
func RunDir(dataDir string, ref model.RepoRef, branch, headSHA string) string {
	sha7 := headSHA
	if len(sha7) > 7 {
		sha7 = sha7[:7]
	}
	name := ref.Slug + "__" + sanitize(branch) + "__" + sha7
	return filepath.Join(dataDir, name)
}
