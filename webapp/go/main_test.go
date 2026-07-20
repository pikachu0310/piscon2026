package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"
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
		var standard []IncomingCondition
		standardErr := json.Unmarshal([]byte(body), &standard)
		var fast []IncomingCondition
		fastErr := conditionJSON.Unmarshal([]byte(body), &fast)

		if (standardErr == nil) != (fastErr == nil) {
			t.Fatalf("error compatibility differs for %q: standard=%v fast=%v", body, standardErr, fastErr)
		}
		if standardErr == nil && !reflect.DeepEqual(standard, fast) {
			t.Fatalf("decoded value differs for %q: standard=%#v fast=%#v", body, standard, fast)
		}
	}
}

func TestCachedConditionLayoutAndFlags(t *testing.T) {
	if size := unsafe.Sizeof(CachedCondition{}); size != 16 {
		t.Fatalf("CachedCondition size = %d, want 16", size)
	}

	for bits, conditionString := range conditionStringByBits {
		parsed, ok := conditionBitsByString[conditionString]
		if !ok || int(parsed) != bits {
			t.Fatalf("condition round trip failed for bits=%d string=%q parsed=%d ok=%v", bits, conditionString, parsed, ok)
		}
		for _, sitting := range []bool{false, true} {
			flags := uint8(bits)
			if sitting {
				flags |= conditionFlagSitting
			}
			condition := CachedCondition{Flags: flags}
			if got := cachedConditionString(condition); got != conditionString {
				t.Fatalf("condition string = %q, want %q", got, conditionString)
			}
			if got := cachedConditionIsSitting(condition); got != sitting {
				t.Fatalf("sitting = %v, want %v", got, sitting)
			}
			wantLevel, err := calculateConditionLevel(conditionString)
			if err != nil {
				t.Fatal(err)
			}
			if got := cachedConditionLevel(condition); got != wantLevel {
				t.Fatalf("level = %q, want %q", got, wantLevel)
			}
		}
	}
}

func TestCompactGraphMatchesStringReference(t *testing.T) {
	conditions := make([]CachedCondition, 0, 16)
	for bits := 0; bits < 8; bits++ {
		conditions = append(conditions,
			CachedCondition{Flags: uint8(bits)},
			CachedCondition{Flags: uint8(bits) | conditionFlagSitting},
		)
	}

	got, err := calculateGraphDataPoint(conditions)
	if err != nil {
		t.Fatal(err)
	}
	want := referenceGraphDataPoint(conditions)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("graph mismatch: got=%#v want=%#v", got, want)
	}
}

func referenceGraphDataPoint(conditions []CachedCondition) GraphDataPoint {
	dirty, overweight, broken, sitting, rawScore := 0, 0, 0, 0, 0
	for _, condition := range conditions {
		conditionString := cachedConditionString(condition)
		dirty += strings.Count(conditionString, "is_dirty=true")
		overweight += strings.Count(conditionString, "is_overweight=true")
		broken += strings.Count(conditionString, "is_broken=true")
		bad := strings.Count(conditionString, "=true")
		switch {
		case bad == 3:
			rawScore += scoreConditionLevelCritical
		case bad > 0:
			rawScore += scoreConditionLevelWarning
		default:
			rawScore += scoreConditionLevelInfo
		}
		if cachedConditionIsSitting(condition) {
			sitting++
		}
	}
	n := len(conditions)
	return GraphDataPoint{
		Score: rawScore * 100 / 3 / n,
		Percentage: ConditionsPercentage{
			Sitting:      sitting * 100 / n,
			IsBroken:     broken * 100 / n,
			IsDirty:      dirty * 100 / n,
			IsOverweight: overweight * 100 / n,
		},
	}
}

func TestConditionStateGenerationKeepsMessagesTogether(t *testing.T) {
	oldState := newConditionState()
	newState := newConditionState()
	oldID := internConditionMessage(oldState, "old generation")
	newID := internConditionMessage(newState, "new generation")
	if oldID != newID {
		t.Fatalf("test requires colliding IDs: old=%d new=%d", oldID, newID)
	}
	oldCondition := CachedCondition{MessageID: oldID}
	if got := conditionMessage(oldState, oldCondition.MessageID); got != "old generation" {
		t.Fatalf("old state resolved %q", got)
	}
	if got := conditionMessage(newState, oldCondition.MessageID); got != "new generation" {
		t.Fatalf("new state resolved %q", got)
	}
}

func TestRegistrationRequestGateDrainsAndReopens(t *testing.T) {
	gate := newRegistrationRequestGate()
	gate.enter()

	drained := make(chan struct{})
	go func() {
		gate.closeAndDrain()
		close(drained)
	}()

	select {
	case <-drained:
		t.Fatal("gate drained while a request was active")
	case <-time.After(20 * time.Millisecond):
	}

	gate.leave()
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("gate did not finish draining")
	}

	entered := make(chan struct{})
	go func() {
		gate.enter()
		close(entered)
		gate.leave()
	}()
	select {
	case <-entered:
		t.Fatal("closed gate accepted a new request")
	case <-time.After(20 * time.Millisecond):
	}

	gate.open()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("opened gate did not accept a new request")
	}
}
