// Package model defines the shared data types produced by the extract stage.
package model

// RepoRef identifies a target repository parsed from a URL or local path.
type RepoRef struct {
	URL  string // original input (remote URL or local path)
	Org  string // owner / parent segment
	Repo string // repository name (without .git)
	Slug string // "<org>__<repo>" — stable filesystem-safe id
}

// Snapshot captures the resolved state at ingest time.
type Snapshot struct {
	Ref           RepoRef
	DefaultBranch string
	HeadSHA       string
	CapturedAt    string // RFC3339
	CommitCount   int
	BranchCount   int
	TagCount      int
}

// Commit is one node in the graph (raw/commits.csv).
type Commit struct {
	SHA         string
	Parents     []string
	AuthorName  string
	AuthorEmail string
	AuthoredAt  string // ISO8601
	CommittedAt string
	Refs        string // git %D decoration
	Subject     string
	PRNum       string // parsed from "(#123)"; "" if none
	IsMerge     bool
}

// Ref is a branch or tag pointer (raw/refs.csv).
type Ref struct {
	Name      string
	Type      string // "branch" | "tag"
	TargetSHA string
	IsDefault bool
}

// PR is pull-request metadata (raw/prs.csv). MergeMethod is classified offline
// (parent count) and refined online by enrich. Fields beyond Num/MergeMethod are
// populated only when enrich runs with a token.
type PR struct {
	Num         string
	State       string // "merged" | "open" | "closed" | "" (unknown offline)
	MergeMethod string // "merge" | "squash" | "rebase" | ""
	MergeSHA    string
	BaseRef     string
	HeadRef     string
	URL         string
	CIState     string // "SUCCESS" | "FAILURE" | "PENDING" | "" (rollup)
}

// Edge is a parent relationship, one row per (child,parent) (raw/edges.csv).
type Edge struct {
	Child       string
	Parent      string
	ParentIndex int
	Type        string // "commit" | "merge" (refined later to squash|cherry)
}

// ---- Ontology output (graph.json / graph.sqlite) ----

// Column is one branch lane in the fixed-column layout. Role drives the left→
// right order (feature < default < release < hotfix < master < other); Kind
// drives currency (spine rules / label): default | active | stale | other.
type Column struct {
	Name  string
	Kind  string // "default" | "active" | "stale" | "other"
	Role  string // "feature" | "default" | "release" | "hotfix" | "master" | "other"
	Color string
}

// Graph is the computed ontology: render-ready nodes + edges plus meta.
type Graph struct {
	Meta     Snapshot
	LinkBase string
	Columns  []Column
	Nodes    []Node
	Edges    []GEdge
	PRs      []PR
	// Containment is the full commit->ref membership set (SQLite only; too large
	// to inline in JSON). Keyed by commit SHA -> list of refs containing it.
	Containment map[string][]ContainRef
}

// Node is a render-ready commit (one graph vertex).
type Node struct {
	SHA               string
	Lane              int
	Col               int // fixed branch column index (into Graph.Columns)
	Color             string
	Subject           string
	Author            string
	CommittedAt       string
	PRNum             string
	IsMerge           bool
	MergeMethod       string    // "merge" | "squash" | "rebase" | "" — landing method
	CIState           string    // PR CI rollup ("" if unknown)
	PRVerified        string    // "verified" | "unverified" | "" (no PR / enrich not run)
	CherryFrom        string    // source SHA this commit was cherry-picked from ("" if none)
	CherryTo          []string  // SHAs that cherry-picked this commit (in-graph)
	BranchOf          string    // first-parent owning branch ("" if none)
	Refs              []NodeRef // branch/tag decorations pointing here
	ContainedBranches []string  // lightweight; inlined in JSON
	Links             NodeLinks
}

// NodeRef is a branch/tag whose tip is exactly this commit.
type NodeRef struct {
	Name string
	Type string // "branch" | "tag"
}

// NodeLinks holds pre-assembled GitHub URLs for a node.
type NodeLinks struct {
	Commit string
	PR     string // "" if no PR
	Tree   string // "" if no owning branch
}

// GEdge is a render edge with resolved lane endpoints.
type GEdge struct {
	Child       string
	Parent      string
	ParentIndex int
	Type        string
	FromLane    int
	ToLane      int
}

// ContainRef is one (ref) that contains a commit.
type ContainRef struct {
	Name string
	Type string // "branch" | "tag"
}
