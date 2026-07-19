package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
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

func TestParseIsuRegistration(t *testing.T) {
	tests := []struct {
		name        string
		withImage   bool
		wantImage   []byte
		wantDefault bool
	}{
		{name: "with image", withImage: true, wantImage: []byte("image-data")},
		{name: "default image", wantDefault: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			if err := writer.WriteField("jia_isu_uuid", "chair-1"); err != nil {
				t.Fatal(err)
			}
			if err := writer.WriteField("isu_name", "Chair One"); err != nil {
				t.Fatal(err)
			}
			if tt.withImage {
				part, err := writer.CreateFormFile("image", "chair.jpg")
				if err != nil {
					t.Fatal(err)
				}
				if _, err = part.Write(tt.wantImage); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}

			identityCalls := 0
			uuid, name, image, useDefault, err := parseIsuRegistration(
				multipart.NewReader(&body, writer.Boundary()),
				func(gotUUID, gotName string) error {
					identityCalls++
					if gotUUID != "chair-1" || gotName != "Chair One" {
						t.Fatalf("unexpected identity: %q %q", gotUUID, gotName)
					}
					return nil
				})
			if err != nil {
				t.Fatal(err)
			}
			if uuid != "chair-1" || name != "Chair One" || identityCalls != 1 {
				t.Fatalf("unexpected result: uuid=%q name=%q calls=%d", uuid, name, identityCalls)
			}
			if !bytes.Equal(image, tt.wantImage) || useDefault != tt.wantDefault {
				t.Fatalf("unexpected image result: image=%q default=%v", image, useDefault)
			}
		})
	}
}
