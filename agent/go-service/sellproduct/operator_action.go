package sellproduct

import (
	"fmt"
	"image"
	"sort"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type SelectBestOperatorRecognition struct{}

type CurrentBestOperatorRecognition struct{}

type OperatorCacheReadyRecognition struct{}

type OperatorListBottomRecognition struct{}

var _ maa.CustomRecognitionRunner = (*SelectBestOperatorRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*CurrentBestOperatorRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*OperatorCacheReadyRecognition)(nil)
var _ maa.CustomRecognitionRunner = (*OperatorListBottomRecognition)(nil)

func (r *SelectBestOperatorRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().Str("component", selectBestOperatorRecognitionName).Msg("got nil custom recognition arg")
		return nil, false
	}
	p, err := parseOperatorActionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorRecognitionName).Msg("invalid params")
		return nil, false
	}
	selectionParam, err := resolveOperatorSelectionParam(p)
	if err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorRecognitionName).Msg("operator data unavailable")
		return nil, false
	}
	owned, err := loadOwnedOperatorsForSelection(p)
	if err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorRecognitionName).Msg("owned operators unavailable")
		return nil, false
	}
	candidates := candidatesForCurrentSelection(selectionParam, owned)
	if len(candidates) == 0 {
		return nil, false
	}

	items, err := recognizeOperatorList(ctx, arg.Img, p.ROI)
	if err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorRecognitionName).Msg("recognize operator list failed")
		return nil, false
	}
	candidate, match, ok := findBestVisibleOperator(candidates, items)
	if !ok {
		return nil, false
	}
	if _, err := recordObservedOperators([]string{operatorCandidateCacheName(candidate)}); err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorRecognitionName).Msg("cache update failed")
		return nil, false
	}
	if p.Mode == operatorCacheModeRefresh {
		delete(operatorListScanStates, operatorListScanStateKey(p))
	}
	return &maa.CustomRecognitionResult{
		Box:    match.box,
		Detail: fmt.Sprintf("%s:%s", match.ocrText, candidate.Name),
	}, true
}

func (r *CurrentBestOperatorRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().Str("component", currentBestOperatorRecognitionName).Msg("got nil custom recognition arg")
		return nil, false
	}
	p, err := parseOperatorActionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", currentBestOperatorRecognitionName).Msg("invalid params")
		return nil, false
	}
	selectionParam, err := resolveOperatorSelectionParam(p)
	if err != nil {
		log.Error().Err(err).Str("component", currentBestOperatorRecognitionName).Msg("operator data unavailable")
		return nil, false
	}
	owned, err := loadOwnedOperatorsForSelection(p)
	if err != nil {
		log.Error().Err(err).Str("component", currentBestOperatorRecognitionName).Msg("owned operators unavailable")
		return nil, false
	}
	candidates := candidatesForCurrentSelection(selectionParam, owned)
	if len(candidates) == 0 {
		return nil, false
	}

	items, err := recognizeOperatorList(ctx, arg.Img, p.ROI)
	if err != nil {
		log.Error().Err(err).Str("component", currentBestOperatorRecognitionName).Msg("recognize current operator failed")
		return nil, false
	}
	candidate, match, ok := findCurrentBestOperator(candidates, items)
	if !ok {
		return nil, false
	}
	if _, err := recordObservedOperators([]string{operatorCandidateCacheName(candidate)}); err != nil {
		log.Error().Err(err).Str("component", currentBestOperatorRecognitionName).Msg("cache update failed")
		return nil, false
	}
	if p.Mode == operatorCacheModeRefresh {
		delete(operatorListScanStates, operatorListScanStateKey(p))
	}
	return &maa.CustomRecognitionResult{
		Box:    match.box,
		Detail: fmt.Sprintf("%s:%s", match.ocrText, candidate.Name),
	}, true
}

func (r *OperatorCacheReadyRecognition) Run(
	_ *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().Str("component", operatorCacheReadyRecognitionName).Msg("got nil custom recognition arg")
		return nil, false
	}
	p, err := parseOperatorActionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", operatorCacheReadyRecognitionName).Msg("invalid params")
		return nil, false
	}
	if operatorCacheReadyForSelection(p) {
		return &maa.CustomRecognitionResult{Detail: "cache_ready"}, true
	}
	return nil, false
}

