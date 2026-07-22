package ontology

// assignLanes maps each commit to a column (lane) using a greedy, topology-based
// algorithm: the first parent continues the current lane (keeping a branch line
// straight), additional parents (merge inflow) take fresh lanes, and lanes are
// freed and reused once nothing is waiting for them.
//
// order:      commit SHAs newest-first, topologically valid (from topoOrder).
// parentsOf:  SHA -> ordered parent SHAs (in-set).
// Returns lane per SHA.
func assignLanes(order []string, parentsOf map[string][]string) map[string]int {
	laneOf := make(map[string]int, len(order))
	// active[i] = SHA that lane i is currently waiting to place ("" = free).
	var active []string

	freeLane := func(sha string) int {
		for i, s := range active {
			if s == "" {
				active[i] = sha
				return i
			}
		}
		active = append(active, sha)
		return len(active) - 1
	}

	for _, sha := range order {
		// Find the lane already waiting for this commit; else allocate one.
		lane := -1
		for i, s := range active {
			if s == sha {
				lane = i
				break
			}
		}
		if lane == -1 {
			lane = freeLane(sha)
		}
		laneOf[sha] = lane

		// Any OTHER lane also waiting for this SHA converges here -> free it.
		for i, s := range active {
			if i != lane && s == sha {
				active[i] = ""
			}
		}

		parents := parentsOf[sha]
		if len(parents) == 0 {
			active[lane] = "" // root -> release the lane
			continue
		}
		// First parent continues this lane.
		active[lane] = parents[0]
		// Extra parents (merge inflow) take fresh lanes.
		for _, p := range parents[1:] {
			// If some lane is already waiting for p, reuse it; else allocate.
			exists := false
			for _, s := range active {
				if s == p {
					exists = true
					break
				}
			}
			if !exists {
				freeLane(p)
			}
		}
	}
	return laneOf
}
