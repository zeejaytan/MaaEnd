package sellproduct

import (
	"fmt"
	"image"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	operatorActionSwipeBeginX  = 620
	operatorActionSwipeTopY    = 285
	operatorActionSwipeBottomY = 520
	operatorActionSwipeDurMs   = 500
	operatorActionSwipeWaitMs  = 250
)

type ScanOwnedOperatorsAction struct{}

type SelectBestOperatorAction struct{}

var (
	_ maa.CustomActionRunner = (*ScanOwnedOperatorsAction)(nil)
	_ maa.CustomActionRunner = (*SelectBestOperatorAction)(nil)
)

func (a *ScanOwnedOperatorsAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", scanOwnedOperatorsActionName).Msg("got nil custom action arg")
		return false
	}
	p, err := parseOperatorActionParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Str("component", scanOwnedOperatorsActionName).Msg("invalid params")
		return false
	}
	selectionParam, err := resolveOperatorSelectionParam(p)
	if err != nil {
		log.Error().Err(err).Str("component", scanOwnedOperatorsActionName).Msg("operator data unavailable")
		return false
	}
	scanCandidates := collectScanCandidates(selectionParam)
	if len(scanCandidates) == 0 {
		log.Warn().Str("component", scanOwnedOperatorsActionName).Msg("scan candidates is empty")
		return false
	}

	owned, err := scanOwnedOperators(ctx, scanCandidates, p.ROI, p.MaxSwipes)
	if err != nil {
		log.Error().Err(err).Str("component", scanOwnedOperatorsActionName).Msg("scan failed")
		return false
	}
	uid := currentOperatorCacheUID()
	path := resolveOperatorCachePathFunc(uid)
	cache, err := readOperatorCache(path)
	if err != nil {
		log.Error().Err(err).Str("component", scanOwnedOperatorsActionName).Msg("cache read failed")
		return false
	}
	cache = mergeOperatorCache(cache, uid, scanCandidates, owned, time.Now())
	if err := writeOperatorCacheFile(path, cache); err != nil {
		log.Error().Err(err).Str("component", scanOwnedOperatorsActionName).Msg("cache write failed")
		return false
	}

	log.Info().
		Str("component", scanOwnedOperatorsActionName).
		Int("operators", len(owned)).
		Msg("owned operators cache refreshed")
	return true
}

func (a *SelectBestOperatorAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", selectBestOperatorActionName).Msg("got nil custom action arg")
		return false
	}
	p, err := parseOperatorActionParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorActionName).Msg("invalid params")
		return false
	}
	selectionParam, err := resolveOperatorSelectionParam(p)
	if err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorActionName).Msg("operator data unavailable")
		return false
	}
	owned, err := loadOrRefreshOwnedOperators(ctx, p, selectionParam)
	if err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorActionName).Msg("owned operators unavailable")
		return false
	}
	candidates := candidatesForCurrentSelection(selectionParam, owned)
	if len(candidates) == 0 {
		owned, err = refreshOwnedOperators(ctx, p, selectionParam)
		if err != nil {
			log.Error().Err(err).Str("component", selectBestOperatorActionName).Msg("owned operators refresh failed")
			return false
		}
		candidates = candidatesForCurrentSelection(selectionParam, owned)
		if len(candidates) == 0 {
			log.Warn().
				Str("component", selectBestOperatorActionName).
				Int("cache_operators", len(owned)).
				Str("location", p.Location).
				Msg("no owned candidate for this selection")
			return false
		}
	}

	scanCandidates := collectScanCandidates(selectionParam)
	selected, observed, err := selectBestOperator(ctx, candidates, scanCandidates, p.ROI, p.MaxSwipes)
	if err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorActionName).Msg("select failed")
		return false
	}
	owned, err = recordObservedOperators(observed)
	if err != nil {
		log.Error().Err(err).Str("component", selectBestOperatorActionName).Msg("cache update failed")
		return false
	}
	if selected == "" && len(observed) > 0 {
		candidates = candidatesForCurrentSelection(selectionParam, owned)
		selected, observed, err = selectBestOperator(ctx, candidates, scanCandidates, p.ROI, p.MaxSwipes)
		if err != nil {
			log.Error().Err(err).Str("component", selectBestOperatorActionName).Msg("select retry failed")
			return false
		}
		if _, err := recordObservedOperators(observed); err != nil {
			log.Error().Err(err).Str("component", selectBestOperatorActionName).Msg("cache retry update failed")
			return false
		}
	}
	if selected == "" {
		log.Warn().
			Str("component", selectBestOperatorActionName).
			Int("candidates", len(candidates)).
			Msg("owned candidates were not found in current operator list")
		return false
	}

	log.Info().
		Str("component", selectBestOperatorActionName).
		Str("operator", selected).
		Msg("best operator selected")
	return true
}

