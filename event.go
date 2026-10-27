//go:build linux && cgo

package lttng

/*
#include <stdlib.h>
#include <lttng/lttng.h>
*/
import "C"

import (
	"context"
	"math"
	"unsafe"
)

// EventType selects an instrumentation point type.
type EventType int

const (
	EventTracepoint EventType = C.LTTNG_EVENT_TRACEPOINT
	EventSyscall    EventType = C.LTTNG_EVENT_SYSCALL
)

// LogLevelType selects the log level matching rule.
type LogLevelType int

const (
	LogLevelAll    LogLevelType = C.LTTNG_EVENT_LOGLEVEL_ALL
	LogLevelRange  LogLevelType = C.LTTNG_EVENT_LOGLEVEL_RANGE
	LogLevelSingle LogLevelType = C.LTTNG_EVENT_LOGLEVEL_SINGLE
)

// EventRule describes a recording rule. The zero value matches all tracepoints
// without filtering. LogLevel uses LTTng UST numeric levels (0 emergency through
// 14 debug). It is ignored when LogLevelType is LogLevelAll.
type EventRule struct {
	Name         string // Empty means "*"; LTTng wildcard syntax is supported.
	Type         EventType
	LogLevelType LogLevelType
	LogLevel     int32
	Filter       string   // Empty means no filter; compiled by liblttng-ctl.
	Exclusions   []string // UST name patterns to exclude from a wildcard rule.
}

// Event is either a configured recording rule or a discovered instrumentation
// point. PID is meaningful only for discovery; Enabled only for recording rules.
type Event struct {
	EventRule
	Enabled bool
	PID     int
}

func (rule EventRule) validate(op string, domain Domain) error {
	if err := checkString(op, "event name", rule.Name, C.LTTNG_SYMBOL_NAME_LEN, true); err != nil {
		return err
	}
	if err := checkString(op, "filter", rule.Filter, 0, true); err != nil {
		return err
	}
	if rule.Type != EventTracepoint && (rule.Type != EventSyscall || domain.Type != DomainKernel) {
		return invalid(op, "only tracepoints and kernel syscalls are supported")
	}
	if rule.LogLevelType < LogLevelAll || rule.LogLevelType > LogLevelSingle {
		return invalid(op, "invalid log level matching rule")
	}
	if rule.LogLevelType != LogLevelAll && (domain.Type != DomainUST || rule.LogLevel < 0 || rule.LogLevel > 14) {
		return invalid(op, "log level conditions require UST and a level from 0 through 14")
	}
	if len(rule.Exclusions) > math.MaxInt32 || (len(rule.Exclusions) > 0 && domain.Type != DomainUST) {
		return invalid(op, "exclusions require UST and a count fitting int32")
	}
	for _, name := range rule.Exclusions {
		if err := checkString(op, "exclusion", name, C.LTTNG_SYMBOL_NAME_LEN, false); err != nil {
			return err
		}
	}
	return nil
}

// EnableEvent creates or enables a recording rule. Empty channel selects the
// official default channel. Filter compilation and semantics are delegated to
// liblttng-ctl; the SDK does not translate the expression.
func EnableEvent(ctx context.Context, session string, domain Domain, channel string, rule EventRule) error {
	const op = "enable event"
	if err := checkString(op, "channel name", channel, C.LTTNG_SYMBOL_NAME_LEN, true); err != nil {
		return err
	}
	if err := rule.validate(op, domain); err != nil {
		return err
	}
	return withHandle(ctx, op, session, domain, func(handle *C.struct_lttng_handle) error {
		event := C.lttng_event_create()
		if event == nil {
			return nativeError(op, -C.LTTNG_ERR_NOMEM)
		}
		defer C.lttng_event_destroy(event)
		event._type = C.enum_lttng_event_type(rule.Type)
		event.loglevel_type = C.enum_lttng_loglevel_type(rule.LogLevelType)
		event.loglevel = C.int(rule.LogLevel)
		copyName(event.name[:], rule.Name)
		channelName, filter := optionalString(channel), optionalString(rule.Filter)
		defer free(channelName)
		defer free(filter)
		// Both the pointer array and its strings live in C memory.
		var exclusions **C.char
		if len(rule.Exclusions) > 0 {
			exclusions = (**C.char)(C.calloc(C.size_t(len(rule.Exclusions)), C.size_t(unsafe.Sizeof(uintptr(0)))))
			if exclusions == nil {
				return nativeError(op, -C.LTTNG_ERR_NOMEM)
			}
			defer C.free(unsafe.Pointer(exclusions))
			for i, name := range rule.Exclusions {
				ptr := C.CString(name)
				defer free(ptr)
				unsafe.Slice(exclusions, len(rule.Exclusions))[i] = ptr
			}
		}
		return nativeError(op, C.lttng_enable_event_with_exclusions(handle, event, channelName, filter, C.int(len(rule.Exclusions)), exclusions))
	})
}

