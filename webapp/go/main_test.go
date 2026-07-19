package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

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

func TestConditionJSONDoesNotAliasInput(t *testing.T) {
	body := []byte(`[{"message":"stable","timestamp":1620000001,"condition":"is_dirty=false,is_overweight=false,is_broken=false","is_sitting":true}]`)
	var conditions []CachedCondition
	if err := conditionJSON.Unmarshal(body, &conditions); err != nil {
		t.Fatal(err)
	}
	for i := range body {
		body[i] = 'x'
	}
	if got := conditions[0].Message; got != "stable" {
		t.Fatalf("decoded string aliases reusable request buffer: got %q", got)
	}
}
