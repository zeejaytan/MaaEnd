package sellproduct

import (
	"path/filepath"
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestOperatorListSignatureIgnoresOCRResultOrder(t *testing.T) {
	a := []ocrItem{
		{text: "陈千语", box: maa.Rect{300, 200, 80, 20}},
		{text: "佩丽卡", box: maa.Rect{100, 100, 80, 20}},
	}
	b := []ocrItem{
		{text: "佩丽卡", box: maa.Rect{100, 100, 80, 20}},
		{text: "陈千语", box: maa.Rect{300, 200, 80, 20}},
	}

	if got, want := operatorListSignature(a), operatorListSignature(b); got != want {
		t.Fatalf("signature mismatch: got %q, want %q", got, want)
	}
}

func TestOperatorListReachedBottomWhenSignatureUnchanged(t *testing.T) {
	previous := operatorListSignature([]ocrItem{
		{text: "佩丽卡", box: maa.Rect{100, 100, 80, 20}},
	})
	same := operatorListSignature([]ocrItem{
		{text: "佩丽卡", box: maa.Rect{100, 100, 80, 20}},
	})
	changed := operatorListSignature([]ocrItem{
		{text: "陈千语", box: maa.Rect{100, 100, 80, 20}},
	})

	if !operatorListReachedBottom(previous, same) {
		t.Fatal("unchanged operator list signature should mean bottom reached")
	}
	if operatorListReachedBottom(previous, changed) {
		t.Fatal("changed operator list signature should not mean bottom reached")
	}
	if operatorListReachedBottom("", same) {
		t.Fatal("empty previous signature should not mean bottom reached")
	}
}

func TestFindBestVisibleOperatorUsesCandidatePriority(t *testing.T) {
	candidates := []operatorCandidate{
		{Name: "Best", CacheName: "最优", Expected: []string{"最优"}, Priority: 0},
		{Name: "Fallback", CacheName: "备选", Expected: []string{"备选"}, Priority: 1},
	}
	items := []ocrItem{
		{text: "备选", box: maa.Rect{100, 100, 80, 20}},
		{text: "最优", box: maa.Rect{100, 200, 80, 20}},
	}

	candidate, match, ok := findBestVisibleOperator(candidates, items)
	if !ok {
		t.Fatal("expected visible operator match")
	}
	if candidate.Name != "Best" {
		t.Fatalf("candidate = %q, want Best", candidate.Name)
	}
	if match.ocrText != "最优" {
		t.Fatalf("ocr text = %q, want 最优", match.ocrText)
	}
}

func TestFindCurrentBestOperatorRequiresTopPriorityCandidate(t *testing.T) {
	candidates := []operatorCandidate{
		{Name: "Best", CacheName: "最优", Expected: []string{"最优"}, Priority: 0},
		{Name: "Fallback", CacheName: "备选", Expected: []string{"备选"}, Priority: 1},
	}
	fallbackItems := []ocrItem{
		{text: "备选", box: maa.Rect{100, 100, 80, 20}},
	}
	if _, _, ok := findCurrentBestOperator(candidates, fallbackItems); ok {
		t.Fatal("fallback candidate should not be treated as the current best operator")
	}

	bestItems := []ocrItem{
		{text: "最优", box: maa.Rect{100, 100, 80, 20}},
	}
	candidate, match, ok := findCurrentBestOperator(candidates, bestItems)
	if !ok {
		t.Fatal("expected current best operator match")
	}
	if candidate.Name != "Best" {
		t.Fatalf("candidate = %q, want Best", candidate.Name)
	}
	if match.ocrText != "最优" {
		t.Fatalf("ocr text = %q, want 最优", match.ocrText)
	}
}

func TestAllOperatorScanCandidatesIncludesTargetAndRestoreCandidates(t *testing.T) {
	data := &operatorSelectionData{
		TargetCandidates: map[string][]operatorCandidate{
			"A": {{Name: "Perlica", CacheName: "佩丽卡", Expected: []string{"佩丽卡"}, Priority: 2}},
			"B": {{Name: "Avywenna", CacheName: "陈千语", Expected: []string{"陈千语"}, Priority: 1}},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location: "A",
				Candidates: []operatorCandidate{
					{Name: "Restore", CacheName: "恢复干员", Expected: []string{"恢复干员"}, Priority: 3},
				},
			},
		},
	}

	got := allOperatorScanCandidates(data)
	want := []string{"陈千语", "佩丽卡", "恢复干员"}
	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i, candidate := range got {
		if candidate.CacheName != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q", i, candidate.CacheName, want[i])
		}
	}
}

