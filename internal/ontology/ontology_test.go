package ontology

import (
	"testing"

	"github.com/wm-it-25/git-branch-graph/internal/model"
)

// buildGraph creates a small graph:
//
//	C (dev tip, merge of B and F)
//	├─ B ─ A (root)        first-parent trunk
//	└─ F ─ E ─ A           feature branch merged in
//
// times are descending C>B>F>E>A so topo order is deterministic.
func testData() (model.Snapshot, []model.Commit, []model.Ref, []model.Edge) {
	c := func(sha string, ps []string, t string, subj string) model.Commit {
		return model.Commit{SHA: sha, Parents: ps, CommittedAt: t, Subject: subj, IsMerge: len(ps) >= 2}
	}
	cM := c("C", []string{"B", "F"}, "2026-01-05T00:00:00Z", "merge feature (#9)")
	cM.PRNum = "9"
	commits := []model.Commit{
		cM,
		c("B", []string{"A"}, "2026-01-04T00:00:00Z", "b"),
		c("F", []string{"E"}, "2026-01-03T00:00:00Z", "f"),
		c("E", []string{"A"}, "2026-01-02T00:00:00Z", "e"),
		c("A", nil, "2026-01-01T00:00:00Z", "root"),
	}
	refs := []model.Ref{
		{Name: "dev", Type: "branch", TargetSHA: "C", IsDefault: true},
		{Name: "v1", Type: "tag", TargetSHA: "B"},
	}
	edges := []model.Edge{
		{Child: "C", Parent: "B", ParentIndex: 0, Type: "commit"},
		{Child: "C", Parent: "F", ParentIndex: 1, Type: "merge"},
		{Child: "B", Parent: "A", ParentIndex: 0, Type: "commit"},
		{Child: "F", Parent: "E", ParentIndex: 0, Type: "commit"},
		{Child: "E", Parent: "A", ParentIndex: 0, Type: "commit"},
	}
	snap := model.Snapshot{
		Ref: model.RepoRef{Org: "o", Repo: "r"}, DefaultBranch: "dev",
		CommitCount: 5, BranchCount: 1, TagCount: 1,
	}
	return snap, commits, refs, edges
}

func TestTopoOrderChildrenBeforeParents(t *testing.T) {
	_, commits, _, _ := testData()
	order := topoOrder(commits)
	pos := map[string]int{}
	for i, s := range order {
		pos[s] = i
	}
	// Every parent must come after its child.
	for _, c := range commits {
		for _, p := range c.Parents {
			if pos[c.SHA] > pos[p] {
				t.Errorf("parent %s appears before child %s", p, c.SHA)
			}
		}
	}
	if order[0] != "C" {
		t.Errorf("newest (C) should be first, got %v", order)
	}
}

func TestBuildLanesAndColors(t *testing.T) {
	snap, commits, refs, edges := testData()
	g := Build(snap, commits, refs, edges, nil, nil, model.LocalState{}, "")

	byS := map[string]model.Node{}
	for _, n := range g.Nodes {
		byS[n.SHA] = n
	}
	// default branch trunk (C,B,A) is green; feature side (F,E) is not.
	if byS["A"].Color != defaultColor {
		t.Errorf("A (dev trunk) color = %s, want default %s", byS["A"].Color, defaultColor)
	}
	if byS["C"].BranchOf != "dev" {
		t.Errorf("C branchOf = %s, want dev", byS["C"].BranchOf)
	}
	// The merge inflow edge C->F must cross lanes (feature on a different lane).
	var inflow model.GEdge
	for _, e := range g.Edges {
		if e.Child == "C" && e.Parent == "F" {
			inflow = e
		}
	}
	if inflow.FromLane == inflow.ToLane {
		t.Errorf("merge inflow C->F should cross lanes, got %d->%d", inflow.FromLane, inflow.ToLane)
	}
	if inflow.Type != "merge" {
		t.Errorf("C->F edge type = %s, want merge", inflow.Type)
	}
	// C is a merge commit carrying (#9) -> classified as a merge PR.
	if byS["C"].MergeMethod != "merge" {
		t.Errorf("C mergeMethod = %q, want merge", byS["C"].MergeMethod)
	}
}

