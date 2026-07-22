package ontology

import "github.com/wm-it-25/git-branch-graph/internal/model"

// classifyPRs derives per-PR merge method and marks squash landing edges.
//
// Offline signal (no token): a commit that carries a PR number lands either as a
// merge commit (>=2 parents) or a squashed single commit (1 parent). This alone
// classifies the common merge/squash workflows. When enrich has supplied PR data
// (enriched != nil), its mergeCommit parent count / state / refs take precedence
// and can additionally distinguish rebase.
//
// Returns PRs keyed for the SQLite/JSON layers plus:
//   - method[sha]  : merge method attributed to that landing commit
//   - ciOf[sha]    : CI rollup state for that landing commit (enriched only)
//   - squashEdges  : set of "child|parent" first-parent edges to mark as squash
func classifyPRs(commits []model.Commit, commitOf map[string]*model.Commit,
	firstParent map[string]string, branchOf map[string]string, linkBase string,
	enriched map[string]model.PR) (prs []model.PR, method, ciOf map[string]string, squashEdges map[string]bool) {

	method = map[string]string{}
	ciOf = map[string]string{}
	squashEdges = map[string]bool{}

	for i := range commits {
		c := &commits[i]
		if c.PRNum == "" {
			continue
		}
		pr := model.PR{
			Num:      c.PRNum,
			MergeSHA: c.SHA,
			BaseRef:  branchOf[c.SHA],
			URL:      linkBase + "/pull/" + c.PRNum,
		}
		// Offline classification from parent count.
		if c.IsMerge {
			pr.MergeMethod = "merge"
		} else {
			pr.MergeMethod = "squash"
		}

		// Enrich override (authoritative when present).
		if e, ok := enriched[c.PRNum]; ok {
			if e.State != "" {
				pr.State = e.State
			}
			if e.BaseRef != "" {
				pr.BaseRef = e.BaseRef
			}
			pr.HeadRef = e.HeadRef
			if e.URL != "" {
				pr.URL = e.URL
			}
			pr.CIState = e.CIState
			if e.MergeMethod != "" {
				pr.MergeMethod = e.MergeMethod
			}
		}

		method[c.SHA] = pr.MergeMethod
		if pr.CIState != "" {
			ciOf[c.SHA] = pr.CIState
		}
		if pr.MergeMethod == "squash" {
			if p, ok := firstParent[c.SHA]; ok {
				squashEdges[c.SHA+"|"+p] = true
			}
		}
		prs = append(prs, pr)
	}
	return prs, method, ciOf, squashEdges
}
