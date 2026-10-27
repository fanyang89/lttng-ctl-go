// Package lttng controls a local LTTng session daemon through liblttng-ctl.
// It requires Linux, CGO, and the LTTng control library and development headers.
// It does not execute the lttng command or implement its private wire protocol.
//
// All functions are safe for concurrent use: calls into liblttng-ctl are
// serialized across the package because the library uses process-global
// connection state. Other code calling the same C library must coordinate with
// this package externally. Session selection follows the library's normal Unix
// credentials, tracing group, and LTTNG_HOME rules; there is no per-client socket
// or credential configuration. Configure the environment before process startup.
//
// Context cancellation is checked before entering the C library, including while
// waiting for another call. An in-flight C call cannot be interrupted. Its actual
// result is returned even if the context expires meanwhile; a canceled context
// does not imply that a submitted operation was rolled back. No background
// goroutine is left running and no operation is automatically retried.
//
// Returned values own their Go memory. C handles and arrays are temporary and
// freed before returning; callers do not need to Close any SDK objects. Sessions
// themselves persist in sessiond until explicitly destroyed.
package lttng
