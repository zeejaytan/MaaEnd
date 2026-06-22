package sellproduct

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
)

const (
	settlementTradeResourcePath      = "data/settlement_trade.json"
	settlementTradeDevResourcePath   = "tools/pipeline-generate/data/settlement_trade.json"
	operatorLocaleOrderResourcePath  = "locales/interface/zh_cn.json"
	operatorLocaleOrderResourcePath2 = "assets/locales/interface/zh_cn.json"
)

var loadOperatorSelectionDataFunc = loadOperatorSelectionData

type operatorSelectionData struct {
	TargetCandidates map[string][]operatorCandidate
	RestoreGroups    []operatorCandidateGroup
}

type settlementTradeFile struct {
	Settlements map[string]settlementTradeSettlement `json:"settlements"`
}

type settlementTradeSettlement struct {
	SettlementName     map[string]string        `json:"settlementName"`
	DomainID           string                   `json:"domainId"`
	SettlementFeatures []settlementTradeFeature `json:"settlementFeatures"`
}

type settlementTradeFeature struct {
	Bonuses           []settlementTradeBonus    `json:"bonuses"`
	MatchingOperators []settlementTradeOperator `json:"matchingOperators"`
}

type settlementTradeBonus struct {
	Type string `json:"type"`
}

type settlementTradeOperator struct {
	CharID string            `json:"charId"`
	Name   map[string]string `json:"name"`
}

type settlementLocation struct {
	SettlementID string
	LocationID   string
	Settlement   settlementTradeSettlement
}

func loadOperatorSelectionData() (*operatorSelectionData, error) {
	var data settlementTradeFile
	if err := readSettlementTradeFile(&data); err != nil {
		return nil, err
	}
	localeOrder := loadOperatorLocaleOrder()
	return buildOperatorSelectionData(data, localeOrder), nil
}

func readSettlementTradeFile(out *settlementTradeFile) error {
	for _, path := range []string{
		settlementTradeDevResourcePath,
		settlementTradeResourcePath,
	} {
		if err := readJsonFromRepoOrResource(path, out); err == nil {
			return nil
		}
	}
	return fmt.Errorf("settlement_trade.json not found")
}

func buildOperatorSelectionData(data settlementTradeFile, localeOrder map[string]int) *operatorSelectionData {
	locations := settlementLocations(data)
	result := &operatorSelectionData{
		TargetCandidates: make(map[string][]operatorCandidate, len(locations)),
		RestoreGroups:    make([]operatorCandidateGroup, 0, len(locations)),
	}
	for _, loc := range locations {
		targetCandidates := buildTargetCandidates(loc.Settlement, localeOrder)
		restoreCandidates := buildRestoreCandidates(loc.Settlement, localeOrder)
		result.TargetCandidates[loc.LocationID] = targetCandidates
		result.RestoreGroups = append(result.RestoreGroups, operatorCandidateGroup{
			Location:   loc.LocationID,
			Candidates: restoreCandidates,
		})
	}
	result.RestoreGroups = normalizeOperatorCandidateGroups(result.RestoreGroups)
	return result
}

func settlementLocations(data settlementTradeFile) []settlementLocation {
	locations := make([]settlementLocation, 0, len(data.Settlements))
	for settlementID, settlement := range data.Settlements {
		locations = append(locations, settlementLocation{
			SettlementID: settlementID,
			LocationID:   settlementLocationID(settlementID, settlement),
			Settlement:   settlement,
		})
	}
	sort.SliceStable(locations, func(i, j int) bool {
		a := locations[i]
		b := locations[j]
		if a.Settlement.DomainID != b.Settlement.DomainID {
			return a.Settlement.DomainID < b.Settlement.DomainID
		}
		return a.SettlementID < b.SettlementID
	})
	return locations
}

func settlementLocationID(settlementID string, settlement settlementTradeSettlement) string {
	switch settlementID {
	case "stm_tundra_1":
		return "RefugeeCamp"
	case "stm_tundra_2":
		return "InfrastructureOutpost"
	case "stm_tundra_3":
		return "ReconstructionCommand"
	case "stm_hongs_1":
		return "SkyKingFlats"
	default:
		return toPascalCase(firstNonEmpty(settlement.SettlementName["EN"], settlementID))
	}
}

func buildTargetCandidates(settlement settlementTradeSettlement, localeOrder map[string]int) []operatorCandidate {
	operators := collectOperatorBonusTypes(settlement, map[string]struct{}{
		"expProfit":   {},
		"moneyProfit": {},
	})
	entries := sortedOperatorEntries(operators, localeOrder)
	candidates := make([]operatorCandidate, 0, len(entries))
	for _, entry := range entries {
		priority := 3
		if _, hasExp := entry.BonusTypes["expProfit"]; hasExp {
			if _, hasMoney := entry.BonusTypes["moneyProfit"]; hasMoney {
				priority = 0
			} else {
				priority = 2
			}
		} else if _, hasMoney := entry.BonusTypes["moneyProfit"]; hasMoney {
			priority = 1
		}
		candidates = append(candidates, operatorCandidate{
			Name:     entry.Name,
			Expected: entry.Expected,
			Priority: priority,
		})
	}
	return normalizeOperatorCandidates(candidates)
}

