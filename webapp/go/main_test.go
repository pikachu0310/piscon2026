package main

import (
	"encoding/json"
	"fmt"
	"net/http"
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

func TestForwardedConditionCodecRoundTrip(t *testing.T) {
	wantUUID := "01234567-89ab-cdef-0123-456789abcdef"
	wantConditions := []ForwardedCondition{
		{Timestamp: -1, Message: "", Flags: 0},
		{Timestamp: 1620000000, Message: "日本語とemoji 🪑", Flags: 15},
		{Timestamp: 1620000001, Message: "invalid marker", Flags: 0xff},
	}
	body, err := encodeForwardedConditions(wantUUID, wantConditions)
	if err != nil {
		t.Fatal(err)
	}
	gotUUID, gotConditions, err := decodeForwardedConditions(body)
	if err != nil {
		t.Fatal(err)
	}
	if gotUUID != wantUUID || !reflect.DeepEqual(gotConditions, wantConditions) {
		t.Fatalf("round trip mismatch: uuid=%q conditions=%#v", gotUUID, gotConditions)
	}
}

func TestForwardedConditionCodecRejectsCorruption(t *testing.T) {
	body, err := encodeForwardedConditions("uuid", []ForwardedCondition{{Timestamp: 1, Message: "message", Flags: 3}})
	if err != nil {
		t.Fatal(err)
	}
	tests := [][]byte{
		nil,
		[]byte("ICD0"),
		body[:len(body)-1],
		append(append([]byte{}, body...), 0),
	}
	for index, test := range tests {
		if _, _, decodeErr := decodeForwardedConditions(test); decodeErr == nil {
			t.Fatalf("corrupt case %d was accepted", index)
		}
	}
}

func TestForwardedConditionBatchCodecRoundTrip(t *testing.T) {
	requests := []*conditionForwardRequest{
		{jiaIsuUUID: "uuid-1", conditions: []ForwardedCondition{{Timestamp: 1, Message: "one", Flags: 1}}},
		{jiaIsuUUID: "uuid-2", conditions: []ForwardedCondition{{Timestamp: 2, Message: "二", Flags: 10}, {Timestamp: 3, Message: "", Flags: 0xff}}},
	}
	body, err := encodeForwardedConditionBatch(requests)
	if err != nil {
		t.Fatal(err)
	}
	uuids, conditions, err := decodeForwardedConditionBatch(body)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(uuids, []string{"uuid-1", "uuid-2"}) {
		t.Fatalf("UUIDs = %#v", uuids)
	}
	for index := range requests {
		if !reflect.DeepEqual(conditions[index], requests[index].conditions) {
			t.Fatalf("conditions[%d] = %#v, want %#v", index, conditions[index], requests[index].conditions)
		}
	}

	wantStatuses := []int{http.StatusAccepted, http.StatusNotFound, http.StatusInternalServerError}
	statusBody, err := encodeForwardedConditionStatuses(wantStatuses)
	if err != nil {
		t.Fatal(err)
	}
	gotStatuses, err := decodeForwardedConditionStatuses(statusBody, len(wantStatuses))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotStatuses, wantStatuses) {
		t.Fatalf("statuses = %#v, want %#v", gotStatuses, wantStatuses)
	}
}

func TestForwardedConditionBatchCodecRejectsCorruption(t *testing.T) {
	request := &conditionForwardRequest{jiaIsuUUID: "uuid", conditions: []ForwardedCondition{{Timestamp: 1}}}
	body, err := encodeForwardedConditionBatch([]*conditionForwardRequest{request})
	if err != nil {
		t.Fatal(err)
	}
	for index, test := range [][]byte{nil, body[:len(body)-1], append(append([]byte{}, body...), 0)} {
		if _, _, decodeErr := decodeForwardedConditionBatch(test); decodeErr == nil {
			t.Fatalf("corrupt batch case %d was accepted", index)
		}
	}

	statusBody, err := encodeForwardedConditionStatuses([]int{http.StatusAccepted})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeForwardedConditionStatuses(statusBody, 2); err == nil {
		t.Fatal("wrong expected status count was accepted")
	}
}

