// Package ontology turns the raw layer (commits, refs, edges) into a
// render-ready graph: lanes, colors, branch ownership, containment, and GitHub
// links. All computation is in-memory (no blob access), so it works on a
// blobless clone. Squash/cherry edge refinement needs PR metadata and is done
// later in the enrich stage.
package ontology

import (
	"fmt"
	"sort"

	"github.com/wm-it-25/git-branch-graph/internal/model"
)

// Build computes the full graph from the raw layer. enriched carries optional PR
// data keyed by PR number (nil when enrich did not run); it refines the offline
// merge/squash classification.
func Build(snap model.Snapshot, commits []model.Commit, refs []model.Ref, edges []model.Edge, enriched map[string]model.PR, cherryPicks map[string]string, local model.LocalState, containMode string) model.Graph {
	linkBase := fmt.Sprintf("https://github.com/%s/%s", snap.Ref.Org, snap.Ref.Repo)

	// Index and derived maps.
	inSet := make(map[string]bool, len(commits))
	commitOf := make(map[string]*model.Commit, len(commits))
	parentsOf := make(map[string][]string, len(commits))
	firstParent := make(map[string]string, len(commits))
	for i := range commits {
		c := &commits[i]
		inSet[c.SHA] = true
		commitOf[c.SHA] = c
	}
	for i := range commits {
		c := &commits[i]
		var ps []string
		for _, p := range c.Parents {
			if inSet[p] {
				ps = append(ps, p)
			}
		}
		parentsOf[c.SHA] = ps
		if len(ps) > 0 {
			firstParent[c.SHA] = ps[0]
		}
	}

	order := topoOrder(commits)
	laneOf := assignLanes(order, parentsOf)

	// Branch ownership + colors.
	var branches []model.Ref
	for _, r := range refs {
		if r.Type == "branch" {
			branches = append(branches, r)
		}
	}
	branchOf := assignBranchOf(branches, firstParent, inSet, snap.DefaultBranch)

	// Refs whose tip is exactly a given commit (for decoration).
	refsAt := make(map[string][]model.NodeRef, len(refs))
	for _, r := range refs {
		refsAt[r.TargetSHA] = append(refsAt[r.TargetSHA], model.NodeRef{Name: r.Name, Type: r.Type})
	}

	containment := computeContainment(order, parentsOf, refs, containKeep(containMode, commits, order))

	// Cherry-pick relations (from `-x` markers): forward source + reverse targets.
	cherryTo := map[string][]string{}
	for cherry, source := range cherryPicks {
		if inSet[cherry] && inSet[source] {
			cherryTo[source] = append(cherryTo[source], cherry)
		}
	}

	// Lightweight per-node branch containment (JSON-inlined; tags stay in SQLite).
	branchContain := make(map[string][]string, len(commits))
	for sha, list := range containment {
		var bs []string
		for _, cr := range list {
			if cr.Type == "branch" {
				bs = append(bs, cr.Name)
			}
		}
		if bs != nil {
			sort.Strings(bs)
			branchContain[sha] = bs
		}
	}

	// PR merge/squash classification (offline + optional enrich override).
	prs, methodOf, ciOf, verifiedOf, squashEdges := classifyPRs(
		commits, commitOf, firstParent, branchOf, linkBase, enriched, enriched != nil)

	// Fixed branch columns (default → active → stale → other) and per-commit
	// column assignment: owning branch, else leftmost containing branch, else other.
	columns, colIdx := computeColumns(refs, commitOf, snap.DefaultBranch)
	// Branches without a remote-tracking counterpart exist only locally — their
	// GitHub tree links would 404.
	if local.Known {
		for i := range columns {
			if !local.RemoteBranches[columns[i].Name] {
				columns[i].LocalOnly = true
			}
		}
	}
	otherCol := len(columns)
	usedOther := false
	colOf := func(sha string) int {
		if bo := branchOf[sha]; bo != "" {
			if c, ok := colIdx[bo]; ok {
				return c
			}
		}
		best := -1
		for _, b := range branchContain[sha] {
			if c, ok := colIdx[b]; ok && (best < 0 || c < best) {
				best = c
			}
		}
		if best >= 0 {
			return best
		}
		usedOther = true
		return otherCol
	}
	colBySha := make(map[string]int, len(commits))
	for _, sha := range order {
		colBySha[sha] = colOf(sha)
	}
	if usedOther {
		columns = append(columns, model.Column{Name: "(merged-in)", Kind: "other", Role: "other", Color: neutralColor})
	}

	// Nodes.
	nodes := make([]model.Node, 0, len(commits))
	for _, sha := range order {
		c := commitOf[sha]
		bo := branchOf[sha]
		// A local-only commit (or a branch that only exists locally) has no
		// remote counterpart — emitting its GitHub URL would just 404.
		unpushed := local.Known && local.Unpushed[sha]
		var links model.NodeLinks
		if !unpushed {
			links.Commit = linkBase + "/commit/" + sha
		}
		// Link the PR only when it isn't known-bad: a verified PR, or an unknown
		// one (enrich didn't run). An "unverified" PR number is likely upstream,
		// so its /pull/N link would be wrong — omit it.
		if c.PRNum != "" && verifiedOf[sha] != "unverified" && !unpushed {
			links.PR = linkBase + "/pull/" + c.PRNum
		}
		if bo != "" && (!local.Known || local.RemoteBranches[bo]) {
			links.Tree = linkBase + "/tree/" + bo
		}
		nodes = append(nodes, model.Node{
			SHA:               sha,
			Lane:              laneOf[sha],
			Col:               colBySha[sha],
			Color:             branchColor(bo, snap.DefaultBranch),
			Subject:           c.Subject,
			Author:            c.AuthorName,
			CommittedAt:       c.CommittedAt,
			PRNum:             c.PRNum,
			IsMerge:           c.IsMerge,
			MergeMethod:       methodOf[sha],
			CIState:           ciOf[sha],
			PRVerified:        verifiedOf[sha],
			CherryFrom:        cherryPicks[sha],
			CherryTo:          cherryTo[sha],
			BranchOf:          bo,
			Refs:              refsAt[sha],
			ContainedBranches: branchContain[sha],
			Unpushed:          unpushed,
			Links:             links,
		})
	}

	// Edges with resolved lane endpoints; mark squash landing edges.
	gedges := make([]model.GEdge, 0, len(edges))
	for _, e := range edges {
		if !inSet[e.Child] || !inSet[e.Parent] {
			continue
		}
		et := e.Type
		if squashEdges[e.Child+"|"+e.Parent] {
			et = "squash"
		}
		gedges = append(gedges, model.GEdge{
			Child:       e.Child,
			Parent:      e.Parent,
			ParentIndex: e.ParentIndex,
			Type:        et,
			FromLane:    laneOf[e.Child],
			ToLane:      laneOf[e.Parent],
		})
	}

	return model.Graph{
		Meta:        snap,
		LinkBase:    linkBase,
		Columns:     columns,
		Nodes:       nodes,
		Edges:       gedges,
		PRs:         prs,
		Containment: containment,
	}
}
