package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/labstack/echo/v4"
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

func TestTrendSnapshotHasNoExpiry(t *testing.T) {
	state := newConditionState()
	want := []byte(`[{"character":"SittingIsPower"}]`)
	state.trendBody = want
	previous := currentConditionState()
	conditionState.Store(state)
	if previous != nil {
		t.Cleanup(func() { conditionState.Store(previous) })
	}

	e := echo.New()
	recorder := httptest.NewRecorder()
	context := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/trend", nil), recorder)
	if err := getTrend(context); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !bytes.Equal(recorder.Body.Bytes(), want) {
		t.Fatalf("trend response = %q, want %q", recorder.Body.Bytes(), want)
	}
}

func TestTrendSnapshotRefreshesOnlyDuringRamp(t *testing.T) {
	state := newConditionState()
	state.trendBody = []byte(`[]`)
	now := time.Unix(100, 0)

	if !trendSnapshotFresh(state, now) {
		t.Fatal("snapshot before condition ramp must stay frozen")
	}
	atomic.StoreInt64(&state.trendRampStartedAt, now.UnixNano())
	state.trendExpiresAt = now.Add(trendRampRefreshInterval)
	if !trendSnapshotFresh(state, now.Add(trendRampRefreshInterval-time.Nanosecond)) {
		t.Fatal("snapshot should be reused before the refresh interval")
	}
	if trendSnapshotFresh(state, now.Add(trendRampRefreshInterval)) {
		t.Fatal("snapshot should refresh when the interval expires during ramp")
	}
	if !trendSnapshotFresh(state, now.Add(trendRampDuration)) {
		t.Fatal("snapshot must freeze after the ramp window")
	}
}

func TestCompletedConditionRangeRequiresSafetyHorizon(t *testing.T) {
	state := newConditionState()
	uuid := "f4880cf8-67ec-4f4c-914d-72673eac94a2"
	endTime := time.Unix(1_700_000_000, 0).Unix()
	history := getOrCreateConditionHistory(state, uuid)
	history.conditions = []CachedCondition{{Timestamp: endTime + int64(completedRangeSafetyHorizon/time.Second) - 1}}

	if completedConditionRange(state, uuid, endTime) {
		t.Fatal("range must not be cached before the safety horizon")
	}
	history.conditions[0].Timestamp++
	if !completedConditionRange(state, uuid, endTime) {
		t.Fatal("range should be cacheable once the safety horizon has elapsed")
	}
	if completedConditionRange(state, "missing", endTime) {
		t.Fatal("missing ISU must not be cacheable")
	}
}

func TestTrendBecomesPrivateCacheableAfterRamp(t *testing.T) {
	state := newConditionState()
	state.trendBody = []byte(`[]`)
	atomic.StoreInt64(&state.trendRampStartedAt, time.Now().Add(-trendRampDuration).UnixNano())
	previous := currentConditionState()
	conditionState.Store(state)
	if previous != nil {
		t.Cleanup(func() { conditionState.Store(previous) })
	}

	e := echo.New()
	recorder := httptest.NewRecorder()
	context := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/trend", nil), recorder)
	if err := getTrend(context); err != nil {
		t.Fatal(err)
	}
	if got := recorder.Header().Get("Cache-Control"); got != privateResponseCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, privateResponseCacheControl)
	}
}