func TestSquashClassification(t *testing.T) {
	// A single-parent commit carrying a PR number is a squash landing; its
	// first-parent edge must be marked squash.
	snap := model.Snapshot{Ref: model.RepoRef{Org: "o", Repo: "r"}, DefaultBranch: "dev"}
	commits := []model.Commit{
		{SHA: "S", Parents: []string{"P"}, CommittedAt: "2026-01-02T00:00:00Z",
			Subject: "fix: thing (#42)", PRNum: "42", IsMerge: false},
		{SHA: "P", Parents: nil, CommittedAt: "2026-01-01T00:00:00Z", Subject: "root"},
	}
	refs := []model.Ref{{Name: "dev", Type: "branch", TargetSHA: "S", IsDefault: true}}
	edges := []model.Edge{{Child: "S", Parent: "P", ParentIndex: 0, Type: "commit"}}
	g := Build(snap, commits, refs, edges, nil, nil, model.LocalState{}, "")

	var sNode model.Node
	for _, n := range g.Nodes {
		if n.SHA == "S" {
			sNode = n
		}
	}
	if sNode.MergeMethod != "squash" {
		t.Errorf("S mergeMethod = %q, want squash", sNode.MergeMethod)
	}
	if len(g.Edges) != 1 || g.Edges[0].Type != "squash" {
		t.Errorf("S->P edge type = %v, want squash", g.Edges)
	}
	if len(g.PRs) != 1 || g.PRs[0].Num != "42" || g.PRs[0].MergeMethod != "squash" {
		t.Errorf("PRs = %+v, want one squash PR #42", g.PRs)
	}
}

func TestContainment(t *testing.T) {
	_, commits, refs, _ := testData()
	order := topoOrder(commits)
	parentsOf := map[string][]string{}
	for _, c := range commits {
		parentsOf[c.SHA] = c.Parents
	}
	cont := computeContainment(order, parentsOf, refs, nil)

	has := func(sha, ref string) bool {
		for _, cr := range cont[sha] {
			if cr.Name == ref {
				return true
			}
		}
		return false
	}
	// dev (tip C) contains all commits.
	for _, s := range []string{"A", "B", "C", "E", "F"} {
		if !has(s, "dev") {
			t.Errorf("dev should contain %s", s)
		}
	}
	// tag v1 (tip B) contains A and B only, not the feature side E/F or the tip C.
	if !has("A", "v1") || !has("B", "v1") {
		t.Error("v1 should contain A and B")
	}
	if has("E", "v1") || has("F", "v1") || has("C", "v1") {
		t.Error("v1 should NOT contain E/F/C")
	}
}

// TestBuildLocalState verifies that local-only commits lose their (dead) GitHub
// links and are marked, and that local-only branch columns are flagged.
func TestBuildLocalState(t *testing.T) {
	snap, commits, refs, edges := testData()
	local := model.LocalState{
		Known:          true,
		Unpushed:       map[string]bool{"C": true}, // tip not pushed yet
		RemoteBranches: map[string]bool{},          // "dev" has no remote counterpart
	}
	g := Build(snap, commits, refs, edges, nil, nil, local, "")

	byS := map[string]model.Node{}
	for _, n := range g.Nodes {
		byS[n.SHA] = n
	}
	if !byS["C"].Unpushed {
		t.Error("C should be marked unpushed")
	}
	if byS["C"].Links.Commit != "" || byS["C"].Links.PR != "" {
		t.Errorf("unpushed C should have no commit/PR links, got %+v", byS["C"].Links)
	}
	if byS["B"].Unpushed {
		t.Error("B is pushed; must not be marked")
	}
	if byS["B"].Links.Commit == "" {
		t.Error("pushed B should keep its commit link")
	}
	if byS["B"].Links.Tree != "" {
		t.Error("tree link should be dropped: branch dev is local-only")
	}
	for _, c := range g.Columns {
		if c.Name == "dev" && !c.LocalOnly {
			t.Error("column dev should be flagged localOnly")
		}
	}

	// Unknown local state must change nothing.
	g2 := Build(snap, commits, refs, edges, nil, nil, model.LocalState{}, "")
	for _, n := range g2.Nodes {
		if n.Unpushed || n.Links.Commit == "" {
			t.Errorf("remote analysis must keep all links: %s %+v", n.SHA, n.Links)
		}
	}
}