// DisableEvent disables all recording rule variants matching name, regardless of
// filter or log level. Empty name matches all events; empty channel selects the
// default channel. It does not remove rule configurations.
func DisableEvent(ctx context.Context, session string, domain Domain, channel, name string) error {
	const op = "disable event"
	if err := checkString(op, "channel name", channel, C.LTTNG_SYMBOL_NAME_LEN, true); err != nil {
		return err
	}
	if err := checkString(op, "event name", name, C.LTTNG_SYMBOL_NAME_LEN, true); err != nil {
		return err
	}
	return withHandle(ctx, op, session, domain, func(handle *C.struct_lttng_handle) error {
		channelName, eventName := optionalString(channel), optionalString(name)
		defer free(channelName)
		defer free(eventName)
		return nativeError(op, C.lttng_disable_event(handle, eventName, channelName))
	})
}

// ListEvents lists recording rules of a channel. Channel must be explicit (for
// example "channel0"), as required by the native listing API.
func ListEvents(ctx context.Context, session string, domain Domain, channel string) ([]Event, error) {
	const op = "list events"
	if err := checkString(op, "channel name", channel, C.LTTNG_SYMBOL_NAME_LEN, false); err != nil {
		return nil, err
	}
	var events []Event
	err := withHandle(ctx, op, session, domain, func(handle *C.struct_lttng_handle) error {
		name := C.CString(channel)
		defer free(name)
		var ptr *C.struct_lttng_event
		n := C.lttng_list_events(handle, name, &ptr)
		defer C.free(unsafe.Pointer(ptr))
		if err := nativeError(op, n); err != nil {
			return err
		}
		var err error
		events, err = copyEvents(op, ptr, int(n), true)
		return err
	})
	return events, err
}

// ListTracepoints discovers instrumentation points registered with sessiond.
// No session is needed. UST applications must be running to be discoverable.
func ListTracepoints(ctx context.Context, domain Domain) ([]Event, error) {
	const op = "list tracepoints"
	if err := domain.validate(op); err != nil {
		return nil, err
	}
	if err := lock(ctx); err != nil {
		return nil, err
	}
	defer unlock()
	var events []Event
	err := useHandle(op, "", domain, func(handle *C.struct_lttng_handle) error {
		var ptr *C.struct_lttng_event
		n := C.lttng_list_tracepoints(handle, &ptr)
		defer C.free(unsafe.Pointer(ptr))
		if err := nativeError(op, n); err != nil {
			return err
		}
		var err error
		events, err = copyEvents(op, ptr, int(n), false)
		return err
	})
	return events, err
}

func copyEvents(op string, ptr *C.struct_lttng_event, n int, rules bool) ([]Event, error) {
	events := make([]Event, n)
	for i := range n {
		native := &unsafe.Slice(ptr, n)[i]
		event := Event{
			Name: C.GoString(&native.name[0]), Type: EventType(native._type),
			LogLevelType: LogLevelType(native.loglevel_type), LogLevel: int32(native.loglevel),
			PID: int(native.pid), Enabled: rules && native.enabled == 1,
		}
		if rules {
			var filter *C.char
			if err := nativeError(op, C.lttng_event_get_filter_expression(native, &filter)); err != nil {
				return nil, err
			}
			event.Filter = C.GoString(filter)
			count := C.lttng_event_get_exclusion_name_count(native)
			if err := nativeError(op, count); err != nil {
				return nil, err
			}
			event.Exclusions = make([]string, int(count))
			for j := range int(count) {
				var name *C.char
				if err := nativeError(op, C.lttng_event_get_exclusion_name(native, C.size_t(j), &name)); err != nil {
					return nil, err
				}
				event.Exclusions[j] = C.GoString(name)
			}
		}
		events[i] = event
	}
	return events, nil
}
