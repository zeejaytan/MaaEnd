package sellproduct

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestOperatorCacheReadWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SellProductOwnedOperators.json")
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	uid := "abc123"

	if err := writeOperatorCache(
		path,
		uid,
		[]string{"Wulfgard", "Ardelia", "Wulfgard", ""},
		now,
	); err != nil {
		t.Fatalf("writeOperatorCache: %v", err)
	}
	cache, err := readOperatorCache(path)
	if err != nil {
		t.Fatalf("readOperatorCache: %v", err)
	}
	if cache.SchemaVersion != operatorCacheSchemaVersion {
		t.Fatalf("schema version = %d, want %d", cache.SchemaVersion, operatorCacheSchemaVersion)
	}
	if cache.UpdatedAt != "2026-06-14T01:02:03Z" {
		t.Fatalf("updated_at = %q", cache.UpdatedAt)
	}
	if cache.UID != uid {
		t.Fatalf("uid = %q, want %q", cache.UID, uid)
	}
	want := []string{"Ardelia", "Wulfgard"}
	if !reflect.DeepEqual(cache.Operators, want) {
		t.Fatalf("operators = %#v, want %#v", cache.Operators, want)
	}
}

func TestDefaultOperatorCachePathUsesUID(t *testing.T) {
	tests := []struct {
		name string
		uid  string
		want string
	}{
		{
			name: "hashed uid",
			uid:  "abc123",
			want: filepath.Join("debug", "record", "SellProductOwnedOperators.abc123.json"),
		},
		{
			name: "empty uid",
			uid:  "",
			want: filepath.Join("debug", "record", "SellProductOwnedOperators.unknown.json"),
		},
		{
			name: "unsafe uid",
			uid:  "../uid value",
			want: filepath.Join("debug", "record", "SellProductOwnedOperators..._uid_value.json"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultOperatorCachePath(tt.uid); got != tt.want {
				t.Fatalf("defaultOperatorCachePath(%q) = %q, want %q", tt.uid, got, tt.want)
			}
		})
	}
}

func TestOperatorCacheMissingAndEmpty(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")
	cache, err := readOperatorCache(missing)
	if err != nil {
		t.Fatalf("missing cache should not error: %v", err)
	}
	if len(cache.Operators) != 0 {
		t.Fatalf("missing cache operators = %#v", cache.Operators)
	}

	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, nil, 0644); err != nil {
		t.Fatal(err)
	}
	cache, err = readOperatorCache(empty)
	if err != nil {
		t.Fatalf("empty cache should not error: %v", err)
	}
	if len(cache.Operators) != 0 {
		t.Fatalf("empty cache operators = %#v", cache.Operators)
	}
}

func TestNormalizeOperatorCandidates(t *testing.T) {
	got := normalizeOperatorCandidates([]operatorCandidate{
		{Name: "Beta", Expected: []string{"贝塔"}, Priority: 2},
		{Name: "", Expected: []string{"忽略"}, Priority: 0},
		{Name: "Alpha", Expected: []string{"阿尔法", "阿尔法", ""}, Priority: 1},
		{Name: "Beta", Expected: []string{"重复"}, Priority: 0},
	})
	want := []operatorCandidate{
		{Name: "Alpha", Expected: []string{"阿尔法"}, Priority: 1},
		{Name: "Beta", Expected: []string{"贝塔"}, Priority: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeOperatorCandidates = %#v, want %#v", got, want)
	}
}

func TestFilterOwnedCandidates(t *testing.T) {
	candidates := []operatorCandidate{
		{Name: "Both", Priority: 0},
		{Name: "Money", Priority: 1},
		{Name: "Exp", Priority: 2},
	}
	owned := operatorNameSet([]string{"Exp", "Both"})
	got := filterOwnedCandidates(candidates, owned)
	want := []operatorCandidate{
		{Name: "Both", Priority: 0},
		{Name: "Exp", Priority: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filterOwnedCandidates = %#v, want %#v", got, want)
	}
}

func TestOperatorCacheHasSnapshot(t *testing.T) {
	uid := "abc123"
	if operatorCacheHasSnapshot(operatorCacheFile{}, uid) {
		t.Fatal("empty cache should not be treated as a snapshot")
	}
	if !operatorCacheHasSnapshot(operatorCacheFile{SchemaVersion: operatorCacheSchemaVersion, UID: uid}, uid) {
		t.Fatal("versioned cache should be treated as a snapshot")
	}
	if !operatorCacheHasSnapshot(operatorCacheFile{Operators: []string{"Alpha"}, UID: uid}, uid) {
		t.Fatal("cache with operators should be treated as a snapshot")
	}
	if operatorCacheHasSnapshot(operatorCacheFile{Operators: []string{"Alpha"}, UID: "other"}, uid) {
		t.Fatal("cache for another uid should not be treated as a snapshot")
	}
}

func TestMergeOperatorCacheUpdatesOwnedOperators(t *testing.T) {
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	uid := "abc123"
	cache := operatorCacheFile{
		UID:       uid,
		Operators: []string{"Old", "Keep"},
	}
	got := mergeOperatorCache(
		cache,
		uid,
		[]operatorCandidate{{Name: "Old"}, {Name: "New"}},
		[]string{"New"},
		now,
	)
	if got.UID != uid {
		t.Fatalf("uid = %q, want %q", got.UID, uid)
	}
	if want := []string{"Keep", "New"}; !reflect.DeepEqual(got.Operators, want) {
		t.Fatalf("operators = %#v, want %#v", got.Operators, want)
	}
}

func TestMergeOperatorCacheDropsMismatchedUID(t *testing.T) {
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	uid := "abc123"
	cache := operatorCacheFile{
		UID:       "other",
		Operators: []string{"OtherAccount"},
	}
	got := mergeOperatorCache(
		cache,
		uid,
		[]operatorCandidate{{Name: "New"}},
		[]string{"New"},
		now,
	)
	if got.UID != uid {
		t.Fatalf("uid = %q, want %q", got.UID, uid)
	}
	if want := []string{"New"}; !reflect.DeepEqual(got.Operators, want) {
		t.Fatalf("operators = %#v, want %#v", got.Operators, want)
	}
}

func TestMergeObservedOperatorCacheOnlyAddsObservedOperators(t *testing.T) {
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	uid := "abc123"
	cache := operatorCacheFile{
		UID:       uid,
		Operators: []string{"Keep"},
	}
	got := mergeObservedOperatorCache(cache, uid, []string{"New", "Keep", ""}, now)
	if got.SchemaVersion != operatorCacheSchemaVersion {
		t.Fatalf("schema version = %d, want %d", got.SchemaVersion, operatorCacheSchemaVersion)
	}
	if got.UpdatedAt != "2026-06-14T01:02:03Z" {
		t.Fatalf("updated_at = %q", got.UpdatedAt)
	}
	if want := []string{"Keep", "New"}; !reflect.DeepEqual(got.Operators, want) {
		t.Fatalf("operators = %#v, want %#v", got.Operators, want)
	}
}

func TestMergeObservedOperatorCacheDropsMismatchedUID(t *testing.T) {
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	uid := "abc123"
	cache := operatorCacheFile{
		UID:       "other",
		Operators: []string{"OtherAccount"},
	}
	got := mergeObservedOperatorCache(cache, uid, []string{"New"}, now)
	if got.UID != uid {
		t.Fatalf("uid = %q, want %q", got.UID, uid)
	}
	if want := []string{"New"}; !reflect.DeepEqual(got.Operators, want) {
		t.Fatalf("operators = %#v, want %#v", got.Operators, want)
	}
}
