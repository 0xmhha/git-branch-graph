package enrich

import (
	"encoding/json"
	"os"
	"strconv"

	"github.com/wm-it-25/git-branch-graph/internal/model"
)

// cache persists enrich results per repository so re-analysis only queries PR
// numbers it has never seen. Both resolved PRs (Found) and numbers confirmed not
// to exist in this repo (Absent — the bulk of the cost on forks) are recorded.
type cache struct {
	Found  map[string]model.PR `json:"found"`
	Absent map[string]bool     `json:"absent"`
}

func loadCache(path string) *cache {
	c := &cache{Found: map[string]model.PR{}, Absent: map[string]bool{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, c)
	if c.Found == nil {
		c.Found = map[string]model.PR{}
	}
	if c.Absent == nil {
		c.Absent = map[string]bool{}
	}
	return c
}

func (c *cache) save(path string) {
	b, err := json.Marshal(c)
	if err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
}

// FetchCached is Fetch backed by a per-repo cache at cachePath. Only PR numbers
// absent from the cache are queried; refresh=true ignores and rebuilds it.
func FetchCached(owner, repo, token string, prNums []string, cachePath string, refresh bool) (map[string]model.PR, error) {
	c := &cache{Found: map[string]model.PR{}, Absent: map[string]bool{}}
	if !refresh {
		c = loadCache(cachePath)
	}

	// Which requested numbers are still unknown?
	var toFetch []string
	seen := map[string]bool{}
	for _, n := range prNums {
		if seen[n] {
			continue
		}
		seen[n] = true
		if _, ok := c.Found[n]; ok {
			continue
		}
		if c.Absent[n] {
			continue
		}
		toFetch = append(toFetch, n)
	}

	if len(toFetch) > 0 {
		fetched, err := Fetch(owner, repo, token, toFetch)
		if err != nil {
			return nil, err
		}
		for _, n := range toFetch {
			if pr, ok := fetched[n]; ok {
				c.Found[n] = pr
			} else {
				c.Absent[n] = true // confirmed not a PR in this repo
			}
		}
		c.save(cachePath)
	}

	// Assemble the result for the requested numbers.
	out := make(map[string]model.PR, len(prNums))
	for n := range seen {
		if pr, ok := c.Found[n]; ok {
			out[n] = pr
		}
	}
	return out, nil
}

// PendingCount reports how many of prNums are not yet cached (for progress msgs).
func PendingCount(prNums []string, cachePath string, refresh bool) int {
	if refresh {
		return len(uniqueNums(prNums))
	}
	c := loadCache(cachePath)
	n := 0
	for _, s := range uniqueNums(prNums) {
		if _, ok := c.Found[s]; ok {
			continue
		}
		if c.Absent[s] {
			continue
		}
		n++
	}
	return n
}

func uniqueNums(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if _, err := strconv.Atoi(s); err != nil || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