func TestServeImmutableIsuIcon(t *testing.T) {
	const uuid = "7f943f69-13d0-4b55-8912-730d1bf6a24d"
	image := []byte("immutable image")
	e := echo.New()

	first := httptest.NewRecorder()
	firstContext := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/isu/"+uuid+"/icon", nil), first)
	if err := serveImmutableIsuIcon(firstContext, uuid, image); err != nil {
		t.Fatal(err)
	}
	if first.Code != http.StatusOK || !bytes.Equal(first.Body.Bytes(), image) {
		t.Fatalf("first response = (%d, %q), want (%d, %q)", first.Code, first.Body.Bytes(), http.StatusOK, image)
	}
	if got := first.Header().Get("Cache-Control"); got != privateResponseCacheControl {
		t.Fatalf("Cache-Control = %q", got)
	}
	etag := first.Header().Get("ETag")
	if etag != `"`+uuid+`"` {
		t.Fatalf("ETag = %q", etag)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/isu/"+uuid+"/icon", nil)
	request.Header.Set("If-None-Match", etag)
	revalidated := httptest.NewRecorder()
	revalidatedContext := e.NewContext(request, revalidated)
	if err := serveImmutableIsuIcon(revalidatedContext, uuid, image); err != nil {
		t.Fatal(err)
	}
	if revalidated.Code != http.StatusNotModified || revalidated.Body.Len() != 0 {
		t.Fatalf("revalidated response = (%d, %q), want empty 304", revalidated.Code, revalidated.Body.Bytes())
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

func TestLatestCachedConditionTracksOutOfOrderUpdates(t *testing.T) {
	state := newConditionState()
	cacheConditionHistory(state, "isu", []CachedCondition{{Timestamp: 20}, {Timestamp: 10}})
	if latest, ok := latestCachedCondition(state, "isu"); !ok || latest.Timestamp != 20 {
		t.Fatalf("first latest = %#v, %v", latest, ok)
	}
	cacheConditionHistory(state, "isu", []CachedCondition{{Timestamp: 15}})
	if latest, ok := latestCachedCondition(state, "isu"); !ok || latest.Timestamp != 20 {
		t.Fatalf("out-of-order latest = %#v, %v", latest, ok)
	}
	cacheConditionHistory(state, "isu", []CachedCondition{{Timestamp: 30}})
	if latest, ok := latestCachedCondition(state, "isu"); !ok || latest.Timestamp != 30 {
		t.Fatalf("new latest = %#v, %v", latest, ok)
	}
	if _, ok := latestCachedCondition(state, "missing"); ok {
		t.Fatal("missing history returned a latest condition")
	}
}

func TestForwardedConditionHistoryAppendsInPlaceAndStaysSorted(t *testing.T) {
	state := newConditionState()
	cacheForwardedConditionHistory(state, "isu", []ForwardedCondition{
		{Timestamp: 20, Message: "twenty", Flags: 2},
		{Timestamp: 10, Message: "ten", Flags: 1},
	})
	cacheForwardedConditionHistory(state, "isu", []ForwardedCondition{
		{Timestamp: 15, Message: "fifteen", Flags: 3},
		{Timestamp: 30, Message: "thirty", Flags: 4},
	})
	history := state.histories["isu"]
	if cap(history.conditions) < conditionHistoryInitialCap {
		t.Fatalf("history capacity = %d, want at least %d", cap(history.conditions), conditionHistoryInitialCap)
	}
	wantTimestamps := []int64{10, 15, 20, 30}
	wantMessages := []string{"ten", "fifteen", "twenty", "thirty"}
	for index, condition := range history.conditions {
		if condition.Timestamp != wantTimestamps[index] || conditionMessage(state, condition.MessageID) != wantMessages[index] {
			t.Fatalf("condition[%d] = %#v message=%q", index, condition, conditionMessage(state, condition.MessageID))
		}
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

func TestConditionForwardStats(t *testing.T) {
	resetConditionForwardStats()
	recordConditionForwardBatch(1)
	recordConditionForwardBatch(4)
	recordConditionForwardBatch(conditionForwardBatchLimit)
	stats := currentConditionForwardStats()
	if stats.Batches != 3 || stats.Requests != 69 || stats.MaxBatch != conditionForwardBatchLimit || stats.AverageBatch != 23 {
		t.Fatalf("unexpected forward stats: %#v", stats)
	}
	resetConditionForwardStats()
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
	referenceBody, err := encodeForwardedConditionBatchReference(requests)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, referenceBody) {
		t.Fatal("direct batch encoding changed the wire format")
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
	var payloads [conditionForwardBatchLimit][]byte
	count, err := parseForwardedConditionBatchPayloads(body, &payloads)
	if err != nil {
		t.Fatal(err)
	}
	if count != len(requests) {
		t.Fatalf("direct payload count = %d, want %d", count, len(requests))
	}
	for index := range requests {
		uuid, decoded, decodeErr := decodeForwardedConditions(payloads[index])
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if uuid != requests[index].jiaIsuUUID || !reflect.DeepEqual(decoded, requests[index].conditions) {
			t.Fatalf("direct payload[%d] = %q %#v", index, uuid, decoded)
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

func encodeForwardedConditionBatchReference(requests []*conditionForwardRequest) ([]byte, error) {
	if len(requests) == 0 || len(requests) > int(^uint16(0)) {
		return nil, fmt.Errorf("invalid forwarded batch size")
	}
	payloads := make([][]byte, len(requests))
	size := 6
	for index := range requests {
		payload, err := encodeForwardedConditions(requests[index].jiaIsuUUID, requests[index].conditions)
		if err != nil {
			return nil, err
		}
		payloads[index] = payload
		size += 4 + len(payload)
	}
	body := make([]byte, size)
	copy(body, "ICB1")
	binary.LittleEndian.PutUint16(body[4:], uint16(len(payloads)))
	offset := 6
	for index := range payloads {
		binary.LittleEndian.PutUint32(body[offset:], uint32(len(payloads[index])))
		offset += 4
		copy(body[offset:], payloads[index])
		offset += len(payloads[index])
	}
	return body, nil
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
		var payloads [conditionForwardBatchLimit][]byte
		if _, parseErr := parseForwardedConditionBatchPayloads(test, &payloads); parseErr == nil {
			t.Fatalf("direct corrupt batch case %d was accepted", index)
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

func TestDecodedForwardBatchDoesNotAliasRequestBody(t *testing.T) {
	requests := []*conditionForwardRequest{{
		jiaIsuUUID: "persistent-uuid",
		conditions: []ForwardedCondition{{Timestamp: 1, Message: "persistent message", Flags: 3}},
	}}
	body, err := encodeForwardedConditionBatch(requests)
	if err != nil {
		t.Fatal(err)
	}
	uuids, conditions, err := decodeForwardedConditionBatch(body)
	if err != nil {
		t.Fatal(err)
	}
	for index := range body {
		body[index] = 'x'
	}
	if uuids[0] != "persistent-uuid" || conditions[0][0].Message != "persistent message" {
		t.Fatalf("decoded batch aliases request body: uuids=%q conditions=%#v", uuids, conditions)
	}
}

func TestForwardBatchRequestBodyUsesBoundedPool(t *testing.T) {
	body, err := encodeForwardedConditionBatch(benchmarkForwardRequests)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "/internal/condition-batch", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	got, pooled, err := readForwardBatchRequestBody(request)
	if err != nil {
		t.Fatal(err)
	}
	if pooled == nil || !bytes.Equal(got, body) {
		t.Fatal("forward batch did not use the bounded pool")
	}
	releaseForwardBatchRequestBuffer(pooled)

	large := bytes.Repeat([]byte("x"), forwardBatchRequestBufSize+1)
	request, err = http.NewRequest(http.MethodPost, "/internal/condition-batch", bytes.NewReader(large))
	if err != nil {
		t.Fatal(err)
	}
	got, pooled, err = readForwardBatchRequestBody(request)
	if err != nil {
		t.Fatal(err)
	}
	if pooled != nil || !bytes.Equal(got, large) {
		t.Fatal("large forward batch fallback differs")
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

func TestConditionRequestBodyUsesBoundedPool(t *testing.T) {
	body := []byte(`[{"timestamp":1,"condition":"is_dirty=false,is_overweight=false,is_broken=false","message":"ok","is_sitting":true}]`)
	request, err := http.NewRequest(http.MethodPost, "/api/condition/test", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	got, pooled, err := readConditionRequestBody(request)
	if err != nil {
		t.Fatal(err)
	}
	if pooled == nil {
		t.Fatal("small condition body did not use the pool")
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body = %q, want %q", got, body)
	}
	releaseConditionRequestBuffer(pooled)

	large := bytes.Repeat([]byte("x"), conditionRequestBufferSize+1)
	request, err = http.NewRequest(http.MethodPost, "/api/condition/test", bytes.NewReader(large))
	if err != nil {
		t.Fatal(err)
	}
	got, pooled, err = readConditionRequestBody(request)
	if err != nil {
		t.Fatal(err)
	}
	if pooled != nil {
		t.Fatal("large condition body unexpectedly used the pool")
	}
	if !bytes.Equal(got, large) {
		t.Fatal("large fallback body differs")
	}

	request, err = http.NewRequest(http.MethodPost, "/api/condition/test", io.NopCloser(bytes.NewReader(body[:4])))
	if err != nil {
		t.Fatal(err)
	}
	request.ContentLength = int64(len(body))
	if _, _, err = readConditionRequestBody(request); err == nil {
		t.Fatal("truncated fixed-length body was accepted")
	}
}

func TestDecodedConditionDoesNotAliasRequestBody(t *testing.T) {
	body := []byte(`[{"timestamp":1,"condition":"is_dirty=false,is_overweight=false,is_broken=false","message":"persistent message","is_sitting":true}]`)
	conditions, err := decodeIncomingConditions(body)
	if err != nil {
		t.Fatal(err)
	}
	for index := range body {
		body[index] = 'x'
	}
	if got := conditions[0].Message; got != "persistent message" {
		t.Fatalf("decoded message aliases request body: %q", got)
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

func BenchmarkConditionRequestReadAll(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		request := &http.Request{Body: io.NopCloser(bytes.NewReader(benchmarkConditionBody))}
		if _, err := io.ReadAll(request.Body); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConditionRequestPooled(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		request := &http.Request{
			Body:          io.NopCloser(bytes.NewReader(benchmarkConditionBody)),
			ContentLength: int64(len(benchmarkConditionBody)),
		}
		if _, pooled, err := readConditionRequestBody(request); err != nil {
			b.Fatal(err)
		} else {
			releaseConditionRequestBuffer(pooled)
		}
	}
}

var benchmarkForwardRequests = func() []*conditionForwardRequest {
	requests := make([]*conditionForwardRequest, 64)
	conditions := []ForwardedCondition{
		{Timestamp: 1620000000, Message: "normal message", Flags: 10},
		{Timestamp: 1620000001, Message: "日本語 message", Flags: 5},
		{Timestamp: 1620000002, Message: "third", Flags: 8},
		{Timestamp: 1620000003, Message: "fourth", Flags: 7},
	}
	for index := range requests {
		requests[index] = &conditionForwardRequest{
			jiaIsuUUID: fmt.Sprintf("%08d-89ab-cdef-0123-456789abcdef", index),
			conditions: conditions,
		}
	}
	return requests
}()

var benchmarkForwardBatchBody = func() []byte {
	body, err := encodeForwardedConditionBatch(benchmarkForwardRequests)
	if err != nil {
		panic(err)
	}
	return body
}()

func BenchmarkForwardBatchEncoderReference(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if _, err := encodeForwardedConditionBatchReference(benchmarkForwardRequests); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkForwardBatchEncoderDirect(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if _, err := encodeForwardedConditionBatch(benchmarkForwardRequests); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkForwardBatchDecoderIntermediate(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if _, _, err := decodeForwardedConditionBatch(benchmarkForwardBatchBody); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkForwardBatchDecoderDirect(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		var payloads [conditionForwardBatchLimit][]byte
		if _, err := parseForwardedConditionBatchPayloads(benchmarkForwardBatchBody, &payloads); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCacheForwardedIntermediate(b *testing.B) {
	state := newConditionState()
	history := getOrCreateConditionHistory(state, "benchmark-isu")
	ensureConditionHistoryCapacity(history, conditionHistoryInitialCap)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		history.conditions = history.conditions[:0]
		compact := make([]CachedCondition, len(benchmarkForwardRequests[0].conditions))
		for index := range compact {
			compact[index] = CachedCondition{
				Timestamp: benchmarkForwardRequests[0].conditions[index].Timestamp,
				MessageID: internConditionMessage(state, benchmarkForwardRequests[0].conditions[index].Message),
				Flags:     benchmarkForwardRequests[0].conditions[index].Flags,
			}
		}
		cacheConditionHistory(state, "benchmark-isu", compact)
	}
}

func BenchmarkCacheForwardedInPlace(b *testing.B) {
	state := newConditionState()
	history := getOrCreateConditionHistory(state, "benchmark-isu")
	ensureConditionHistoryCapacity(history, conditionHistoryInitialCap)
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		history.conditions = history.conditions[:0]
		cacheForwardedConditionHistory(state, "benchmark-isu", benchmarkForwardRequests[0].conditions)
	}
}

func BenchmarkForwardBatchRequestReadAll(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		request := &http.Request{Body: io.NopCloser(bytes.NewReader(benchmarkForwardBatchBody))}
		if _, err := io.ReadAll(request.Body); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkForwardBatchRequestPooled(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		request := &http.Request{
			Body:          io.NopCloser(bytes.NewReader(benchmarkForwardBatchBody)),
			ContentLength: int64(len(benchmarkForwardBatchBody)),
		}
		if _, pooled, err := readForwardBatchRequestBody(request); err != nil {
			b.Fatal(err)
		} else {
			releaseForwardBatchRequestBuffer(pooled)
		}
	}
}

var benchmarkForwardStatusBody = func() []byte {
	statuses := make([]int, conditionForwardBatchLimit)
	for index := range statuses {
		statuses[index] = http.StatusAccepted
	}
	body, err := encodeForwardedConditionStatuses(statuses)
	if err != nil {
		panic(err)
	}
	return body
}()

func BenchmarkForwardStatusDecoderAllocating(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if _, err := decodeForwardedConditionStatuses(benchmarkForwardStatusBody, conditionForwardBatchLimit); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkForwardStatusDecoderInto(b *testing.B) {
	statuses := make([]int, conditionForwardBatchLimit)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if err := decodeForwardedConditionStatusesInto(benchmarkForwardStatusBody, statuses); err != nil {
			b.Fatal(err)
		}
	}
}

var benchmarkLatestState, benchmarkLatestUUIDs = func() (*ConditionState, []string) {
	state := newConditionState()
	uuids := make([]string, 512)
	for index := range uuids {
		uuids[index] = fmt.Sprintf("isu-%04d", index)
		state.histories[uuids[index]] = &ConditionHistory{conditions: []CachedCondition{{Timestamp: int64(index)}}}
	}
	state.loaded = true
	return state, uuids
}()

var benchmarkLatestTimestamp int64

func benchmarkLatestSnapshot(state *ConditionState) map[string]CachedCondition {
	state.RLock()
	histories := make(map[string]*ConditionHistory, len(state.histories))
	for uuid, history := range state.histories {
		histories[uuid] = history
	}
	state.RUnlock()
	conditions := make(map[string]CachedCondition, len(histories))
	for uuid, history := range histories {
		history.RLock()
		conditions[uuid] = history.conditions[len(history.conditions)-1]
		history.RUnlock()
	}
	return conditions
}

func BenchmarkLatestConditionsSnapshot(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		latest := benchmarkLatestSnapshot(benchmarkLatestState)
		benchmarkLatestTimestamp = latest[benchmarkLatestUUIDs[len(benchmarkLatestUUIDs)-1]].Timestamp
	}
}

func BenchmarkLatestConditionsDirect(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		benchmarkLatestState.RLock()
		for _, uuid := range benchmarkLatestUUIDs {
			history := benchmarkLatestState.histories[uuid]
			history.RLock()
			benchmarkLatestTimestamp = history.conditions[len(history.conditions)-1].Timestamp
			history.RUnlock()
		}
		benchmarkLatestState.RUnlock()
	}
}
