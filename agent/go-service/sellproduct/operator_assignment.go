package sellproduct

type restoreAssignmentPlan struct {
	Assignments map[string]operatorCandidate
	Assigned    int
	TotalCost   int
}

func buildRestoreAssignmentPlan(groups []operatorCandidateGroup, owned map[string]struct{}) restoreAssignmentPlan {
	best := restoreAssignmentPlan{
		Assignments: map[string]operatorCandidate{},
	}
	current := map[string]operatorCandidate{}
	used := map[string]struct{}{}

	var walk func(index int, assigned int, totalCost int)
	walk = func(index int, assigned int, totalCost int) {
		if index >= len(groups) {
			if isBetterRestorePlan(assigned, totalCost, best.Assigned, best.TotalCost) {
				best.Assigned = assigned
				best.TotalCost = totalCost
				best.Assignments = cloneRestoreAssignments(current)
			}
			return
		}

		group := groups[index]
		walk(index+1, assigned, totalCost)

		for _, candidate := range filterOwnedCandidates(group.Candidates, owned) {
			if _, ok := used[candidate.Name]; ok {
				continue
			}
			used[candidate.Name] = struct{}{}
			current[group.Location] = candidate
			walk(index+1, assigned+1, totalCost+candidate.Priority)
			delete(current, group.Location)
			delete(used, candidate.Name)
		}
	}
	walk(0, 0, 0)
	return best
}

func isBetterRestorePlan(assigned, totalCost, bestAssigned, bestTotalCost int) bool {
	if assigned != bestAssigned {
		return assigned > bestAssigned
	}
	return totalCost < bestTotalCost
}

func cloneRestoreAssignments(src map[string]operatorCandidate) map[string]operatorCandidate {
	dst := make(map[string]operatorCandidate, len(src))
	for location, candidate := range src {
		dst[location] = candidate
	}
	return dst
}
