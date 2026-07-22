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

// Build computes the full graph from the raw layer.
func Build(snap model.Snapshot, commits []model.Commit, refs []model.Ref, edges []model.Edge) model.Graph {
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

	containment := computeContainment(order, parentsOf, refs)

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

	// Nodes.
	nodes := make([]model.Node, 0, len(commits))
	for _, sha := range order {
		c := commitOf[sha]
		bo := branchOf[sha]
		links := model.NodeLinks{Commit: linkBase + "/commit/" + sha}
		if c.PRNum != "" {
			links.PR = linkBase + "/pull/" + c.PRNum
		}
		if bo != "" {
			links.Tree = linkBase + "/tree/" + bo
		}
		nodes = append(nodes, model.Node{
			SHA:               sha,
			Lane:              laneOf[sha],
			Color:             branchColor(bo, snap.DefaultBranch),
			Subject:           c.Subject,
			Author:            c.AuthorName,
			CommittedAt:       c.CommittedAt,
			PRNum:             c.PRNum,
			IsMerge:           c.IsMerge,
			BranchOf:          bo,
			Refs:              refsAt[sha],
			ContainedBranches: branchContain[sha],
			Links:             links,
		})
	}

	// Edges with resolved lane endpoints.
	gedges := make([]model.GEdge, 0, len(edges))
	for _, e := range edges {
		if !inSet[e.Child] || !inSet[e.Parent] {
			continue
		}
		gedges = append(gedges, model.GEdge{
			Child:       e.Child,
			Parent:      e.Parent,
			ParentIndex: e.ParentIndex,
			Type:        e.Type,
			FromLane:    laneOf[e.Child],
			ToLane:      laneOf[e.Parent],
		})
	}

	return model.Graph{
		Meta:        snap,
		LinkBase:    linkBase,
		Nodes:       nodes,
		Edges:       gedges,
		Containment: containment,
	}
}
