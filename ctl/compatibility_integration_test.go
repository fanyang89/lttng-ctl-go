//go:build integration && linux && cgo

package ctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	lttng "github.com/fanyang89/lttng-ctl-go"
)

func TestCompatibilityDaemon(t *testing.T) {
	if os.Getenv("LTTNG_CTL_GO_INTEGRATION") != "1" {
		t.Skip("use the isolated integration harness")
	}
	ctx := t.Context()
	name := fmt.Sprintf("ctl-compat-%d", os.Getpid())
	path := filepath.Join(t.TempDir(), "trace")
	success(CreateSession(ctx, &name, &path))
	t.Cleanup(func() { _, _ = DestroySession(context.Background(), &name) })
	ust := lttng.Domain{Type: lttng.DomainUST, BufferType: lttng.BufferPerUID}
	config, err := lttng.DefaultChannelConfig(ctx, ust)
	if err != nil {
		t.Fatal(err)
	}
	config.SubbufferSize, config.SubbufferCount = 4096, 2
	if err := lttng.EnableChannel(ctx, name, ust, "events", &config); err != nil {
		t.Fatal(err)
	}
	if err := lttng.EnableEvent(ctx, name, ust, "events", lttng.EventRule{Name: "lttng_ust_tracef:*"}); err != nil {
		t.Fatal(err)
	}
	domain := take(NewDomain(ctx, DomainData{Type: DomainUST, BufferType: BufferPerUID}))
	defer domain.Close()
	handle := take(CreateHandle(ctx, &name, domain))
	defer handle.Close()
	field := take(NewEventContext(ctx, ContextData{Type: EventContextVTID}))
	defer field.Close()
	success(AddContext(ctx, handle, field, nil, new("events")))
	_, channels, err := ListChannels(ctx, handle)
	if err != nil {
		t.Fatal(err)
	}
	defer channels.Close()
	if len(channels.Items()) != 1 {
		t.Fatalf("channels: %d", len(channels.Items()))
	}
	if data := take(channels.Items()[0].Data(ctx)); data.Name != "events" || data.Attributes.SubbufferSize != 4096 {
		t.Fatalf("channel ABI: %+v", data)
	}
	_, events, err := ListEvents(ctx, handle, new("events"))
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()
	if len(events.Items()) != 1 {
		t.Fatalf("events: %d", len(events.Items()))
	}
	if data := take(events.Items()[0].Data(ctx)); data.Name != "lttng_ust_tracef:*" || data.Type != EventTracepoint {
		t.Fatalf("event ABI: %+v", data)
	}
	success(StartTracing(ctx, &name))
	success(StopTracing(ctx, &name))
	_, sessions, err := ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sessions.Close()
	found := false
	for _, session := range sessions.Items() {
		data := take(session.Data(ctx))
		if data.Name == name {
			found = true
			if data.Enabled != 0 {
				t.Fatal("session did not stop")
			}
		}
	}
	if !found {
		t.Fatal("session missing from list")
	}

	t.Run("save-load", func(t *testing.T) {
		dir := t.TempDir()
		save := take(SaveSessionAttrCreate(ctx))
		defer save.Close()
		success(SaveSessionAttrSetSessionName(ctx, save, &name))
		success(SaveSessionAttrSetOutputURL(ctx, save, &dir))
		success(SaveSession(ctx, save))
		success(DestroySession(ctx, &name))
		load := take(LoadSessionAttrCreate(ctx))
		defer load.Close()
		success(LoadSessionAttrSetSessionName(ctx, load, &name))
		success(LoadSessionAttrSetInputURL(ctx, load, &dir))
		success(LoadSession(ctx, load))
		success(StartTracing(ctx, &name))
		success(StopTracing(ctx, &name))
	})

	t.Run("notification", func(t *testing.T) {
		if available := take(HasSymbol(ctx, "lttng_action_notify_create")); !available {
			t.Skip("notifications require 2.10+")
		}
		condition := take(ConditionBufferUsageHighCreate(ctx))
		defer condition.Close()
		success(ConditionBufferUsageSetSessionName(ctx, condition, &name))
		success(ConditionBufferUsageSetChannelName(ctx, condition, new("events")))
		success(ConditionBufferUsageSetDomainType(ctx, condition, DomainUST))
		success(ConditionBufferUsageSetThresholdRatio(ctx, condition, 0.9))
		action := take(ActionNotifyCreate(ctx))
		defer action.Close()
		trigger := take(TriggerCreate(ctx, condition, action))
		defer trigger.Close()
		endpoint := take(SessionDaemonNotificationEndpoint(ctx))
		defer endpoint.Close()
		channel := take(NotificationChannelCreate(ctx, endpoint))
		defer channel.Close()
		success(NotificationChannelSubscribe(ctx, channel, condition))
		success(RegisterTrigger(ctx, trigger))
		defer func() { _, _ = UnregisterTrigger(context.Background(), trigger) }()
		available := take(HasSymbol(ctx, "lttng_notification_channel_has_pending_notification"))
		_, pending, err := NotificationChannelHasPendingNotification(ctx, channel)
		if available {
			if err != nil || pending {
				t.Fatalf("notification: %v, %v", pending, err)
			}
		} else if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("unavailable notification query: %v", err)
		}
		success(NotificationChannelUnsubscribe(ctx, channel, condition))
	})

	t.Run("snapshot", func(t *testing.T) {
		snapshot := name + "-snapshot"
		success(CreateSessionSnapshot(ctx, &snapshot, nil))
		defer func() { _, _ = DestroySession(context.Background(), &snapshot) }()
		output := take(SnapshotOutputCreate(ctx))
		defer output.Close()
		success(SnapshotOutputSetCtrlURL(ctx, new(filepath.Join(t.TempDir(), "snapshot")), output))
		success(SnapshotOutputSetName(ctx, new("compat"), output))
		success(SnapshotAddOutput(ctx, &snapshot, output))
		_, outputs, err := SnapshotListOutput(ctx, &snapshot)
		if err != nil {
			t.Fatal(err)
		}
		defer outputs.Close()
		item := take(SnapshotOutputListGetNext(ctx, outputs))
		if name := take(SnapshotOutputGetName(ctx, item)); name == nil || *name != "compat" {
			t.Fatalf("snapshot output: %v", name)
		}
		success(SnapshotDelOutput(ctx, &snapshot, output))
	})
}
