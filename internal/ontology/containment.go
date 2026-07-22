package ontology

import "github.com/wm-it-25/git-branch-graph/internal/model"

// computeContainment answers "which refs contain each commit" for every commit.
//
// A commit C is contained in ref R iff C is an ancestor of (or equal to) R's
// target. We seed each ref's bit at its target commit, then push bits down to
// parents in topological order (child before parent). Because `order` guarantees
// every child is processed before its parents, each commit's bitset is complete
// by the time we OR it into its parents. One pass over the edges — no per-commit
// `git --contains` calls.
//
// keep, when non-nil, restricts which commits appear in the OUTPUT (the bitset
// propagation always runs fully, so the answer stays correct). Used by the
// partial-containment modes to shrink the emitted table on very large repos.
//
// Returns commit SHA -> refs containing it. Uses a fixed-width bitset (nrefs
// bits) per commit, so memory is ~nrefs/8 bytes × ncommits.
func computeContainment(order []string, parentsOf map[string][]string, refs []model.Ref, keep map[string]bool) map[string][]model.ContainRef {
	words := (len(refs) + 63) / 64
	if words == 0 {
		return map[string][]model.ContainRef{}
	}

	bits := make(map[string][]uint64, len(order))
	inSet := make(map[string]bool, len(order))
	for _, sha := range order {
		bits[sha] = make([]uint64, words)
		inSet[sha] = true
	}

	// Seed: each ref sets its bit on its target commit.
	for i, r := range refs {
		if bs, ok := bits[r.TargetSHA]; ok {
			bs[i/64] |= 1 << uint(i%64)
		}
	}

	// Propagate down to parents (child processed before parent).
	for _, sha := range order {
		bs := bits[sha]
		for _, p := range parentsOf[sha] {
			pb, ok := bits[p]
			if !ok {
				continue
			}
			for w := 0; w < words; w++ {
				pb[w] |= bs[w]
			}
		}
	}

	// Decode bitsets -> ref lists (only for kept commits when a filter is set).
	out := make(map[string][]model.ContainRef, len(order))
	for _, sha := range order {
		if keep != nil && !keep[sha] {
			continue
		}
		bs := bits[sha]
		var list []model.ContainRef
		for i, r := range refs {
			if bs[i/64]&(1<<uint(i%64)) != 0 {
				list = append(list, model.ContainRef{Name: r.Name, Type: r.Type})
			}
		}
		if list != nil {
			out[sha] = list
		}
	}
	return out
}
