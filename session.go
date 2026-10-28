//go:build linux && cgo

package lttng

/*
#include <stdlib.h>
#include "internal/ctlstate/bridge_generated.h"
*/
import "C"

import (
	"context"
	"errors"
	"path/filepath"
	"time"
	"unsafe"
)

// SessionOptions selects a normal recording session's output. The zero value
// creates a session with no output. OutputPath and network URLs are exclusive.
type SessionOptions struct {
	// OutputPath is an absolute local directory, interpreted on the tracing host.
	OutputPath string
	// ControlURL and optional DataURL are LTTng network output URLs (not a
	// remote sessiond endpoint). An empty DataURL uses liblttng-ctl defaults.
	ControlURL string
	DataURL    string
}

// Session is a point-in-time summary, not an open C handle.
type Session struct {
	Name         string
	Path         string
	Enabled      bool
	SnapshotMode bool
	LiveTimer    time.Duration
}

// CreateSession creates a normal recording session. It does not start tracing or
// implicitly create channels or event rules. Older libraries use the legacy API.
func CreateSession(ctx context.Context, name string, options SessionOptions) error {
	const op = "create session"
	for _, field := range []struct{ name, value string }{
		{"output path", options.OutputPath}, {"control URL", options.ControlURL}, {"data URL", options.DataURL},
	} {
		if err := checkString(op, field.name, field.value, 0, true); err != nil {
			return err
		}
	}
	if options.OutputPath != "" && (!filepath.IsAbs(options.OutputPath) || options.ControlURL != "" || options.DataURL != "") {
		return invalid(op, "output path must be absolute and cannot be combined with network URLs")
	}
	if options.DataURL != "" && options.ControlURL == "" {
		return invalid(op, "data URL requires a control URL")
	}
	var rollbackError error
	err := sessionCommand(ctx, op, name, func(name *C.char) C.int {
		if !nativeHas("lttng_create_session_ext") {
			url := options.OutputPath
			if options.ControlURL != "" {
				url = options.ControlURL
			}
			if options.DataURL != "" {
				// Like the upstream CLI, configure separate URLs after creation.
				url = ""
			}
			nativeURL := optionalString(url)
			defer free(nativeURL)
			result := C.ctlgo_lttng_create_session(name, nativeURL)
			if result < 0 || options.DataURL == "" {
				return result
			}
			handle := C.ctlgo_lttng_create_handle(name, nil)
			if handle == nil {
				result = -C.LTTNG_ERR_NOMEM
			} else {
				defer C.ctlgo_lttng_destroy_handle(handle)
				control, data := C.CString(options.ControlURL), C.CString(options.DataURL)
				defer free(control)
				defer free(data)
				result = C.ctlgo_lttng_set_consumer_url(handle, control, data)
			}
			if result < 0 {
				rollbackError = nativeError("destroy session after URL setup failure", C.ctlgo_lttng_destroy_session(name))
			}
			return result
		}
		var descriptor *C.struct_lttng_session_descriptor
		switch {
		case options.OutputPath != "":
			path := C.CString(options.OutputPath)
			defer free(path)
			descriptor = C.ctlgo_lttng_session_descriptor_local_create(name, path)
		case options.ControlURL != "":
			control, data := C.CString(options.ControlURL), optionalString(options.DataURL)
			defer free(control)
			defer free(data)
			descriptor = C.ctlgo_lttng_session_descriptor_network_create(name, control, data)
		default:
			descriptor = C.ctlgo_lttng_session_descriptor_create(name)
		}
		if descriptor == nil {
			return -C.LTTNG_ERR_INVALID
		}
		defer C.ctlgo_lttng_session_descriptor_destroy(descriptor)
		code := C.ctlgo_lttng_create_session_ext(descriptor)
		if code != C.LTTNG_OK {
			return -C.int(code)
		}
		return 0
	})
	return errors.Join(err, rollbackError)
}

// ListSessions returns sessions visible to the process's Unix credentials.
func ListSessions(ctx context.Context) ([]Session, error) {
	if err := lock(ctx); err != nil {
		return nil, err
	}
	defer unlock()
	var ptr *C.struct_lttng_session
	n := C.ctlgo_lttng_list_sessions(&ptr)
	defer C.free(unsafe.Pointer(ptr))
	if err := nativeError("list sessions", n); err != nil {
		return nil, err
	}
	sessions := make([]Session, int(n))
	for i, session := range unsafe.Slice(ptr, int(n)) {
		sessions[i] = Session{
			Name: C.GoString(&session.name[0]), Path: C.GoString(&session.path[0]),
			Enabled: session.enabled != 0, SnapshotMode: session.snapshot_mode != 0,
			LiveTimer: time.Duration(session.live_timer_interval) * time.Microsecond,
		}
	}
	return sessions, nil
}

// SessionDaemonAlive reports whether the control library can reach sessiond.
// It never launches a daemon.
func SessionDaemonAlive(ctx context.Context) (bool, error) {
	if err := lock(ctx); err != nil {
		return false, err
	}
	defer unlock()
	result := C.ctlgo_lttng_session_daemon_alive()
	return result == 1, nativeError("session daemon alive", result)
}

// StartSession starts tracing. Already-started errors are not suppressed.
func StartSession(ctx context.Context, session string) error {
	return sessionCommand(ctx, "start session", session, func(name *C.char) C.int {
		return C.ctlgo_lttng_start_tracing(name)
	})
}

// StopSession stops tracing and waits for pending trace data to be consumed.
// This native wait cannot be interrupted by ctx. Use StopSessionNoWait and
// DataPending when the caller needs to control polling and cancellation.
func StopSession(ctx context.Context, session string) error {
	return sessionCommand(ctx, "stop session", session, func(name *C.char) C.int {
		return C.ctlgo_lttng_stop_tracing(name)
	})
}

// StopSessionNoWait stops tracing without waiting for trace data to be consumed.
// The command itself still performs a synchronous C call.
func StopSessionNoWait(ctx context.Context, session string) error {
	return sessionCommand(ctx, "stop session without waiting", session, func(name *C.char) C.int {
		return C.ctlgo_lttng_stop_tracing_no_wait(name)
	})
}

// DataPending reports whether a stopped session still has trace data to consume.
func DataPending(ctx context.Context, session string) (bool, error) {
	var pending bool
	err := sessionCommand(ctx, "data pending", session, func(name *C.char) C.int {
		result := C.ctlgo_lttng_data_pending(name)
		pending = result == 1
		return result
	})
	return pending, err
}

// DestroySession destroys a session and waits for destruction and data draining.
// It does not delete already recorded trace files. This native wait cannot be
// interrupted by ctx.
func DestroySession(ctx context.Context, session string) error {
	return sessionCommand(ctx, "destroy session", session, func(name *C.char) C.int {
		return C.ctlgo_lttng_destroy_session(name)
	})
}
