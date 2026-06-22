package sellproduct

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/captureuid"
)

const (
	operatorCacheFilePrefix    = "SellProductOwnedOperators"
	operatorCacheFileExt       = ".json"
	operatorCacheUnknownUID    = "unknown"
	operatorCacheSchemaVersion = 1
)

var resolveOperatorCachePathFunc = defaultOperatorCachePath

type operatorCacheFile struct {
	SchemaVersion int      `json:"schema_version"`
	UpdatedAt     string   `json:"updated_at"`
	UID           string   `json:"uid"`
	Operators     []string `json:"operators"`
}

func currentOperatorCacheUID() string {
	return normalizeOperatorCacheUID(captureuid.GetCachedUID())
}

func defaultOperatorCachePath(uid string) string {
	fileName := fmt.Sprintf("%s.%s%s", operatorCacheFilePrefix, normalizeOperatorCacheUID(uid), operatorCacheFileExt)
	return filepath.Join("debug", "record", fileName)
}

func readOperatorCache(path string) (operatorCacheFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return operatorCacheFile{}, nil
		}
		return operatorCacheFile{}, fmt.Errorf("read operator cache: %w", err)
	}
	if len(raw) == 0 {
		return operatorCacheFile{}, nil
	}

	var cache operatorCacheFile
	if err := json.Unmarshal(raw, &cache); err != nil {
		return operatorCacheFile{}, fmt.Errorf("parse operator cache: %w", err)
	}
	if strings.TrimSpace(cache.UID) != "" {
		cache.UID = normalizeOperatorCacheUID(cache.UID)
	}
	cache.Operators = uniqueNonEmptyStrings(cache.Operators)
	sort.Strings(cache.Operators)
	return cache, nil
}

func writeOperatorCache(path string, uid string, operators []string, now time.Time) error {
	operators = uniqueNonEmptyStrings(operators)
	sort.Strings(operators)

	cache := operatorCacheFile{
		SchemaVersion: operatorCacheSchemaVersion,
		UpdatedAt:     now.UTC().Format(time.RFC3339),
		UID:           normalizeOperatorCacheUID(uid),
		Operators:     operators,
	}
	return writeOperatorCacheFile(path, cache)
}

func writeOperatorCacheFile(path string, cache operatorCacheFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create operator cache dir: %w", err)
	}
	raw, err := json.MarshalIndent(cache, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal operator cache: %w", err)
	}
	raw = append(raw, '\n')
	if err := writeOperatorCacheAtomic(path, raw, 0644); err != nil {
		return fmt.Errorf("write operator cache: %w", err)
	}
	return nil
}

func mergeOperatorCache(
	cache operatorCacheFile,
	uid string,
	scanCandidates []operatorCandidate,
	owned []string,
	now time.Time,
) operatorCacheFile {
	uid = normalizeOperatorCacheUID(uid)
	operatorSet := operatorNameSet(operatorCacheOperatorsForUID(cache, uid))
	scanSet := operatorCandidateNameSet(scanCandidates)

	for name := range scanSet {
		delete(operatorSet, name)
	}
	for _, name := range owned {
		if _, ok := scanSet[name]; ok {
			operatorSet[name] = struct{}{}
		}
	}

	return operatorCacheFile{
		SchemaVersion: operatorCacheSchemaVersion,
		UpdatedAt:     now.UTC().Format(time.RFC3339),
		UID:           uid,
		Operators:     sortedSetValues(operatorSet),
	}
}

func mergeObservedOperatorCache(cache operatorCacheFile, uid string, observed []string, now time.Time) operatorCacheFile {
	uid = normalizeOperatorCacheUID(uid)
	operatorSet := operatorNameSet(operatorCacheOperatorsForUID(cache, uid))
	for _, name := range observed {
		if name == "" {
			continue
		}
		operatorSet[name] = struct{}{}
	}

	return operatorCacheFile{
		SchemaVersion: operatorCacheSchemaVersion,
		UpdatedAt:     now.UTC().Format(time.RFC3339),
		UID:           uid,
		Operators:     sortedSetValues(operatorSet),
	}
}

func operatorCacheHasSnapshot(cache operatorCacheFile, uid string) bool {
	uid = normalizeOperatorCacheUID(uid)
	if cache.UID != "" && normalizeOperatorCacheUID(cache.UID) != uid {
		return false
	}
	return cache.SchemaVersion > 0 || cache.UpdatedAt != "" || len(cache.Operators) > 0
}

func operatorCacheOperatorsForUID(cache operatorCacheFile, uid string) []string {
	uid = normalizeOperatorCacheUID(uid)
	if cache.UID != "" && normalizeOperatorCacheUID(cache.UID) != uid {
		return nil
	}
	return cache.Operators
}

func writeOperatorCacheAtomic(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func normalizeOperatorCacheUID(uid string) string {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return operatorCacheUnknownUID
	}

	var b strings.Builder
	b.Grow(len(uid))
	for _, r := range uid {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}

	normalized := b.String()
	if normalized == "" {
		return operatorCacheUnknownUID
	}
	return normalized
}

func operatorNameSet(names []string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}

func operatorCandidateNameSet(candidates []operatorCandidate) map[string]struct{} {
	set := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.Name == "" {
			continue
		}
		set[candidate.Name] = struct{}{}
	}
	return set
}

func sortedSetValues(set map[string]struct{}) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}
