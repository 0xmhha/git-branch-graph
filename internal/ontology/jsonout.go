package ontology

import (
	"encoding/json"
	"os"

	"github.com/wm-it-25/git-branch-graph/internal/model"
)

// WriteJSON serializes the render-ready view (nodes + edges + meta). Full
// containment is intentionally omitted here — it lives in graph.sqlite — but
// each node keeps its lightweight branch containment inline.
func WriteJSON(path string, g model.Graph) error {
	type jRef struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	type jLinks struct {
		Commit string `json:"commit"`
		PR     string `json:"pr,omitempty"`
		Tree   string `json:"tree,omitempty"`
	}
	type jNode struct {
		SHA               string   `json:"sha"`
		Lane              int      `json:"lane"`
		Color             string   `json:"color"`
		Subject           string   `json:"subject"`
		Author            string   `json:"author"`
		CommittedAt       string   `json:"committedAt"`
		PRNum             string   `json:"prNum,omitempty"`
		IsMerge           bool     `json:"isMerge"`
		MergeMethod       string   `json:"mergeMethod,omitempty"`
		CIState           string   `json:"ciState,omitempty"`
		BranchOf          string   `json:"branchOf,omitempty"`
		Refs              []jRef   `json:"refs,omitempty"`
		ContainedBranches []string `json:"containedBranches,omitempty"`
		Links             jLinks   `json:"links"`
	}
	type jEdge struct {
		Child       string `json:"child"`
		Parent      string `json:"parent"`
		ParentIndex int    `json:"parentIndex"`
		Type        string `json:"type"`
		FromLane    int    `json:"fromLane"`
		ToLane      int    `json:"toLane"`
	}
	type jDoc struct {
		Meta struct {
			Org           string `json:"org"`
			Repo          string `json:"repo"`
			DefaultBranch string `json:"defaultBranch"`
			HeadSHA       string `json:"headSha"`
			CapturedAt    string `json:"capturedAt"`
			Counts        struct {
				Commits  int `json:"commits"`
				Branches int `json:"branches"`
				Tags     int `json:"tags"`
			} `json:"counts"`
		} `json:"meta"`
		LinkBase string  `json:"linkBase"`
		Nodes    []jNode `json:"nodes"`
		Edges    []jEdge `json:"edges"`
	}

	var doc jDoc
	doc.Meta.Org = g.Meta.Ref.Org
	doc.Meta.Repo = g.Meta.Ref.Repo
	doc.Meta.DefaultBranch = g.Meta.DefaultBranch
	doc.Meta.HeadSHA = g.Meta.HeadSHA
	doc.Meta.CapturedAt = g.Meta.CapturedAt
	doc.Meta.Counts.Commits = g.Meta.CommitCount
	doc.Meta.Counts.Branches = g.Meta.BranchCount
	doc.Meta.Counts.Tags = g.Meta.TagCount
	doc.LinkBase = g.LinkBase

	doc.Nodes = make([]jNode, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		var refs []jRef
		for _, r := range n.Refs {
			refs = append(refs, jRef{Name: r.Name, Type: r.Type})
		}
		doc.Nodes = append(doc.Nodes, jNode{
			SHA: n.SHA, Lane: n.Lane, Color: n.Color, Subject: n.Subject,
			Author: n.Author, CommittedAt: n.CommittedAt, PRNum: n.PRNum,
			IsMerge: n.IsMerge, MergeMethod: n.MergeMethod, CIState: n.CIState,
			BranchOf: n.BranchOf, Refs: refs,
			ContainedBranches: n.ContainedBranches,
			Links:             jLinks{Commit: n.Links.Commit, PR: n.Links.PR, Tree: n.Links.Tree},
		})
	}
	doc.Edges = make([]jEdge, 0, len(g.Edges))
	for _, e := range g.Edges {
		doc.Edges = append(doc.Edges, jEdge{
			Child: e.Child, Parent: e.Parent, ParentIndex: e.ParentIndex,
			Type: e.Type, FromLane: e.FromLane, ToLane: e.ToLane,
		})
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	return enc.Encode(doc)
}