func (r *OperatorListBottomRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if arg == nil {
		log.Error().Str("component", operatorListBottomRecognitionName).Msg("got nil custom recognition arg")
		return nil, false
	}
	p, err := parseOperatorActionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", operatorListBottomRecognitionName).Msg("invalid params")
		return nil, false
	}
	selectionParam, err := resolveOperatorSelectionParam(p)
	if err != nil {
		log.Error().Err(err).Str("component", operatorListBottomRecognitionName).Msg("operator data unavailable")
		return nil, false
	}
	scanCandidates := collectScanCandidates(selectionParam)
	items, err := recognizeOperatorList(ctx, arg.Img, p.ROI)
	if err != nil {
		log.Error().Err(err).Str("component", operatorListBottomRecognitionName).Msg("recognize operator list failed")
		return nil, false
	}
	observed := observedOperatorCacheNames(items, scanCandidates)
	state := operatorListStateFor(p)
	state.Observed = append(state.Observed, observed...)
	signature := operatorListSignature(items)
	reachedBottom := operatorListReachedBottom(state.PreviousSignature, signature)
	if !reachedBottom {
		state.PreviousSignature = signature
		operatorListScanStates[state.Key] = state
		return nil, false
	}
	scanMode := p.Mode == operatorCacheModeRefresh || !state.CacheReadyAtStart
	if scanMode {
		if err := replaceObservedOperators(p, scanCandidates, state.Observed); err != nil {
			log.Error().Err(err).Str("component", operatorListBottomRecognitionName).Msg("cache refresh failed")
			return nil, false
		}
	} else {
		if _, err := recordObservedOperators(state.Observed); err != nil {
			log.Error().Err(err).Str("component", operatorListBottomRecognitionName).Msg("cache update failed")
			return nil, false
		}
		delete(operatorListScanStates, state.Key)
	}
	if shouldHitOperatorListBottomResult(p, state.CacheReadyAtStart) {
		return &maa.CustomRecognitionResult{Detail: p.Result}, true
	}
	return nil, false
}

func resolveOperatorSelectionParam(p *operatorActionParam) (*operatorSelectionParam, error) {
	data, err := loadOperatorSelectionDataFunc()
	if err != nil {
		return nil, err
	}
	result := &operatorSelectionParam{
		Usage:          p.Usage,
		Location:       p.Location,
		ScanCandidates: allOperatorScanCandidates(data),
	}
	switch p.Usage {
	case operatorActionUsageTarget:
		result.Candidates = normalizeOperatorCandidates(data.TargetCandidates[p.Location])
	case operatorActionUsageRestore:
		result.RestoreGroups = normalizeOperatorCandidateGroups(data.RestoreGroups)
	case operatorActionUsageAll:
	default:
		return nil, fmt.Errorf("invalid usage %q", p.Usage)
	}
	return result, nil
}

func allOperatorScanCandidates(data *operatorSelectionData) []operatorCandidate {
	if data == nil {
		return nil
	}
	var candidates []operatorCandidate
	for _, targetCandidates := range data.TargetCandidates {
		candidates = append(candidates, targetCandidates...)
	}
	for _, group := range data.RestoreGroups {
		candidates = append(candidates, group.Candidates...)
	}
	return normalizeOperatorCandidates(candidates)
}

func candidatesForCurrentSelection(p *operatorSelectionParam, owned map[string]struct{}) []operatorCandidate {
	if p.Usage == operatorActionUsageTarget {
		return filterOwnedCandidates(p.Candidates, owned)
	}
	if p.Usage != operatorActionUsageRestore {
		return nil
	}
	plan := buildRestoreAssignmentPlan(p.RestoreGroups, owned)
	candidate, ok := plan.Assignments[p.Location]
	if !ok {
		return nil
	}
	return []operatorCandidate{candidate}
}

type operatorListScanState struct {
	Key               string
	PreviousSignature string
	Observed          []string
	CacheReadyAtStart bool
}

var operatorListScanStates = map[string]operatorListScanState{}

func loadOwnedOperatorsForSelection(p *operatorActionParam) (map[string]struct{}, error) {
	uid := currentOperatorCacheUID()
	path := resolveOperatorCachePathFunc(uid)
	cache, err := readOperatorCache(path)
	if err != nil {
		return nil, err
	}
	if p.Mode == operatorCacheModeRefresh && !operatorListScanComplete(p) {
		return nil, nil
	}
	if !operatorCacheHasSnapshot(cache, uid) {
		return nil, nil
	}
	return operatorNameSet(operatorCacheOperatorsForUID(cache, uid)), nil
}

func operatorCacheReadyForSelection(p *operatorActionParam) bool {
	if p.Mode == operatorCacheModeRefresh {
		return operatorListScanComplete(p)
	}
	uid := currentOperatorCacheUID()
	path := resolveOperatorCachePathFunc(uid)
	cache, err := readOperatorCache(path)
	return err == nil && operatorCacheHasSnapshot(cache, uid)
}

func recordObservedOperators(observed []string) (map[string]struct{}, error) {
	uid := currentOperatorCacheUID()
	path := resolveOperatorCachePathFunc(uid)
	cache, err := readOperatorCache(path)
	if err != nil {
		return nil, err
	}
	if len(observed) > 0 {
		cache = mergeObservedOperatorCache(cache, uid, observed, time.Now())
		if err := writeOperatorCacheFile(path, cache); err != nil {
			return nil, err
		}
	}
	return operatorNameSet(operatorCacheOperatorsForUID(cache, uid)), nil
}