func resolveOperatorSelectionParam(p *operatorActionParam) (*operatorSelectionParam, error) {
	data, err := loadOperatorSelectionDataFunc()
	if err != nil {
		return nil, err
	}
	result := &operatorSelectionParam{
		Usage:    p.Usage,
		Location: p.Location,
	}
	switch p.Usage {
	case operatorActionUsageTarget:
		result.Candidates = normalizeOperatorCandidates(data.TargetCandidates[p.Location])
	case operatorActionUsageRestore:
		result.RestoreGroups = normalizeOperatorCandidateGroups(data.RestoreGroups)
	default:
		return nil, fmt.Errorf("invalid usage %q", p.Usage)
	}
	return result, nil
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

func loadOrRefreshOwnedOperators(
	ctx *maa.Context,
	p *operatorActionParam,
	selectionParam *operatorSelectionParam,
) (map[string]struct{}, error) {
	uid := currentOperatorCacheUID()
	path := resolveOperatorCachePathFunc(uid)
	cache, err := readOperatorCache(path)
	if err != nil {
		return nil, err
	}
	scanCandidates := collectScanCandidates(selectionParam)
	if len(scanCandidates) == 0 {
		return nil, fmt.Errorf("scan candidates is empty")
	}
	needsRefresh := p.Mode == operatorCacheModeRefresh || !operatorCacheHasSnapshot(cache, uid)
	if !needsRefresh {
		return operatorNameSet(operatorCacheOperatorsForUID(cache, uid)), nil
	}

	return refreshOwnedOperators(ctx, p, selectionParam)
}

func refreshOwnedOperators(
	ctx *maa.Context,
	p *operatorActionParam,
	selectionParam *operatorSelectionParam,
) (map[string]struct{}, error) {
	uid := currentOperatorCacheUID()
	path := resolveOperatorCachePathFunc(uid)
	cache, err := readOperatorCache(path)
	if err != nil {
		return nil, err
	}
	scanCandidates := collectScanCandidates(selectionParam)
	if len(scanCandidates) == 0 {
		return nil, fmt.Errorf("scan candidates is empty")
	}
	owned, err := scanOwnedOperators(ctx, scanCandidates, p.ROI, p.MaxSwipes)
	if err != nil {
		return nil, err
	}
	cache = mergeOperatorCache(cache, uid, scanCandidates, owned, time.Now())
	if err := writeOperatorCacheFile(path, cache); err != nil {
		return nil, err
	}
	return operatorNameSet(cache.Operators), nil
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
	return operatorNameSet(cache.Operators), nil
}

func scanOwnedOperators(ctx *maa.Context, candidates []operatorCandidate, roi []int, maxSwipes int) ([]string, error) {
	ctrl, adaptor, err := operatorController(ctx)
	if err != nil {
		return nil, err
	}

	resetOperatorListToTop(adaptor, maxSwipes)
	ownedSet := make(map[string]struct{}, len(candidates))
	for page := 0; page <= maxSwipes; page++ {
		img, err := operatorScreencap(ctrl)
		if err != nil {
			return nil, err
		}
		items, err := recognizeOperatorList(ctx, img, roi)
		if err != nil {
			return nil, err
		}
		for _, candidate := range candidates {
			if _, ok := ownedSet[candidate.Name]; ok {
				continue
			}
			if findBestMatch(items, candidate.Expected) != nil {
				ownedSet[candidate.Name] = struct{}{}
			}
		}
		if page < maxSwipes {
			swipeOperatorListNext(adaptor)
		}
	}

	owned := make([]string, 0, len(ownedSet))
	for name := range ownedSet {
		owned = append(owned, name)
	}
	return uniqueNonEmptyStrings(owned), nil
}

func selectBestOperator(
	ctx *maa.Context,
	candidates []operatorCandidate,
	discoveryCandidates []operatorCandidate,
	roi []int,
	maxSwipes int,
) (string, []string, error) {
	ctrl, adaptor, err := operatorController(ctx)
	if err != nil {
		return "", nil, err
	}
	observedSet := map[string]struct{}{}

	for _, candidate := range candidates {
		resetOperatorListToTop(adaptor, maxSwipes)
		for page := 0; page <= maxSwipes; page++ {
			img, err := operatorScreencap(ctrl)
			if err != nil {
				return "", nil, err
			}
			items, err := recognizeOperatorList(ctx, img, roi)
			if err != nil {
				return "", nil, err
			}
			for _, discoveryCandidate := range discoveryCandidates {
				if _, ok := observedSet[discoveryCandidate.Name]; ok {
					continue
				}
				if findBestMatch(items, discoveryCandidate.Expected) != nil {
					observedSet[discoveryCandidate.Name] = struct{}{}
				}
			}
			match := findBestMatch(items, candidate.Expected)
			if match != nil {
				clickOperatorBox(adaptor, match.box)
				observedSet[candidate.Name] = struct{}{}
				return candidate.Name, sortedSetValues(observedSet), nil
			}
			if page < maxSwipes {
				swipeOperatorListNext(adaptor)
			}
		}
	}
	return "", sortedSetValues(observedSet), nil
}

func operatorController(ctx *maa.Context) (*maa.Controller, control.ControlAdaptor, error) {
	if ctx == nil || ctx.GetTasker() == nil || ctx.GetTasker().GetController() == nil {
		return nil, nil, fmt.Errorf("nil context, tasker, or controller")
	}
	ctrl := ctx.GetTasker().GetController()
	adaptor, err := control.NewControlAdaptor(ctx, ctrl, 1280, 720)
	if err != nil {
		return nil, nil, err
	}
	return ctrl, adaptor, nil
}

func operatorScreencap(ctrl *maa.Controller) (image.Image, error) {
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		return nil, err
	}
	if img == nil {
		return nil, fmt.Errorf("cached image is nil")
	}
	return img, nil
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

func resetOperatorListToTop(adaptor control.ControlAdaptor, maxSwipes int) {
	for i := 0; i < maxSwipes; i++ {
		adaptor.Swipe(
			0,
			operatorActionSwipeBeginX,
			operatorActionSwipeTopY,
			0,
			operatorActionSwipeBottomY-operatorActionSwipeTopY,
			operatorActionSwipeDurMs,
			operatorActionSwipeWaitMs,
		)
	}
}

func swipeOperatorListNext(adaptor control.ControlAdaptor) {
	adaptor.Swipe(
		0,
		operatorActionSwipeBeginX,
		operatorActionSwipeBottomY,
		0,
		operatorActionSwipeTopY-operatorActionSwipeBottomY,
		operatorActionSwipeDurMs,
		operatorActionSwipeWaitMs,
	)
}

func clickOperatorBox(adaptor control.ControlAdaptor, box maa.Rect) {
	x := box.X() + box.Width()/2
	y := box.Y() + box.Height()/2
	adaptor.TouchClick(0, x, y, 80, 150)
}