func TestIsuRegistryIndexesAndPublishesRegistration(t *testing.T) {
	registry := buildIsuRegistry([]Isu{
		{ID: 3, JIAIsuUUID: "isu-3", Name: "third", Character: "zeta", JIAUserID: "user-a"},
		{ID: 1, JIAIsuUUID: "isu-1", Name: "first", Character: "alpha", JIAUserID: "user-a"},
		{ID: 2, JIAIsuUUID: "isu-2", Name: "second", Character: "zeta", JIAUserID: "user-b"},
	})

	list := registry.listForUser("user-a")
	if got := []int{list[0].ID, list[1].ID}; !reflect.DeepEqual(got, []int{3, 1}) {
		t.Fatalf("list order = %v, want newest ID first", got)
	}
	if _, ok := registry.get("user-b", "isu-2"); !ok {
		t.Fatal("owner lookup missed an existing ISU")
	}
	if _, ok := registry.get("user-a", "isu-2"); ok {
		t.Fatal("owner lookup crossed user boundary")
	}
	characters, rows := registry.trendSnapshot()
	if !reflect.DeepEqual(characters, []string{"alpha", "zeta"}) || len(rows) != 3 {
		t.Fatalf("trend snapshot = characters %v, rows %d", characters, len(rows))
	}

	registry.add(Isu{ID: 4, JIAIsuUUID: "isu-4", Name: "fourth", Character: "beta", JIAUserID: "user-a"})
	list = registry.listForUser("user-a")
	if got := []int{list[0].ID, list[1].ID, list[2].ID}; !reflect.DeepEqual(got, []int{4, 3, 1}) {
		t.Fatalf("list after registration = %v", got)
	}
	characters, rows = registry.trendSnapshot()
	if !reflect.DeepEqual(characters, []string{"alpha", "beta", "zeta"}) || len(rows) != 4 {
		t.Fatalf("trend snapshot after registration = characters %v, rows %d", characters, len(rows))
	}
}

func referenceDecodeIncomingConditions(body []byte) ([]ForwardedCondition, error) {
	incoming := []IncomingCondition{}
	if err := conditionJSON.Unmarshal(body, &incoming); err != nil || len(incoming) == 0 {
		return nil, fmt.Errorf("bad request body: %v", err)
	}
	conditions := make([]ForwardedCondition, len(incoming))
	for index := range incoming {
		flags, ok := conditionBitsByString[incoming[index].Condition]
		if !ok {
			flags = 0xff
		} else if incoming[index].IsSitting {
			flags |= conditionFlagSitting
		}
		conditions[index] = ForwardedCondition{
			Timestamp: incoming[index].Timestamp,
			Message:   incoming[index].Message,
			Flags:     flags,
		}
	}
	return conditions, nil
}

func TestDirectConditionDecoderMatchesReference(t *testing.T) {
	tests := []string{
		`[{"timestamp":1620000000,"condition":"is_dirty=false,is_overweight=true,is_broken=false","message":"ok","is_sitting":true}]`,
		`[{"unknown":{"nested":[1,2,3]},"message":"escaped \"quote\" and 日本語","is_sitting":false,"condition":"is_dirty=true,is_overweight=false,is_broken=true","timestamp":-1}]`,
		`[{"timestamp":null,"condition":null,"message":null,"is_sitting":null}]`,
		`[{"timestamp":1,"condition":"invalid","message":"bad","is_sitting":true}]`,
		`[null]`,
		`[]`,
		`{}`,
		`[`,
		`[{"timestamp":1,"condition":"is_dirty=false,is_overweight=false,is_broken=false"}] trailing`,
		`[{"timestamp":"one","condition":"is_dirty=false,is_overweight=false,is_broken=false"}]`,
	}
	for _, body := range tests {
		want, wantErr := referenceDecodeIncomingConditions([]byte(body))
		got, gotErr := decodeIncomingConditions([]byte(body))
		if (wantErr == nil) != (gotErr == nil) {
			t.Fatalf("error compatibility differs for %q: reference=%v direct=%v", body, wantErr, gotErr)
		}
		if wantErr == nil && !reflect.DeepEqual(got, want) {
			t.Fatalf("decoded %q as %#v, want %#v", body, got, want)
		}
	}
}

var benchmarkConditionBody = []byte(`[
  {"timestamp":1620000000,"condition":"is_dirty=false,is_overweight=true,is_broken=false","message":"normal message","is_sitting":true},
  {"timestamp":1620000001,"condition":"is_dirty=true,is_overweight=false,is_broken=true","message":"日本語 message","is_sitting":false},
  {"timestamp":1620000002,"condition":"is_dirty=false,is_overweight=false,is_broken=false","message":"third","is_sitting":true},
  {"timestamp":1620000003,"condition":"is_dirty=true,is_overweight=true,is_broken=true","message":"fourth","is_sitting":false}
]`)

func BenchmarkConditionDecoderReference(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if _, err := referenceDecodeIncomingConditions(benchmarkConditionBody); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConditionDecoderDirect(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if _, err := decodeIncomingConditions(benchmarkConditionBody); err != nil {
			b.Fatal(err)
		}
	}
}
