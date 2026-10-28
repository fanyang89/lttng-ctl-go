// Package ctl provides complete, typed bindings for the public liblttng-ctl
// 2.14 API. The parent lttng package provides simpler recording conveniences.
// The library is loaded at runtime; binaries also support 2.8, 2.10, and 2.12.
// APIs absent from the selected library return ErrUnsupported. HasSymbol checks
// native symbol availability. Build-time headers must still be 2.14.x.
//
// Functions retain upstream names without the lttng_ prefix, converted to Go
// capitalization. All take a context first. Scalar native return values are
// preserved, followed by converted output parameters and an error. Native status
// families are distinct Go enum types; negative statuses and non-OK general
// error codes also produce a StatusError. Wait timeout and completion statuses
// remain distinguishable. Strings returned by C are copied. A *string input
// preserves the distinction between NULL (nil) and an empty C string (new("")).
//
// Owned native objects and Lists require Close. Borrowed objects keep an owning
// resource reference and reject use after the owner is closed or modified;
// their Close does not destroy the parent's C object. Const borrows reject
// mutations. Ownership-transfer APIs invalidate the moved input only on success.
// Do not copy object wrappers by value; pointer aliases share lifetime state.
// There are no exposed C pointers, finalizers, or background native calls.
// Triggers retain their input resources even on older libraries without native
// reference counting; explicitly closed input wrappers still become unusable.
//
// Calls are serialized with the parent package. Contexts cancel queueing but
// cannot interrupt a native call already in progress. A blocking notification
// receive therefore also blocks other control calls in the same process; poll
// NotificationChannelHasPendingNotification first or dedicate a process to
// blocking reception.
// Input object/type-specific preconditions from the upstream C documentation
// still apply; the wrapper is not a policy or semantic validation layer.
//
//go:generate python3 ../scripts/generate-ctl.py
package ctl
