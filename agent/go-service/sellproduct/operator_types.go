package sellproduct

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	scanOwnedOperatorsActionName = "SellProductScanOwnedOperators"
	selectBestOperatorActionName = "SellProductSelectBestOperator"

	operatorCacheModeCache   = "cache"
	operatorCacheModeRefresh = "refresh"

	operatorActionUsageTarget  = "target"
	operatorActionUsageRestore = "restore"

	defaultOperatorMaxSwipes = 8
)

type operatorCandidate struct {
	Name     string   `json:"name"`
	Expected []string `json:"expected"`
	Priority int      `json:"priority"`
}

type operatorCandidateGroup struct {
	Location   string              `json:"location"`
	Candidates []operatorCandidate `json:"candidates"`
}

type operatorActionParam struct {
	Mode      string `json:"mode"`
	Usage     string `json:"usage"`
	Location  string `json:"location"`
	ROI       []int  `json:"roi"`
	MaxSwipes int    `json:"max_swipes"`
}

type operatorSelectionParam struct {
	Usage         string
	Location      string
	Candidates    []operatorCandidate
	RestoreGroups []operatorCandidateGroup
}

func parseOperatorActionParam(raw string) (*operatorActionParam, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("custom_action_param is empty")
	}

	var p operatorActionParam
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("unmarshal custom_action_param: %w", err)
	}
	p.Mode = strings.TrimSpace(p.Mode)
	if p.Mode == "" {
		p.Mode = operatorCacheModeCache
	}
	if p.Mode != operatorCacheModeCache && p.Mode != operatorCacheModeRefresh {
		return nil, fmt.Errorf("invalid mode %q", p.Mode)
	}
	p.Usage = strings.TrimSpace(p.Usage)
	if p.Usage != operatorActionUsageTarget && p.Usage != operatorActionUsageRestore {
		return nil, fmt.Errorf("invalid usage %q", p.Usage)
	}
	p.Location = strings.TrimSpace(p.Location)
	if p.Location == "" {
		return nil, fmt.Errorf("location is empty")
	}
	if p.MaxSwipes <= 0 {
		p.MaxSwipes = defaultOperatorMaxSwipes
	}
	if len(p.ROI) == 0 {
		p.ROI = []int{164, 121, 700, 430}
	}
	if len(p.ROI) != 4 {
		return nil, fmt.Errorf("invalid roi length %d, expected 4", len(p.ROI))
	}
	return &p, nil
}

func normalizeOperatorCandidates(candidates []operatorCandidate) []operatorCandidate {
	normalized := make([]operatorCandidate, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate.Name = strings.TrimSpace(candidate.Name)
		candidate.Expected = uniqueNonEmptyStrings(candidate.Expected)
		if candidate.Name == "" || len(candidate.Expected) == 0 {
			continue
		}
		if _, ok := seen[candidate.Name]; ok {
			continue
		}
		seen[candidate.Name] = struct{}{}
		normalized = append(normalized, candidate)
	}
	sortOperatorCandidates(normalized)
	return normalized
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func sortOperatorCandidates(candidates []operatorCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Priority < candidates[j].Priority
	})
}

func normalizeOperatorCandidateGroups(groups []operatorCandidateGroup) []operatorCandidateGroup {
	normalized := make([]operatorCandidateGroup, 0, len(groups))
	seen := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		group.Location = strings.TrimSpace(group.Location)
		if group.Location == "" {
			continue
		}
		if _, ok := seen[group.Location]; ok {
			continue
		}
		group.Candidates = normalizeOperatorCandidates(group.Candidates)
		if len(group.Candidates) == 0 {
			continue
		}
		seen[group.Location] = struct{}{}
		normalized = append(normalized, group)
	}
	return normalized
}

func filterOwnedCandidates(candidates []operatorCandidate, owned map[string]struct{}) []operatorCandidate {
	if len(candidates) == 0 || len(owned) == 0 {
		return nil
	}
	filtered := make([]operatorCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := owned[candidate.Name]; ok {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func collectScanCandidates(p *operatorSelectionParam) []operatorCandidate {
	if p == nil {
		return nil
	}
	candidates := append([]operatorCandidate{}, p.Candidates...)
	for _, group := range p.RestoreGroups {
		candidates = append(candidates, group.Candidates...)
	}
	return normalizeOperatorCandidates(candidates)
}