func TestOperatorCacheReadyForSelectionUsesCacheModeSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SellProductOwnedOperators.json")
	previousPathFunc := resolveOperatorCachePathFunc
	previousStates := operatorListScanStates
	resolveOperatorCachePathFunc = func(string) string { return path }
	operatorListScanStates = map[string]operatorListScanState{}
	t.Cleanup(func() {
		resolveOperatorCachePathFunc = previousPathFunc
		operatorListScanStates = previousStates
	})

	p := &operatorActionParam{
		Mode:     operatorCacheModeCache,
		Usage:    operatorActionUsageTarget,
		Location: "TestLocation",
	}
	if operatorCacheReadyForSelection(p) {
		t.Fatal("missing cache should not be ready")
	}

	if err := writeOperatorCache(path, operatorCacheUnknownUID, []string{"佩丽卡"}, time.Now()); err != nil {
		t.Fatalf("writeOperatorCache: %v", err)
	}
	if !operatorCacheReadyForSelection(p) {
		t.Fatal("cache mode should be ready when current uid has a snapshot")
	}
}

func TestOperatorCacheReadyForSelectionRefreshModeWaitsForScanComplete(t *testing.T) {
	previousStates := operatorListScanStates
	operatorListScanStates = map[string]operatorListScanState{}
	t.Cleanup(func() {
		operatorListScanStates = previousStates
	})

	p := &operatorActionParam{
		Mode:     operatorCacheModeRefresh,
		Usage:    operatorActionUsageTarget,
		Location: "TestLocation",
	}
	if operatorCacheReadyForSelection(p) {
		t.Fatal("refresh mode should not be ready before scan completion")
	}
	operatorListMarkScanComplete(p)
	if !operatorCacheReadyForSelection(p) {
		t.Fatal("refresh mode should be ready after scan completion")
	}
}

func TestOperatorCacheReadyForSelectionRefreshModeUsesGlobalScanCompletion(t *testing.T) {
	previousStates := operatorListScanStates
	operatorListScanStates = map[string]operatorListScanState{}
	t.Cleanup(func() {
		operatorListScanStates = previousStates
	})

	globalScan := &operatorActionParam{
		Mode:     operatorCacheModeRefresh,
		Usage:    operatorActionUsageAll,
		Location: "global",
	}
	targetSelection := &operatorActionParam{
		Mode:     operatorCacheModeRefresh,
		Usage:    operatorActionUsageTarget,
		Location: "SkyKingFlats",
	}
	operatorListMarkScanComplete(globalScan)
	if !operatorCacheReadyForSelection(targetSelection) {
		t.Fatal("refresh mode selection should reuse the global operator scan completion")
	}
}

func TestParseOperatorActionParamAllowsGlobalScanUsage(t *testing.T) {
	got, err := parseOperatorActionParam(`{"mode":"cache","usage":"all","location":"global"}`)
	if err != nil {
		t.Fatalf("parseOperatorActionParam: %v", err)
	}
	if got.Usage != operatorActionUsageAll {
		t.Fatalf("usage = %q, want %q", got.Usage, operatorActionUsageAll)
	}
}

func TestOperatorListBottomNotFoundCanHitAfterRefreshScan(t *testing.T) {
	p := &operatorActionParam{
		Mode:   operatorCacheModeRefresh,
		Result: operatorListBottomResultNotFound,
	}
	if !shouldHitOperatorListBottomResult(p, true) {
		t.Fatal("not_found should hit after refresh scan has prepared the cache")
	}
	if shouldHitOperatorListBottomResult(p, false) {
		t.Fatal("not_found should not hit before cache is prepared")
	}
}
