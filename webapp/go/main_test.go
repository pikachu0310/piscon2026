package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"reflect"
	"strings"
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

func TestReadConditionRequestBody(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		contentLength int64
		wantErr       bool
	}{
		{name: "known length", body: `[{"timestamp":1}]`, contentLength: int64(len(`[{"timestamp":1}]`))},
		{name: "premature EOF", body: `short`, contentLength: 10, wantErr: true},
		{name: "unknown length", body: `[{"timestamp":2}]`, contentLength: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{
				Body:          ioutil.NopCloser(strings.NewReader(tt.body)),
				ContentLength: tt.contentLength,
			}
			got, err := readConditionRequestBody(r)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && string(got) != tt.body {
				t.Fatalf("body = %q, want %q", got, tt.body)
			}
		})
	}
}
