package ontology

import (
	"container/heap"

	"github.com/wm-it-25/git-branch-graph/internal/model"
)

// topoOrder returns commit SHAs newest-first in a topologically valid order:
// every commit appears before all of its parents. Among ready commits, the one
// with the newest committed_at wins (ties broken by SHA for determinism).
//
// This single canonical order is used by both lane assignment and containment
// propagation (child processed before parent).
func topoOrder(commits []model.Commit) []string {
	idx := make(map[string]*model.Commit, len(commits))
	for i := range commits {
		idx[commits[i].SHA] = &commits[i]
	}

	// pendingChildren[sha] = in-set children not yet emitted.
	pending := make(map[string]int, len(commits))
	for _, c := range commits {
		for _, p := range c.Parents {
			if _, ok := idx[p]; ok {
				pending[p]++
			}
		}
	}

	h := &commitHeap{}
	heap.Init(h)
	for i := range commits {
		if pending[commits[i].SHA] == 0 { // tips (no in-set children)
			heap.Push(h, &commits[i])
		}
	}

	order := make([]string, 0, len(commits))
	for h.Len() > 0 {
		c := heap.Pop(h).(*model.Commit)
		order = append(order, c.SHA)
		for _, p := range c.Parents {
			pc, ok := idx[p]
			if !ok {
				continue
			}
			pending[p]--
			if pending[p] == 0 {
				heap.Push(h, pc)
			}
		}
	}
	return order
}

// commitHeap is a max-heap by committed_at (ISO8601 sorts chronologically),
// SHA as a deterministic tiebreak.
type commitHeap []*model.Commit

func (h commitHeap) Len() int { return len(h) }
func (h commitHeap) Less(i, j int) bool {
	if h[i].CommittedAt != h[j].CommittedAt {
		return h[i].CommittedAt > h[j].CommittedAt // newer first
	}
	return h[i].SHA > h[j].SHA
}
func (h commitHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *commitHeap) Push(x any)   { *h = append(*h, x.(*model.Commit)) }
func (h *commitHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return x
}
