package ontology

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wm-it-25/git-branch-graph/internal/model"
)

// activeWindow: an existing branch whose tip is within this of the newest commit
// is "active"; older existing branches are "stale".
const activeWindow = 90 * 24 * time.Hour

// roleRank orders columns left→right: development branches sit left of the
// default, integration/production branches to its right.
var roleRank = map[string]int{"feature": 0, "default": 1, "release": 2, "hotfix": 3, "master": 4, "other": 5}

// containKeep builds the set of commits whose containment is emitted, per the
// mode: "" / "full" = all (nil), "pr-only" = commits carrying a PR, "recent:N" =
// the newest N commits. Restricting only shrinks the OUTPUT, never the answer.
func containKeep(mode string, commits []model.Commit, order []string) map[string]bool {
	switch {
	case mode == "" || mode == "full":
		return nil
	case mode == "pr-only":
		keep := make(map[string]bool)
		for _, c := range commits {
			if c.PRNum != "" {
				keep[c.SHA] = true
			}
		}
		return keep
	case strings.HasPrefix(mode, "recent:"):
		n, err := strconv.Atoi(strings.TrimPrefix(mode, "recent:"))
		if err != nil || n < 0 {
			return nil
		}
		if n > len(order) {
			n = len(order)
		}
		keep := make(map[string]bool, n)
		for _, sha := range order[:n] {
			keep[sha] = true
		}
		return keep
	default:
		return nil
	}
}

// roleOf classifies a branch by its GitFlow role from its name.
func roleOf(name, defaultBranch string) string {
	if name == defaultBranch {
		return "default"
	}
	l := strings.ToLower(name)
	switch {
	case l == "master" || l == "main" || l == "production" || l == "prod":
		return "master"
	case hasAnyPrefix(l, "release/", "release-", "releases/", "rel/"):
		return "release"
	case hasAnyPrefix(l, "hotfix/", "hotfix-", "hotfixes/"):
		return "hotfix"
	default:
		return "feature" // feature/*, fix/*, dev-stage, environment, uncategorized
	}
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

type branchMeta struct {
	name string
	date time.Time
	kind string
	role string
}

// computeColumns classifies existing branches (default / active / stale) and
// returns them ordered default → active → stale (each group newest-tip first),
// plus a name→column-index map. The "other" column (deleted/merged-in commits)
// is appended later by the caller only if needed.
func computeColumns(refs []model.Ref, commitOf map[string]*model.Commit, defaultBranch string) ([]model.Column, map[string]int) {
	// Newest commit date across the graph — the reference point for staleness.
	var maxDate time.Time
	for _, c := range commitOf {
		if t, err := time.Parse(time.RFC3339, c.CommittedAt); err == nil && t.After(maxDate) {
			maxDate = t
		}
	}

	var branches []branchMeta
	for _, r := range refs {
		if r.Type != "branch" {
			continue
		}
		bm := branchMeta{name: r.Name}
		if c, ok := commitOf[r.TargetSHA]; ok {
			if t, err := time.Parse(time.RFC3339, c.CommittedAt); err == nil {
				bm.date = t
			}
		}
		switch {
		case r.Name == defaultBranch:
			bm.kind = "default"
		case !maxDate.IsZero() && !bm.date.IsZero() && maxDate.Sub(bm.date) <= activeWindow:
			bm.kind = "active"
		default:
			bm.kind = "stale"
		}
		bm.role = roleOf(r.Name, defaultBranch)
		branches = append(branches, bm)
	}

	// Order by GitFlow role (feature → default → release → hotfix → master),
	// newest tip first within a role group.
	sort.SliceStable(branches, func(i, j int) bool {
		if roleRank[branches[i].role] != roleRank[branches[j].role] {
			return roleRank[branches[i].role] < roleRank[branches[j].role]
		}
		return branches[i].date.After(branches[j].date)
	})

	cols := make([]model.Column, 0, len(branches))
	idx := make(map[string]int, len(branches))
	for i, b := range branches {
		cols = append(cols, model.Column{Name: b.name, Kind: b.kind, Role: b.role, Color: branchColor(b.name, defaultBranch)})
		idx[b.name] = i
	}
	return cols, idx
}
