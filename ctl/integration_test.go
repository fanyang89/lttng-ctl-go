//go:build integration && linux && cgo

package ctl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestIntegrationAdvancedControl(t *testing.T) {
	if os.Getenv("LTTNG_CTL_GO_INTEGRATION") != "1" {
		t.Skip("use the isolated integration harness")
	}
	ctx := t.Context()
	name := fmt.Sprintf("ctl-advanced-%d", os.Getpid())
	descriptor := take(SessionDescriptorLocalCreate(ctx, &name, new(filepath.Join(t.TempDir(), "trace"))))
	defer descriptor.Close()
	success(CreateSessionExt(ctx, descriptor))
	t.Cleanup(func() { _, _ = DestroySession(context.Background(), &name) })
	domain := take(NewDomain(ctx, DomainData{Type: DomainUST, BufferType: BufferPerUID}))
	defer domain.Close()
	handle := take(CreateHandle(ctx, &name, domain))
	defer handle.Close()
	channel := take(ChannelCreate(ctx, domain))
	defer channel.Close()
	data := take(channel.Data(ctx))
	data.Name = "events"
	data.Attributes.SubbufferSize = 4096
	data.Attributes.SubbufferCount = 2
	if err := channel.SetData(ctx, data); err != nil {
		t.Fatal(err)
	}
	success(EnableChannel(ctx, handle, channel))
	event := take(EventCreate(ctx))
	defer event.Close()
	if err := event.SetData(ctx, EventData{Name: "*", Type: EventTracepoint}); err != nil {
		t.Fatal(err)
	}
	success(EnableEventWithFilter(ctx, handle, event, new("events"), new("$ctx.vtid > 0")))
	_, sessions, err := ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range sessions.Items() {
		data := take(session.Data(ctx))
		if data.Name == name {
			_, _, err = SessionGetCreationTime(ctx, session)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = GetSessionShmPathOverride(ctx, session)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	_ = sessions.Close()
	_, tracker, err := SessionGetTrackerHandle(ctx, &name, DomainUST, ProcessAttrVirtualProcessID)
	if err != nil {
		t.Fatal(err)
	}
	defer tracker.Close()
	success(ProcessAttrTrackerHandleSetTrackingPolicy(ctx, tracker, TrackingPolicyIncludeSet))
	success(ProcessAttrVirtualProcessIDTrackerHandleAddPID(ctx, tracker, int32(os.Getpid())))
	_, values, err := ProcessAttrTrackerHandleGetInclusionSet(ctx, tracker)
	if err != nil {
		t.Fatal(err)
	}
	if _, count, err := ProcessAttrValuesGetCount(ctx, values); err != nil || count != 1 {
		t.Fatalf("tracker %d %v", count, err)
	}
	success(ProcessAttrVirtualProcessIDTrackerHandleRemovePID(ctx, tracker, int32(os.Getpid())))
	success(ProcessAttrTrackerHandleSetTrackingPolicy(ctx, tracker, TrackingPolicyIncludeAll))
	schedule := take(RotationSchedulePeriodicCreate(ctx))
	defer schedule.Close()
	success(RotationSchedulePeriodicSetPeriod(ctx, schedule, 60000000))
	success(SessionAddRotationSchedule(ctx, &name, schedule))
	_, schedules, err := SessionListRotationSchedules(ctx, &name)
	if err != nil {
		t.Fatal(err)
	}
	defer schedules.Close()
	if _, count, err := RotationSchedulesGetCount(ctx, schedules); err != nil || count != 1 {
		t.Fatalf("schedules %d %v", count, err)
	}
	success(SessionRemoveRotationSchedule(ctx, &name, schedule))
	success(StartTracing(ctx, &name))
	success(StopTracing(ctx, &name))
	_, clearHandle, err := ClearSession(ctx, &name)
	if err != nil {
		t.Fatal(err)
	}
	defer clearHandle.Close()
	if status, err := ClearHandleWaitForCompletion(ctx, clearHandle, 5000); err != nil || status != ClearHandleStatusCompleted {
		t.Fatalf("clear %v %v", status, err)
	}
	if _, code, err := ClearHandleGetResult(ctx, clearHandle); err != nil || code != Ok {
		t.Fatalf("clear result %v %v", code, err)
	}

	condition := take(ConditionSessionConsumedSizeCreate(ctx))
	defer condition.Close()
	success(ConditionSessionConsumedSizeSetSessionName(ctx, condition, &name))
	success(ConditionSessionConsumedSizeSetThreshold(ctx, condition, 1024))
	action := take(ActionNotifyCreate(ctx))
	defer action.Close()
	trigger := take(TriggerCreate(ctx, condition, action))
	defer trigger.Close()
	endpoint := take(SessionDaemonNotificationEndpoint(ctx))
	defer endpoint.Close()
	notifications := take(NotificationChannelCreate(ctx, endpoint))
	defer notifications.Close()
	success(NotificationChannelSubscribe(ctx, notifications, condition))
	success(RegisterTriggerWithName(ctx, trigger, new(name+"-trigger")))
	defer func() { _, _ = UnregisterTrigger(context.Background(), trigger) }()
	_, triggers, err := ListTriggers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer triggers.Close()
	if _, count, err := TriggersGetCount(ctx, triggers); err != nil || count == 0 {
		t.Fatalf("triggers %d %v", count, err)
	}
	if _, pending, err := NotificationChannelHasPendingNotification(ctx, notifications); err != nil || pending {
		t.Fatalf("notification %v %v", pending, err)
	}
	success(NotificationChannelUnsubscribe(ctx, notifications, condition))
	query := take(ErrorQueryTriggerCreate(ctx, trigger))
	defer query.Close()
	command := take(SessionDaemonCommandEndpoint(ctx))
	defer command.Close()
	_, results, err := ErrorQueryExecute(ctx, query, command)
	if err != nil {
		t.Fatal(err)
	}
	defer results.Close()
	_, _, err = ErrorQueryResultsGetCount(ctx, results)
	if err != nil {
		t.Fatal(err)
	}

	configDir := t.TempDir()
	save := take(SaveSessionAttrCreate(ctx))
	defer save.Close()
	success(SaveSessionAttrSetSessionName(ctx, save, &name))
	success(SaveSessionAttrSetOutputURL(ctx, save, &configDir))
	success(SaveSession(ctx, save))
	_, destruction, err := DestroySessionExt(ctx, &name)
	if err != nil {
		t.Fatal(err)
	}
	defer destruction.Close()
	if status, err := DestructionHandleWaitForCompletion(ctx, destruction, 5000); err != nil || status != DestructionHandleStatusCompleted {
		t.Fatalf("destruction %v %v", status, err)
	}
	if _, code, err := DestructionHandleGetResult(ctx, destruction); err != nil || code != Ok {
		t.Fatalf("destruction result %v %v", code, err)
	}
	load := take(LoadSessionAttrCreate(ctx))
	defer load.Close()
	success(LoadSessionAttrSetInputURL(ctx, load, &configDir))
	success(LoadSessionAttrSetSessionName(ctx, load, &name))
	success(LoadSession(ctx, load))
	_, restored, err := ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	found := false
	for _, session := range restored.Items() {
		data, err := session.Data(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found = found || data.Name == name
	}
	if !found {
		t.Fatal("load reported success without restoring the saved session")
	}
}