func replaceObservedOperators(p *operatorActionParam, scanCandidates []operatorCandidate, observed []string) error {
	uid := currentOperatorCacheUID()
	path := resolveOperatorCachePathFunc(uid)
	cache, err := readOperatorCache(path)
	if err != nil {
		return err
	}
	cache = mergeOperatorCache(cache, uid, scanCandidates, observed, time.Now())
	if err := writeOperatorCacheFile(path, cache); err != nil {
		return err
	}
	operatorListMarkScanComplete(p)
	return nil
}

func observedOperatorCacheNames(items []ocrItem, candidates []operatorCandidate) []string {
	observedSet := map[string]struct{}{}
	for _, candidate := range candidates {
		if findBestMatch(items, candidate.Expected) != nil {
			observedSet[operatorCandidateCacheName(candidate)] = struct{}{}
		}
	}
	return sortedSetValues(observedSet)
}

func operatorListSignature(items []ocrItem) string {
	if len(items) == 0 {
		return ""
	}
	sortedItems := make([]ocrItem, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(item.text)
		if text == "" {
			continue
		}
		item.text = text
		sortedItems = append(sortedItems, item)
	}
	sort.SliceStable(sortedItems, func(i, j int) bool {
		if sortedItems[i].box.Y() != sortedItems[j].box.Y() {
			return sortedItems[i].box.Y() < sortedItems[j].box.Y()
		}
		if sortedItems[i].box.X() != sortedItems[j].box.X() {
			return sortedItems[i].box.X() < sortedItems[j].box.X()
		}
		return sortedItems[i].text < sortedItems[j].text
	})

	var b strings.Builder
	for _, item := range sortedItems {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(item.text)
	}
	return b.String()
}

func operatorListReachedBottom(previousSignature string, currentSignature string) bool {
	return previousSignature != "" && previousSignature == currentSignature
}

func findBestVisibleOperator(candidates []operatorCandidate, items []ocrItem) (operatorCandidate, *matchResult, bool) {
	for _, candidate := range candidates {
		match := findBestMatch(items, candidate.Expected)
		if match != nil {
			return candidate, match, true
		}
	}
	return operatorCandidate{}, nil, false
}

func findCurrentBestOperator(candidates []operatorCandidate, items []ocrItem) (operatorCandidate, *matchResult, bool) {
	if len(candidates) == 0 {
		return operatorCandidate{}, nil, false
	}
	candidate := candidates[0]
	match := findBestMatch(items, candidate.Expected)
	if match == nil {
		return operatorCandidate{}, nil, false
	}
	return candidate, match, true
}

func recognizeOperatorList(ctx *maa.Context, img image.Image, roi []int) ([]ocrItem, error) {
	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeOCR,
		maa.OCRParam{ROI: maa.NewTargetRect(maa.Rect{roi[0], roi[1], roi[2], roi[3]})},
		img,
	)
	if err != nil {
		return nil, err
	}
	return collectOCRResults(detail), nil
}

func operatorListStateFor(p *operatorActionParam) operatorListScanState {
	key := operatorListScanStateKey(p)
	if state, ok := operatorListScanStates[key]; ok {
		return state
	}
	uid := currentOperatorCacheUID()
	path := resolveOperatorCachePathFunc(uid)
	cache, err := readOperatorCache(path)
	cacheReady := err == nil && p.Mode != operatorCacheModeRefresh && operatorCacheHasSnapshot(cache, uid)
	return operatorListScanState{
		Key:               key,
		CacheReadyAtStart: cacheReady,
	}
}

func shouldHitOperatorListBottomResult(p *operatorActionParam, cacheReadyAtStart bool) bool {
	switch p.Result {
	case operatorListBottomResultScanDone:
		return p.Mode == operatorCacheModeRefresh || !cacheReadyAtStart
	case operatorListBottomResultNotFound:
		return cacheReadyAtStart
	default:
		return true
	}
}

func operatorListScanStateKey(p *operatorActionParam) string {
	return strings.Join([]string{
		currentOperatorCacheUID(),
		p.Mode,
		p.Usage,
		p.Location,
	}, "|")
}

func operatorListMarkScanComplete(p *operatorActionParam) {
	key := operatorListScanCompleteKey()
	operatorListScanStates[key] = operatorListScanState{
		Key:               key,
		CacheReadyAtStart: true,
	}
}

func operatorListScanComplete(_ *operatorActionParam) bool {
	state, ok := operatorListScanStates[operatorListScanCompleteKey()]
	return ok && state.CacheReadyAtStart
}

func operatorListScanCompleteKey() string {
	return strings.Join([]string{
		currentOperatorCacheUID(),
		operatorCacheModeRefresh,
		"operator-cache",
	}, "|")
}
