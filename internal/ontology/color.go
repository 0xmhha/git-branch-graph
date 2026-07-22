package ontology

import (
	"hash/fnv"
	"sort"

	"github.com/wm-it-25/git-branch-graph/internal/model"
)

const (
	defaultColor = "#39d353" // default branch: fixed primary (green)
	neutralColor = "#8b949e" // commits owned by no current branch line
)

// palette: colorblind-aware, distinct in light and dark. Default branch does not
// draw from this pool (it is pinned to defaultColor).
var palette = []string{
	"#58a6ff", // blue
	"#f778ba", // pink
	"#e3b341", // amber
	"#bc8cff", // purple
	"#ff7b72", // red
	"#39c5cf", // teal
	"#db6d28", // orange
	"#a5d6ff", // light blue
	"#7ee787", // light green
	"#ffa657", // tan
}

// branchColor returns a deterministic color for a branch name.
func branchColor(name, defaultBranch string) string {
	if name == "" {
		return neutralColor
	}
	if name == defaultBranch {
		return defaultColor
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return palette[int(h.Sum32())%len(palette)]
}

// assignBranchOf labels each commit with the branch whose first-parent history
// reaches it, default branch taking priority. Commits reachable only via merge
// inflow from deleted branches remain unlabeled ("").
//
// branches:   branch refs (name -> tip SHA).
// firstParent: SHA -> first parent SHA ("" if none / root).
func assignBranchOf(branches []model.Ref, firstParent map[string]string, inSet map[string]bool, defaultBranch string) map[string]string {
	// Priority order: default branch first, then the rest by name (stable).
	ordered := make([]model.Ref, 0, len(branches))
	for _, b := range branches {
		if b.Name == defaultBranch {
			ordered = append(ordered, b)
		}
	}
	rest := make([]model.Ref, 0, len(branches))
	for _, b := range branches {
		if b.Name != defaultBranch {
			rest = append(rest, b)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].Name < rest[j].Name })
	ordered = append(ordered, rest...)

	label := make(map[string]string, len(firstParent))
	for _, b := range ordered {
		cur := b.TargetSHA
		for cur != "" && inSet[cur] {
			if _, done := label[cur]; done {
				break // this line already claimed by a higher-priority branch
			}
			label[cur] = b.Name
			cur = firstParent[cur]
		}
	}
	return label
}
