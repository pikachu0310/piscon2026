package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func useEmptyMetadataCache(t *testing.T) {
	t.Helper()
	isuMetadataCache.Lock()
	oldByUUID := isuMetadataCache.byUUID
	oldByUser := isuMetadataCache.byUser
	oldLoaded := isuMetadataCache.loaded
	isuMetadataCache.byUUID = make(map[string]CachedIsuMetadata)
	isuMetadataCache.byUser = make(map[string][]CachedIsuMetadata)
	isuMetadataCache.loaded = true
	isuMetadataCache.Unlock()
	t.Cleanup(func() {
		isuMetadataCache.Lock()
		isuMetadataCache.byUUID = oldByUUID
		isuMetadataCache.byUser = oldByUser
		isuMetadataCache.loaded = oldLoaded
		isuMetadataCache.Unlock()
	})
}

func TestConditionJSONMatchesStandardLibrary(t *testing.T) {
	tests := []string{
		`[{"is_sitting":true,"condition":"is_dirty=false,is_overweight=false,is_broken=false","message":"ok","timestamp":1620000000}]`,
		`[{"message":"escaped \"quote\" and \u65e5\u672c","timestamp":1620000001,"unknown":"ignored","condition":"is_dirty=true,is_overweight=false,is_broken=true","is_sitting":false}]`,
		`[]`,
		`{}`,
		`[{"timestamp":1.5}]`,
		`[{"message":"unterminated}]`,
	}

	for _, body := range tests {
		var standard []CachedCondition
		standardErr := json.Unmarshal([]byte(body), &standard)
		var fast []CachedCondition
		fastErr := conditionJSON.Unmarshal([]byte(body), &fast)

		if (standardErr == nil) != (fastErr == nil) {
			t.Fatalf("error compatibility differs for %q: standard=%v fast=%v", body, standardErr, fastErr)
		}
		if standardErr == nil && !reflect.DeepEqual(standard, fast) {
			t.Fatalf("decoded value differs for %q: standard=%#v fast=%#v", body, standard, fast)
		}
	}
}

func TestIsuMetadataCachePreservesOwnershipAndNewestFirst(t *testing.T) {
	useEmptyMetadataCache(t)
	cacheIsuMetadata(CachedIsuMetadata{
		ID: 10, JIAIsuUUID: "uuid-old", Name: "old", Character: "chair", JIAUserID: "user-a",
	})
	cacheIsuMetadata(CachedIsuMetadata{
		ID: 20, JIAIsuUUID: "uuid-new", Name: "new", Character: "chair", JIAUserID: "user-a",
	})

	if _, found, loaded := getCachedOwnedIsuMetadata("user-b", "uuid-new"); !loaded || found {
		t.Fatalf("another user's ISU must not be visible: loaded=%v found=%v", loaded, found)
	}
	metadata, found, loaded := getCachedOwnedIsuMetadata("user-a", "uuid-new")
	if !loaded || !found || metadata.Name != "new" {
		t.Fatalf("owned ISU lookup failed: loaded=%v found=%v metadata=%#v", loaded, found, metadata)
	}
	list, loaded := getCachedIsuList("user-a")
	if !loaded || len(list) != 2 || list[0].ID != 20 || list[1].ID != 10 {
		t.Fatalf("cached list is not newest first: loaded=%v list=%#v", loaded, list)
	}
}

func TestSnapshotIsuMetadataContainsEveryCharacter(t *testing.T) {
	useEmptyMetadataCache(t)
	cacheIsuMetadata(CachedIsuMetadata{
		ID: 1, JIAIsuUUID: "uuid-1", Name: "one", Character: "zeta", JIAUserID: "user-a",
	})
	cacheIsuMetadata(CachedIsuMetadata{
		ID: 2, JIAIsuUUID: "uuid-2", Name: "two", Character: "alpha", JIAUserID: "user-b",
	})
	cacheIsuMetadata(CachedIsuMetadata{
		ID: 3, JIAIsuUUID: "uuid-3", Name: "three", Character: "zeta", JIAUserID: "user-c",
	})

	rows, characters, loaded := snapshotIsuMetadata()
	if !loaded || len(rows) != 3 {
		t.Fatalf("metadata snapshot incomplete: loaded=%v rows=%#v", loaded, rows)
	}
	if want := []string{"alpha", "zeta"}; !reflect.DeepEqual(characters, want) {
		t.Fatalf("character snapshot differs: got=%#v want=%#v", characters, want)
	}
}
