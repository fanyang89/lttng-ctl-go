//go:build linux && cgo

package ctl

import (
	"context"
	"errors"
	"testing"
)

func take[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
func success[T any](_ T, err error) {
	if err != nil {
		panic(err)
	}
}

func TestNativeEventRuleFamilies(t *testing.T) {
	ctx := t.Context()
	families := []struct {
		name      string
		create    func(context.Context) (*EventRule, error)
		setName   func(context.Context, *EventRule, *string) (EventRuleStatus, error)
		getName   func(context.Context, *EventRule) (EventRuleStatus, *string, error)
		setFilter func(context.Context, *EventRule, *string) (EventRuleStatus, error)
		setLevel  func(context.Context, *EventRule, *LogLevelRule) (EventRuleStatus, error)
	}{
		{"ust", EventRuleUserTracepointCreate, EventRuleUserTracepointSetNamePattern, EventRuleUserTracepointGetNamePattern, EventRuleUserTracepointSetFilter, EventRuleUserTracepointSetLogLevelRule},
		{"jul", EventRuleJULLoggingCreate, EventRuleJULLoggingSetNamePattern, EventRuleJULLoggingGetNamePattern, EventRuleJULLoggingSetFilter, EventRuleJULLoggingSetLogLevelRule},
		{"log4j", EventRuleLog4jLoggingCreate, EventRuleLog4jLoggingSetNamePattern, EventRuleLog4jLoggingGetNamePattern, EventRuleLog4jLoggingSetFilter, EventRuleLog4jLoggingSetLogLevelRule},
		{"log4j2", EventRuleLog4j2LoggingCreate, EventRuleLog4j2LoggingSetNamePattern, EventRuleLog4j2LoggingGetNamePattern, EventRuleLog4j2LoggingSetFilter, EventRuleLog4j2LoggingSetLogLevelRule},
		{"python", EventRulePythonLoggingCreate, EventRulePythonLoggingSetNamePattern, EventRulePythonLoggingGetNamePattern, EventRulePythonLoggingSetFilter, EventRulePythonLoggingSetLogLevelRule},
	}
	for _, family := range families {
		t.Run(family.name, func(t *testing.T) {
			rule := take(family.create(ctx))
			defer rule.Close()
			success(family.setName(ctx, rule, new("provider:*")))
			_, name, err := family.getName(ctx, rule)
			if err != nil || name == nil || *name != "provider:*" {
				t.Fatalf("name: %v %v", name, err)
			}
			success(family.setFilter(ctx, rule, new("value > 0")))
			level := take(LogLevelRuleExactlyCreate(ctx, 6))
			defer level.Close()
			success(family.setLevel(ctx, rule, level))
		})
	}
	kernel := take(EventRuleKernelTracepointCreate(ctx))
	defer kernel.Close()
	success(EventRuleKernelTracepointSetNamePattern(ctx, kernel, new("sched_*")))
	success(EventRuleKernelTracepointSetFilter(ctx, kernel, new("prev_pid > 0")))
	syscall := take(EventRuleKernelSyscallCreate(ctx, EventRuleKernelSyscallEmissionSiteEntryExit))
	defer syscall.Close()
	success(EventRuleKernelSyscallSetNamePattern(ctx, syscall, new("open*")))
	success(EventRuleKernelSyscallSetFilter(ctx, syscall, new("fd > 0")))
	location := take(KernelProbeLocationSymbolCreate(ctx, new("do_sys_open"), 4))
	defer location.Close()
	probe := take(EventRuleKernelKprobeCreate(ctx, location))
	defer probe.Close()
	success(EventRuleKernelKprobeSetEventName(ctx, probe, new("open_probe")))
	if _, offset, err := KernelProbeLocationSymbolGetOffset(ctx, location); err != nil || offset != 4 {
		t.Fatalf("offset %d %v", offset, err)
	}
}

func TestNativeOwnershipAndBorrows(t *testing.T) {
	ctx := t.Context()
	policy := take(RatePolicyEveryNCreate(ctx, 3))
	defer policy.Close()
	action := take(ActionNotifyCreate(ctx))
	defer action.Close()
	success(ActionNotifySetRatePolicy(ctx, action, policy))
	_, borrowed, err := ActionNotifyGetRatePolicy(ctx, action)
	if err != nil {
		t.Fatal(err)
	}
	if _, n, err := RatePolicyEveryNGetInterval(ctx, borrowed); err != nil || n != 3 {
		t.Fatalf("policy %d %v", n, err)
	}
	success(ActionNotifySetRatePolicy(ctx, action, policy))
	if _, _, err := RatePolicyEveryNGetInterval(ctx, borrowed); !errors.Is(err, ErrClosed) {
		t.Fatalf("stale borrow: %v", err)
	}
	list := take(ActionListCreate(ctx))
	success(ActionListAddAction(ctx, list, action))
	child := take(ActionListGetAtIndex(ctx, list, 0))
	if _, err := ActionNotifySetRatePolicy(ctx, child, policy); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("const mutation: %v", err)
	}
	_ = list.Close()
	if _, err := ActionGetType(ctx, child); !errors.Is(err, ErrClosed) {
		t.Fatalf("owner close: %v", err)
	}
	_ = child.Close()

	expr := take(EventExprEventPayloadFieldCreate(ctx, new("values")))
	array := take(EventExprArrayFieldElementCreate(ctx, expr, 2))
	defer array.Close()
	if _, err := EventExprGetType(ctx, expr); !errors.Is(err, ErrClosed) {
		t.Fatalf("moved expression: %v", err)
	}
	parent := take(EventExprArrayFieldElementGetParentExpr(ctx, array))
	if name := take(EventExprEventPayloadFieldGetName(ctx, parent)); name == nil || *name != "values" {
		t.Fatal(name)
	}
	rule := take(EventRuleUserTracepointCreate(ctx))
	defer rule.Close()
	condition := take(ConditionEventRuleMatchesCreate(ctx, rule))
	defer condition.Close()
	success(ConditionEventRuleMatchesAppendCaptureDescriptor(ctx, condition, array))
	if _, err := EventExprGetType(ctx, array); !errors.Is(err, ErrClosed) {
		t.Fatalf("moved capture: %v", err)
	}
	_, count, err := ConditionEventRuleMatchesGetCaptureDescriptorCount(ctx, condition)
	if err != nil || count != 1 {
		t.Fatalf("captures %d %v", count, err)
	}

	snapshot := take(ActionSnapshotSessionCreate(ctx))
	defer snapshot.Close()
	output := take(SnapshotOutputCreate(ctx))
	success(SnapshotOutputSetLocalPath(ctx, new(t.TempDir()), output))
	success(ActionSnapshotSessionSetOutput(ctx, snapshot, output))
	if _, err := SnapshotOutputGetID(ctx, output); !errors.Is(err, ErrClosed) {
		t.Fatalf("moved snapshot: %v", err)
	}
	_ = output.Close()
	_, ownedOutput, err := ActionSnapshotSessionGetOutput(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if value := take(SnapshotOutputGetCtrlURL(ctx, ownedOutput)); value == nil || *value == "" {
		t.Fatal(value)
	}
}

func TestNativeConditionsAndAttributes(t *testing.T) {
	ctx := t.Context()
	for _, create := range []func(context.Context) (*Condition, error){ConditionBufferUsageHighCreate, ConditionBufferUsageLowCreate} {
		condition := take(create(ctx))
		success(ConditionBufferUsageSetThresholdRatio(ctx, condition, 0.5))
		success(ConditionBufferUsageSetSessionName(ctx, condition, new("session")))
		success(ConditionBufferUsageSetChannelName(ctx, condition, new("channel")))
		success(ConditionBufferUsageSetDomainType(ctx, condition, DomainUST))
		if _, n, err := ConditionBufferUsageGetThresholdRatio(ctx, condition); err != nil || n != 0.5 {
			t.Fatalf("ratio %f %v", n, err)
		}
		_ = condition.Close()
	}
	condition := take(ConditionSessionConsumedSizeCreate(ctx))
	defer condition.Close()
	success(ConditionSessionConsumedSizeSetThreshold(ctx, condition, 1024))
	success(ConditionSessionConsumedSizeSetSessionName(ctx, condition, new("session")))
	for _, create := range []func(context.Context) (*Condition, error){ConditionSessionRotationOngoingCreate, ConditionSessionRotationCompletedCreate} {
		c := take(create(ctx))
		success(ConditionSessionRotationSetSessionName(ctx, c, new("session")))
		_ = c.Close()
	}
	path := take(ActionPathCreate(ctx, []uint64{1, 2, 3}))
	defer path.Close()
	if _, n, err := ActionPathGetIndexAtIndex(ctx, path, 1); err != nil || n != 2 {
		t.Fatalf("path %d %v", n, err)
	}
	domain := take(NewDomain(ctx, DomainData{Type: DomainUST, BufferType: BufferPerUID}))
	defer domain.Close()
	channel := take(ChannelCreate(ctx, domain))
	defer channel.Close()
	data := take(channel.Data(ctx))
	data.Name = "channel"
	data.Attributes.SubbufferSize = 4096
	data.Attributes.SubbufferCount = 2
	if err := channel.SetData(ctx, data); err != nil {
		t.Fatal(err)
	}
	success(ChannelSetBlockingTimeout(ctx, channel, 100))
	success(ChannelSetMonitorTimerInterval(ctx, channel, 100000))
	success(ChannelSetAllocationPolicy(ctx, channel, ChannelAllocationPolicyPerCPU))
	if _, n, err := ChannelGetBlockingTimeout(ctx, channel); err != nil || n != 100 {
		t.Fatalf("timeout %d %v", n, err)
	}
	attr := take(NewChannelAttr(ctx, ChannelAttributes{}))
	defer attr.Close()
	if err := ChannelSetDefaultAttr(ctx, domain, attr); err != nil {
		t.Fatal(err)
	}
	if data := take(attr.Data(ctx)); data.SubbufferCount == 0 {
		t.Fatal(data)
	}
	event := take(EventCreate(ctx))
	defer event.Close()
	eventData := EventData{Name: "probe", Type: EventProbe, ProbeSymbol: "symbol", ProbeOffset: 8}
	if err := event.SetData(ctx, eventData); err != nil {
		t.Fatal(err)
	}
	if data := take(event.Data(ctx)); data != eventData {
		t.Fatalf("event %+v", data)
	}
	for _, data := range []ContextData{{Type: EventContextVTID}, {Type: EventContextPerfThreadCounter, PerfType: 1, PerfConfig: 0, PerfName: "cycles"}, {Type: EventContextAppContext, ProviderName: "provider", ContextName: "ctx"}} {
		c := take(NewEventContext(ctx, data))
		_ = c.Close()
	}
	calibration := take(NewCalibration(ctx, CalibrateFunction))
	defer calibration.Close()
	health := take(HealthCreateSessiond(ctx))
	defer health.Close()
	load := take(LoadSessionAttrCreate(ctx))
	defer load.Close()
	success(LoadSessionAttrSetSessionName(ctx, load, new("session")))
	success(LoadSessionAttrSetOverwrite(ctx, load, 1))
	save := take(SaveSessionAttrCreate(ctx))
	defer save.Close()
	success(SaveSessionAttrSetSessionName(ctx, save, new("session")))
	success(SaveSessionAttrSetOmitOutput(ctx, save, 1))
	if _, err := SaveSessionAttrSetSessionName(ctx, save, new("a\x00b")); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
}