func buildRestoreCandidates(settlement settlementTradeSettlement, localeOrder map[string]int) []operatorCandidate {
	operators := collectOperatorBonusTypes(settlement, map[string]struct{}{
		"moneyProduceSpeed": {},
	})
	entries := sortedOperatorEntries(operators, localeOrder)
	candidates := make([]operatorCandidate, 0, len(entries))
	for index, entry := range entries {
		candidates = append(candidates, operatorCandidate{
			Name:     entry.Name,
			Expected: entry.Expected,
			Priority: index,
		})
	}
	return normalizeOperatorCandidates(candidates)
}

type operatorDataEntry struct {
	Name       string
	Expected   []string
	BonusTypes map[string]struct{}
}

func collectOperatorBonusTypes(settlement settlementTradeSettlement, accepted map[string]struct{}) map[string]operatorDataEntry {
	operators := map[string]operatorDataEntry{}
	for _, feature := range settlement.SettlementFeatures {
		matchedBonusTypes := make([]string, 0, len(feature.Bonuses))
		for _, bonus := range feature.Bonuses {
			if _, ok := accepted[bonus.Type]; ok {
				matchedBonusTypes = append(matchedBonusTypes, bonus.Type)
			}
		}
		if len(matchedBonusTypes) == 0 {
			continue
		}
		for _, operator := range feature.MatchingOperators {
			if isIgnoredOperator(operator) {
				continue
			}
			name := toPascalCase(firstNonEmpty(operator.Name["EN"], operator.CharID))
			expected := operatorExpectedNames(operator.Name)
			if name == "" || len(expected) == 0 {
				continue
			}
			entry := operators[name]
			if entry.Name == "" {
				entry = operatorDataEntry{
					Name:       name,
					Expected:   expected,
					BonusTypes: map[string]struct{}{},
				}
			}
			for _, bonusType := range matchedBonusTypes {
				entry.BonusTypes[bonusType] = struct{}{}
			}
			operators[name] = entry
		}
	}
	return operators
}

func sortedOperatorEntries(operators map[string]operatorDataEntry, localeOrder map[string]int) []operatorDataEntry {
	entries := make([]operatorDataEntry, 0, len(operators))
	for _, entry := range operators {
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		aOrder := operatorLocaleOrder(entries[i].Name, localeOrder)
		bOrder := operatorLocaleOrder(entries[j].Name, localeOrder)
		if aOrder != bOrder {
			return aOrder < bOrder
		}
		return entries[i].Name < entries[j].Name
	})
	return entries
}

func loadOperatorLocaleOrder() map[string]int {
	pattern := regexp.MustCompile(`"operator\.([^"]+)"\s*:`)
	for _, path := range []string{
		operatorLocaleOrderResourcePath,
		operatorLocaleOrderResourcePath2,
	} {
		content, err := readBytesFromRepoOrResource(path)
		if err == nil {
			return operatorLocaleOrderMap(pattern.FindAllSubmatch(content, -1))
		}
	}
	return nil
}

func readJsonFromRepoOrResource(relativePath string, out any) error {
	content, err := readBytesFromRepoOrResource(relativePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(content, out)
}

func readBytesFromRepoOrResource(relativePath string) ([]byte, error) {
	if content, err := os.ReadFile(filepath.Join(repoRootFromSource(), filepath.FromSlash(relativePath))); err == nil {
		return content, nil
	}
	return resource.ReadResource(relativePath)
}

func repoRootFromSource() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func operatorLocaleOrderMap(matches [][][]byte) map[string]int {
	order := map[string]int{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := string(match[1])
		if _, ok := order[name]; ok {
			continue
		}
		order[name] = len(order)
	}
	return order
}

func operatorLocaleOrder(name string, localeOrder map[string]int) int {
	if order, ok := localeOrder[name]; ok {
		return order
	}
	return int(^uint(0) >> 1)
}

func operatorExpectedNames(names map[string]string) []string {
	return uniqueNonEmptyStrings([]string{
		names["CN"],
		names["TC"],
		names["EN"],
		names["JP"],
		names["KR"],
	})
}

func isIgnoredOperator(operator settlementTradeOperator) bool {
	return operator.Name["EN"] == "Endministrator" || operator.Name["CN"] == "管理员"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func toPascalCase(value string) string {
	var parts []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		parts = append(parts, current.String())
		current.Reset()
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()

	var result strings.Builder
	for _, part := range parts {
		runes := []rune(part)
		if len(runes) == 0 {
			continue
		}
		result.WriteString(strings.ToUpper(string(runes[0])))
		if len(runes) > 1 {
			result.WriteString(string(runes[1:]))
		}
	}
	return result.String()
}
