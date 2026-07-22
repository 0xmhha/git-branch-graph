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
	SHA          string
	Parents      []string
	AuthorName   string
	AuthorEmail  string
	AuthoredAt   string // ISO8601
	CommittedAt  string
	Refs         string // git %D decoration
	Subject      string
	PRNum        string // parsed from "(#123)"; "" if none
	IsMerge      bool
}

// Ref is a branch or tag pointer (raw/refs.csv).
type Ref struct {
	Name      string
	Type      string // "branch" | "tag"
	TargetSHA string
	IsDefault bool
}

// Edge is a parent relationship, one row per (child,parent) (raw/edges.csv).
type Edge struct {
	Child       string
	Parent      string
	ParentIndex int
	Type        string // "commit" | "merge" (refined later to squash|cherry)
}
