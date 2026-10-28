//go:build linux && cgo

package ctl

import (
	"errors"
	"os"
	"testing"
)

func TestCompatibilityCapabilities(t *testing.T) {
	series := os.Getenv("LTTNG_COMPAT_SERIES")
	if series == "" {
		t.Skip("version expectations are supplied by the compatibility harness")
	}
	for _, api := range []struct {
		symbol string
		since  string
	}{
		{"lttng_create_session", "2.8"},
		{"lttng_channel_create", "2.10"},
		{"lttng_action_notify_create", "2.10"},
		{"lttng_session_daemon_notification_endpoint", "2.10"},
		{"lttng_event_create", "2.12"},
		{"lttng_create_session_ext", "2.12"},
		{"lttng_rotate_session", "2.12"},
		{"lttng_notification_channel_has_pending_notification", "2.12"},
		{"lttng_event_rule_user_tracepoint_create", "2.14"},
		{"lttng_session_daemon_command_endpoint", "2.14"},
	} {
		order := map[string]int{"2.8": 0, "2.10": 1, "2.12": 2, "2.14": 3}
		want := order[series] >= order[api.since]
		got, err := HasSymbol(t.Context(), api.symbol)
		if err != nil || got != want {
			t.Errorf("%s on %s: got %v (%v), want %v", api.symbol, series, got, err, want)
		}
	}
}

func TestCompatibilityObjects(t *testing.T) {
	ctx := t.Context()
	domain := take(NewDomain(ctx, DomainData{Type: DomainUST, BufferType: BufferPerUID}))
	defer domain.Close()
	attrs := take(NewChannelAttr(ctx, ChannelAttributes{}))
	defer attrs.Close()
	if err := ChannelSetDefaultAttr(ctx, domain, attrs); err != nil {
		t.Fatal(err)
	}
	if data := take(attrs.Data(ctx)); data.SubbufferSize == 0 || data.SubbufferCount == 0 {
		t.Fatalf("invalid channel defaults: %+v", data)
	}
	output := take(SnapshotOutputCreate(ctx))
	defer output.Close()
	success(SnapshotOutputSetSize(ctx, 4096, output))
	success(SnapshotOutputSetName(ctx, new("compat-snapshot"), output))
	if name := take(SnapshotOutputGetName(ctx, output)); name == nil || *name != "compat-snapshot" {
		t.Fatalf("snapshot name: %v", name)
	}
	if size := take(SnapshotOutputGetMaxsize(ctx, output)); size != 4096 {
		t.Fatalf("snapshot size: %d", size)
	}
	save := take(SaveSessionAttrCreate(ctx))
	defer save.Close()
	success(SaveSessionAttrSetSessionName(ctx, save, new("compat")))
	if name := take(SaveSessionAttrGetSessionName(ctx, save)); name == nil || *name != "compat" {
		t.Fatalf("save name: %v", name)
	}
	load := take(LoadSessionAttrCreate(ctx))
	defer load.Close()
	success(LoadSessionAttrSetSessionName(ctx, load, new("compat")))
	if name := take(LoadSessionAttrGetSessionName(ctx, load)); name == nil || *name != "compat" {
		t.Fatalf("load name: %v", name)
	}
}

func TestCompatibilityTriggerLifetime(t *testing.T) {
	ctx := t.Context()
	condition, err := ConditionBufferUsageHighCreate(ctx)
	if errors.Is(err, ErrUnsupported) {
		t.Skip("buffer conditions require LTTng 2.10+")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer condition.Close()
	success(ConditionBufferUsageSetThresholdRatio(ctx, condition, 0.75))
	action := take(ActionNotifyCreate(ctx))
	defer action.Close()
	trigger := take(TriggerCreate(ctx, condition, action))
	defer trigger.Close()
	_ = condition.Close()
	_ = action.Close()
	if _, _, err := ConditionBufferUsageGetThresholdRatio(ctx, condition); !errors.Is(err, ErrClosed) {
		t.Fatalf("explicitly closed input: %v", err)
	}
	borrowed := take(TriggerGetCondition(ctx, trigger))
	if _, ratio, err := ConditionBufferUsageGetThresholdRatio(ctx, borrowed); err != nil || ratio != 0.75 {
		t.Fatalf("retained condition: %f, %v", ratio, err)
	}
	// A new trigger may receive inputs borrowed from another trigger. It must
	// keep the ultimate owners alive even after the first trigger is closed.
	nested := take(TriggerCreate(ctx, borrowed, take(TriggerGetAction(ctx, trigger))))
	defer nested.Close()
	_ = trigger.Close()
	if _, _, err := ConditionBufferUsageGetThresholdRatio(ctx, borrowed); !errors.Is(err, ErrClosed) {
		t.Fatalf("stale borrow: %v", err)
	}
	if _, ratio, err := ConditionBufferUsageGetThresholdRatio(ctx, take(TriggerGetCondition(ctx, nested))); err != nil || ratio != 0.75 {
		t.Fatalf("retained borrowed input: %f, %v", ratio, err)
	}
}

func TestCompatibilityRotation(t *testing.T) {
	ctx := t.Context()
	schedule, err := RotationSchedulePeriodicCreate(ctx)
	if errors.Is(err, ErrUnsupported) {
		t.Skip("rotation schedules require LTTng 2.11+")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer schedule.Close()
	success(RotationSchedulePeriodicSetPeriod(ctx, schedule, 60000000))
	if _, period, err := RotationSchedulePeriodicGetPeriod(ctx, schedule); err != nil || period != 60000000 {
		t.Fatalf("rotation period: %d, %v", period, err)
	}
}
