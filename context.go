//go:build linux && cgo

package lttng

/*
#include <lttng/lttng.h>
*/
import "C"

import "context"

// ContextType selects a basic context field. Domain restrictions are enforced
// by liblttng-ctl. Perf-counter and application-specific contexts are not exposed.
type ContextType int

const (
	ContextPID             ContextType = C.LTTNG_EVENT_CONTEXT_PID
	ContextVPID            ContextType = C.LTTNG_EVENT_CONTEXT_VPID
	ContextTID             ContextType = C.LTTNG_EVENT_CONTEXT_TID
	ContextVTID            ContextType = C.LTTNG_EVENT_CONTEXT_VTID
	ContextPPID            ContextType = C.LTTNG_EVENT_CONTEXT_PPID
	ContextVPPID           ContextType = C.LTTNG_EVENT_CONTEXT_VPPID
	ContextProcname        ContextType = C.LTTNG_EVENT_CONTEXT_PROCNAME
	ContextPthreadID       ContextType = C.LTTNG_EVENT_CONTEXT_PTHREAD_ID
	ContextHostname        ContextType = C.LTTNG_EVENT_CONTEXT_HOSTNAME
	ContextIP              ContextType = C.LTTNG_EVENT_CONTEXT_IP
	ContextCallstackUser   ContextType = C.LTTNG_EVENT_CONTEXT_CALLSTACK_USER
	ContextCallstackKernel ContextType = C.LTTNG_EVENT_CONTEXT_CALLSTACK_KERNEL
)

// AddContext adds a basic context field to a channel before the session's first
// start. Empty channel applies to all channels, following the native API.
func AddContext(ctx context.Context, session string, domain Domain, channel string, field ContextType) error {
	const op = "add context"
	if err := checkString(op, "channel name", channel, C.LTTNG_SYMBOL_NAME_LEN, true); err != nil {
		return err
	}
	switch field {
	case ContextPID, ContextVPID, ContextTID, ContextVTID, ContextPPID, ContextVPPID,
		ContextProcname, ContextPthreadID, ContextHostname, ContextIP, ContextCallstackUser, ContextCallstackKernel:
	default:
		return invalid(op, "unsupported basic context field")
	}
	return withHandle(ctx, op, session, domain, func(handle *C.struct_lttng_handle) error {
		name := optionalString(channel)
		defer free(name)
		field := C.struct_lttng_event_context{ctx: C.enum_lttng_event_context_type(field)}
		return nativeError(op, C.lttng_add_context(handle, &field, nil, name))
	})
}
